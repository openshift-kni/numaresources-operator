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
	"k8s.io/apimachinery/pkg/api/equality"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	"github.com/pkg/errors"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	"github.com/openshift-kni/numaresources-operator/pkg/kubeletconfig"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	cfgstate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/cfg"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
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
	instance := &nropv1.NUMAResourcesOperator{}
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
		var klErr *InvalidKubeletConfig
		if errors.As(err, &klErr) {
			r.Recorder.Event(instance, "Normal", "ProcessSkip", "ignored kubelet config "+klErr.ObjectName)
			return ctrl.Result{}, nil
		}

		klog.ErrorS(err, "failed to reconcile configmap", "controller", "kubeletconfig")

		r.Recorder.Event(instance, "Warning", "ProcessFailed", "Failed to update RTE config from kubelet config "+req.NamespacedName.String())
		return ctrl.Result{}, err
	}

	r.Recorder.Event(instance, "Normal", "ProcessOK", fmt.Sprintf("Updated RTE config %s/%s from kubelet config %s", cm.Namespace, cm.Name, req.NamespacedName.String()))
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
	numaResourcesOperatorPredicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			nroOld := e.ObjectOld.(*nropv1.NUMAResourcesOperator)
			nroNew := e.ObjectNew.(*nropv1.NUMAResourcesOperator)
			oldNodeGroups := nroOld.Spec.NodeGroups
			newNodeGroups := nroNew.Spec.NodeGroups
			// if nodeGroups has changed,
			// it means that we should iterate and create/delete ConfigMap data for RTE pods
			return equality.Semantic.DeepEqual(oldNodeGroups, newNodeGroups)
		},
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcov1.KubeletConfig{}, builder.WithPredicates(p)).
		Owns(&corev1.ConfigMap{}).
		Watches(&nropv1.NUMAResourcesOperator{}, handler.EnqueueRequestsFromMapFunc(r.numaResourcesOperatorToKubeletConfig),
			builder.WithPredicates(numaResourcesOperatorPredicate)).
		Complete(r)
}

type InvalidKubeletConfig struct {
	ObjectName string
	Err        error
}

func (e *InvalidKubeletConfig) Error() string {
	return "invalid KubeletConfig object: " + e.ObjectName
}

func (e *InvalidKubeletConfig) Unwrap() error {
	return e.Err
}

func (r *KubeletConfigReconciler) reconcileConfigMap(ctx context.Context, instance *nropv1.NUMAResourcesOperator, kcKey client.ObjectKey) (*corev1.ConfigMap, error) {
	mcoKc := &mcov1.KubeletConfig{}
	if err := r.Client.Get(ctx, kcKey, mcoKc); err != nil {
		return nil, err
	}

	mcps, err := machineconfigpools.GetListByNodeGroupsV1(ctx, r.Client, instance.Spec.NodeGroups)
	if err != nil {
		return nil, err
	}

	mcp, err := machineconfigpools.FindBySelector(mcps, mcoKc.Spec.MachineConfigPoolSelector)
	if err != nil {
		klog.ErrorS(err, "cannot find a matching mcp for MCO KubeletConfig", "name", kcKey.Name)
		var notFound *machineconfigpools.NotFound
		if errors.As(err, &notFound) {
			return nil, &InvalidKubeletConfig{
				ObjectName: kcKey.Name,
				Err:        notFound,
			}
		}
		return nil, err
	}

	klog.V(3).InfoS("matched MCP to MCO KubeletConfig", "kubeletconfig name", kcKey.Name, "MCP name", mcp.Name)

	// nothing we care about, and we can't do much anyway
	if mcoKc.Spec.KubeletConfig == nil {
		klog.InfoS("detected KubeletConfig with empty payload, ignoring", "name", kcKey.Name)
		return nil, &InvalidKubeletConfig{ObjectName: kcKey.Name}
	}

	kubeletConfig, err := kubeletconfig.MCOKubeletConfToKubeletConf(mcoKc)
	if err != nil {
		klog.ErrorS(err, "cannot extract KubeletConfiguration from MCO KubeletConfig", "name", kcKey.Name)
		return nil, err
	}

	return r.syncConfigMap(ctx, mcoKc, kubeletConfig, instance, mcp.Name)
}

func (r *KubeletConfigReconciler) syncConfigMap(ctx context.Context, mcoKc *mcov1.KubeletConfig, kubeletConfig *kubeletconfigv1beta1.KubeletConfiguration, instance *nropv1.NUMAResourcesOperator, mcpName string) (*corev1.ConfigMap, error) {
	generatedName := objectnames.GetComponentName(instance.Name, mcpName)
	klog.V(3).InfoS("generated configMap name", "generatedName", generatedName)

	podExcludes := podExcludesListToMap(instance.Spec.PodExcludes)
	klog.V(5).InfoS("using podExcludes", "podExcludes", podExcludes)

	data, err := rteconfig.Render(kubeletConfig, podExcludes)
	if err != nil {
		klog.ErrorS(err, "rendering config", "namespace", r.Namespace, "name", generatedName)
		return nil, err
	}

	rendered := rteconfig.CreateConfigMap(r.Namespace, generatedName, data)
	cfgManifests := cfgstate.Manifests{
		Config: rteconfig.AddSoftRefLabels(rendered, instance.Name, mcpName),
	}
	existing := cfgstate.FromClient(ctx, r.Client, r.Namespace, generatedName)
	for _, objState := range existing.State(cfgManifests) {
		// the owner should be the KubeletConfig object and not the NUMAResourcesOperator CR
		// this means that when KubeletConfig will get deleted, the ConfigMap gets deleted as well
		if err := controllerutil.SetControllerReference(mcoKc, objState.Desired, r.Scheme); err != nil {
			return nil, errors.Wrapf(err, "Failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
		if _, err := apply.ApplyObject(ctx, r.Client, objState); err != nil {
			return nil, errors.Wrapf(err, "could not create %s", objState.Desired.GetObjectKind().GroupVersionKind().String())
		}
	}
	return rendered, nil
}

func (r *KubeletConfigReconciler) numaResourcesOperatorToKubeletConfig(ctx context.Context, object client.Object) []reconcile.Request {
	var requests []reconcile.Request
	kcList := &mcov1.KubeletConfigList{}
	if err := r.Client.List(ctx, kcList); err != nil {
		klog.ErrorS(err, "failed to list KubeletConfigs %v")
	}
	for _, kc := range kcList.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKey{
			Name: kc.Name,
		}})
	}
	return requests
}

func podExcludesListToMap(podExcludes []nropv1.NamespacedName) map[string]string {
	ret := make(map[string]string)
	for _, pe := range podExcludes {
		ret[pe.Namespace] = pe.Name
	}
	return ret
}
