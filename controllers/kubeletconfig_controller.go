/*
 * Copyright 2021 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controllers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	"github.com/ghodss/yaml"
	"github.com/pkg/errors"

	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	"github.com/openshift-kni/numaresources-operator/pkg/kubeletconfig"
	"github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	cfgstate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/cfg"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
	"github.com/openshift-kni/numaresources-operator/rte/pkg/sysinfo"
)

const (
	kubeletConfigRetryPeriod = 30 * time.Second
)

// KubeletConfigReconciler reconciles a KubeletConfig object
type KubeletConfigReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Namespace string
}

//+kubebuilder:rbac:groups="",resources=configmaps,verbs=*
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=machineconfiguration.openshift.io,resources=kubeletconfigs,verbs=get;list;watch
//+kubebuilder:rbac:groups=machineconfiguration.openshift.io,resources=kubeletconfigs/finalizers,verbs=update

func (r *KubeletConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.V(3).InfoS("Starting KubeletConfig reconcile loop", "object", req.NamespacedName)
	defer klog.V(3).InfoS("Finish KubeletConfig reconcile loop", "object", req.NamespacedName)

	nname := types.NamespacedName{
		Name: objectnames.DefaultNUMAResourcesOperatorCrName,
	}
	instance := &nropv1alpha1.NUMAResourcesOperator{}
	err := r.Get(ctx, nname, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// so we need to have a NUMAResourceOperator instance to be able to set owner refs
			// correctly. And we just wait forever until it comes, which is expected to be "soon".
			return ctrl.Result{RequeueAfter: kubeletConfigRetryPeriod}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// KubeletConfig changes are expected to be sporadic, yet are important enough
	// to be made visible at kubernetes level. So we generate events to handle them

	cm, err := r.reconcileConfigMap(ctx, instance, req.NamespacedName)
	if err != nil {
		klog.ErrorS(err, "failed to reconcile configmap", "controller", "kubeletconfig")

		msg := fmt.Sprintf("Failed to update RTE config from kubelet config %s/%s", req.NamespacedName.Namespace, req.NamespacedName.Name)
		r.Recorder.Event(instance, "Warning", "ProcessFailed", msg)
		return ctrl.Result{}, err
	}

	msg := fmt.Sprintf("Updated RTE config %s/%s from kubelet config %s/%s", cm.Namespace, cm.Name, req.NamespacedName.Namespace, req.NamespacedName.Name)
	r.Recorder.Event(instance, "Normal", "ProcessOK", msg)
	return ctrl.Result{}, nil
}

func (r *KubeletConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// we have nothing to do in case of deletion
	p := predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			kubelet := e.Object.(*mcov1.KubeletConfig)
			klog.InfoS("KubeletConfig object got deleted", "KubeletConfig", kubelet.Name)
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&mcov1.KubeletConfig{}, builder.WithPredicates(p)).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}

func (r *KubeletConfigReconciler) reconcileConfigMap(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator, kcKey client.ObjectKey) (*corev1.ConfigMap, error) {
	mcoKc := &mcov1.KubeletConfig{}
	if err := r.Client.Get(ctx, kcKey, mcoKc); err != nil {
		return nil, err
	}

	kubeletConfig, err := kubeletconfig.MCOKubeletConfToKubeletConf(mcoKc)
	if err != nil {
		klog.ErrorS(err, "cannot extract KubeletConfiguration from MCO KubeletConfig", "name", kcKey.Name)
		return nil, err
	}

	mcps, err := machineconfigpools.GetListByNodeGroups(ctx, r.Client, instance.Spec.NodeGroups)
	if err != nil {
		return nil, err
	}

	mcp, err := machineconfigpools.FindBySelector(mcps, mcoKc.Spec.MachineConfigPoolSelector)
	if err != nil {
		klog.ErrorS(err, "cannot find a matching mcp for MCO KubeletConfig", "name", kcKey.Name)
		return nil, err
	}
	klog.InfoS("matched MCP to MCO KubeletConfig", "kubeletconfig name", kcKey.Name, "MCP name", mcp.Name)

	generatedName := objectnames.GetComponentName(instance.Name, mcp.Name)
	klog.V(3).InfoS("generated configMap name", "generatedName", generatedName)

	klog.V(5).InfoS("using podExcludes", "podExcludes", instance.Spec.PodExcludes)
	return r.syncConfigMap(ctx, mcoKc, kubeletConfig, generatedName, instance.Spec.PodExcludes)
}

func (r *KubeletConfigReconciler) syncConfigMap(ctx context.Context, mcoKc *mcov1.KubeletConfig, kubeletConfig *kubeletconfigv1beta1.KubeletConfiguration, name string, podExcludes map[string]string) (*corev1.ConfigMap, error) {
	rendered, err := renderRTEConfig(r.Namespace, name, kubeletConfig, podExcludes)
	if err != nil {
		klog.ErrorS(err, "rendering config", "namespace", r.Namespace, "name", name)
		return nil, err
	}

	cfgManifests := cfgstate.Manifests{
		Config: rendered,
	}
	existing := cfgstate.FromClient(context.TODO(), r.Client, r.Namespace, name)
	for _, objState := range existing.State(cfgManifests) {
		// the owner should be the KubeletConfig object and not the NUMAResourcesOperator CR
		// this means that when KubeletConfig will get deleted, the ConfigMap gets deleted as well
		if err := controllerutil.SetControllerReference(mcoKc, objState.Desired, r.Scheme); err != nil {
			return nil, errors.Wrapf(err, "Failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
		if _, err := apply.ApplyObject(context.TODO(), r.Client, objState); err != nil {
			return nil, errors.Wrapf(err, "could not create %s", objState.Desired.GetObjectKind().GroupVersionKind().String())
		}
	}
	return rendered, nil
}

func renderRTEConfig(namespace, name string, klConfig *kubeletconfigv1beta1.KubeletConfiguration, podExcludes map[string]string) (*corev1.ConfigMap, error) {
	conf := rteconfig.Config{
		Resources: sysinfo.Config{
			ReservedCPUs:   klConfig.ReservedSystemCPUs,
			ReservedMemory: findReservedMemoryFromKubelet(klConfig.ReservedMemory),
		},
		TopologyManagerPolicy: klConfig.TopologyManagerPolicy,
		TopologyManagerScope:  klConfig.TopologyManagerScope,
	}
	if len(podExcludes) > 0 {
		conf.PodExcludes = podExcludes
	}
	data, err := yaml.Marshal(conf)
	if err != nil {
		return nil, err
	}
	return rtemanifests.CreateConfigMap(namespace, name, string(data)), nil
}

func findReservedMemoryFromKubelet(klMemRes []kubeletconfigv1beta1.MemoryReservation) map[int]int64 {
	res := make(map[int]int64)
	for _, memRes := range klMemRes {
		for resName, resQty := range memRes.Limits {
			if resName != corev1.ResourceMemory {
				// TODO we support only memory reservation atm
				continue
			}
			v, ok := resQty.AsInt64()
			if !ok {
				// TODO log?
				continue
			}
			res[int(memRes.NumaNode)] = v
		}
	}
	return res
}
