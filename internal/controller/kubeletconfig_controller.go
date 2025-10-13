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

package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

	mcov1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	intreconcile "github.com/openshift-kni/numaresources-operator/internal/reconcile"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	"github.com/openshift-kni/numaresources-operator/pkg/kubeletconfig"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	cfgstate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/cfg"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
)

const (
	kubeletConfigRetryPeriod           = 30 * time.Second
	MachineConfigPoolPausedRetryPeriod = 2 * time.Minute
)

const (
	HypershiftKubeletConfigConfigMapLabel     = "hypershift.openshift.io/kubeletconfig-config"
	HyperShiftNodePoolLabel                   = "hypershift.openshift.io/nodePool"
	HyperShiftConfigMapConfigKey              = "config"
	HyperShiftKubeletConfigConfigMapNamespace = "openshift-config-managed"
)

// KubeletConfigReconciler reconciles a KubeletConfig object
type KubeletConfigReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Namespace string
	Platform  platform.Platform
	// garbageCollectionFor is a list of objects
	// that their dependent needs to be removed from the cluster.
	garbageCollectionFor []*corev1.ConfigMap
}

type kubeletConfigHandler struct {
	ownerObject client.Object
	mcoKc       *mcov1.KubeletConfig
	// mcp or nodePool name
	poolName   string
	setCtrlRef func(owner, controlled metav1.Object, scheme *runtime.Scheme, opts ...controllerutil.OwnerReferenceOption) error
}

// Namespace Scoped

// Cluster Scoped
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
	cm, step := r.reconcileConfigMap(ctx, instance, req.NamespacedName)
	if step.Error != nil && step.ConditionInfo.Reason != intreconcile.EventProcessSkip {
		klog.ErrorS(step.Error, "failed to reconcile configmap", "controller", "kubeletconfig")
		r.Recorder.Event(instance, step.ConditionInfo.Type, step.ConditionInfo.Reason, step.ConditionInfo.Message)
		return step.Result, step.Error
	}

	if step.ConditionInfo.Reason == intreconcile.EventProcessSuccess {
		step = step.WithMessage(fmt.Sprintf("Updated RTE config %s/%s from kubelet config %s", cm.Namespace, cm.Name, req.NamespacedName.String()))
	}

	r.Recorder.Event(instance, step.ConditionInfo.Type, step.ConditionInfo.Reason, step.ConditionInfo.Message)

	return step.Result, nil
}

func (r *KubeletConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var o client.Object
	var p predicate.Funcs
	if r.Platform == platform.OpenShift {
		o = &mcov1.KubeletConfig{}
		// we have nothing to do in case of deletion
		p = predicate.Funcs{
			DeleteFunc: func(e event.DeleteEvent) bool {
				kubelet := e.Object.(*mcov1.KubeletConfig)
				klog.InfoS("KubeletConfig object got deleted", "KubeletConfig", kubelet.Name)
				return false
			},
		}
	}
	if r.Platform == platform.HyperShift {
		o = &corev1.ConfigMap{}
		p = predicate.NewPredicateFuncs(func(o client.Object) bool {
			kubelet := o.(*corev1.ConfigMap)
			_, ok := kubelet.Labels[HypershiftKubeletConfigConfigMapLabel]
			return ok
		})
		p.DeleteFunc = func(e event.DeleteEvent) bool {
			kubelet := e.Object.(*corev1.ConfigMap)
			if _, ok := kubelet.Labels[HypershiftKubeletConfigConfigMapLabel]; !ok {
				return false
			}
			klog.InfoS("KubeletConfig ConfigMap object got deleted", "KubeletConfig", kubelet.Name)
			r.garbageCollectionFor = append(r.garbageCollectionFor, kubelet)
			return true
		}
	}
	numaResourcesOperatorPredicate := predicate.Funcs{
		UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool {
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
		For(o, builder.WithPredicates(p)).
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

func (r *KubeletConfigReconciler) reconcileConfigMap(ctx context.Context, instance *nropv1.NUMAResourcesOperator, kcKey client.ObjectKey) (*corev1.ConfigMap, intreconcile.Step) {
	// first check if the ConfigMap should be deleted
	// to save all the additional work related for create/update
	cm, deleted, err := r.deleteConfigMap(ctx, instance, kcKey)
	if deleted {
		return cm, intreconcile.StepWarning(fmt.Errorf("Failed to update RTE config from kubelet config %s: %v", kcKey.Name, err))
	}

	kcHandler, step := r.makeKCHandlerForPlatform(ctx, instance, kcKey)
	if step.Error != nil {
		return nil, step
	}

	kubeletConfig, err := kubeletconfig.MCOKubeletConfToKubeletConf(kcHandler.mcoKc)
	if err != nil {
		klog.ErrorS(err, "cannot extract KubeletConfiguration from MCO KubeletConfig", "name", kcKey.Name)
		return nil, FailedConfigMapUpdateStep(kcKey.Name, err)
	}
	cm, err = r.syncConfigMap(ctx, kubeletConfig, instance, kcHandler)
	if err != nil {
		return cm, FailedConfigMapUpdateStep(kcKey.Name, err)
	}
	return cm, intreconcile.StepNormalSucess("")
}

func (r *KubeletConfigReconciler) syncConfigMap(ctx context.Context, kubeletConfig *kubeletconfigv1beta1.KubeletConfiguration, instance *nropv1.NUMAResourcesOperator, kcHandler *kubeletConfigHandler) (*corev1.ConfigMap, error) {
	generatedName := objectnames.GetComponentName(instance.Name, kcHandler.poolName)
	klog.V(3).InfoS("generated configMap name", "generatedName", generatedName)

	data, err := rteconfig.Render(kubeletConfig, instance.Spec.PodExcludes)
	if err != nil {
		klog.ErrorS(err, "rendering config", "namespace", r.Namespace, "name", generatedName)
		return nil, err
	}

	rendered := rteconfig.CreateConfigMap(r.Namespace, generatedName, data)
	cfgManifests := cfgstate.Manifests{
		Config: rteconfig.AddSoftRefLabels(rendered, instance.Name, kcHandler.poolName),
	}
	existing := cfgstate.FromClient(ctx, r.Client, r.Namespace, generatedName)
	for _, objState := range existing.State(cfgManifests) {
		if err := kcHandler.setCtrlRef(kcHandler.ownerObject, objState.Desired, r.Scheme); err != nil {
			return nil, fmt.Errorf("failed to set controller reference to %s %s: %w", objState.Desired.GetNamespace(), objState.Desired.GetName(), err)
		}
		if _, _, err := apply.ApplyObject(ctx, r.Client, objState); err != nil {
			return nil, fmt.Errorf("could not create %s: %w", objState.Desired.GetObjectKind().GroupVersionKind().String(), err)
		}
	}
	return rendered, nil
}

func (r *KubeletConfigReconciler) makeKCHandlerForPlatform(ctx context.Context, instance *nropv1.NUMAResourcesOperator, kcKey client.ObjectKey) (*kubeletConfigHandler, intreconcile.Step) {
	switch r.Platform {
	case platform.OpenShift:
		mcoKc := &mcov1.KubeletConfig{}
		if err := r.Client.Get(ctx, kcKey, mcoKc); err != nil {
			return nil, FailedConfigMapUpdateStep(kcKey.Name, err)
		}

		mcps, err := machineconfigpools.GetListByNodeGroupsV1(ctx, r.Client, instance.Spec.NodeGroups)
		if err != nil {
			return nil, FailedConfigMapUpdateStep(kcKey.Name, err)
		}

		mcp, err := machineconfigpools.FindBySelector(mcps, mcoKc.Spec.MachineConfigPoolSelector)
		if err != nil {
			klog.ErrorS(err, "cannot find a matching mcp for MCO KubeletConfig", "name", kcKey.Name)
			var notFound *machineconfigpools.NotFound
			if errors.As(err, &notFound) {
				return nil, intreconcile.StepNormalSkip(fmt.Errorf("%s: %v", kcKey, notFound))
			}
			return nil, FailedConfigMapUpdateStep(kcKey.Name, err)
		}

		klog.V(3).InfoS("matched MCP to MCO KubeletConfig", "kubeletconfig name", kcKey.Name, "MCP name", mcp.Name)

		// nothing we care about, and we can't do much anyway
		if mcoKc.Spec.KubeletConfig == nil {
			klog.InfoS("detected KubeletConfig with empty payload, ignoring", "name", kcKey.Name)
			return nil, intreconcile.StepNormalSkip(fmt.Errorf("Invalid KubeletConfig %s", kcKey.Name))
		}

		if mcp.Spec.Paused {
			klog.InfoS("detected paused MCP", "name", mcp.Name)
			step := intreconcile.StepNormalSkip(fmt.Errorf("MachineConfigPool of KubeletConfig %s is paused", kcKey.Name))
			step.Result = ctrl.Result{RequeueAfter: MachineConfigPoolPausedRetryPeriod}
			return nil, step
		}

		return &kubeletConfigHandler{
			ownerObject: mcoKc,
			mcoKc:       mcoKc,
			poolName:    mcp.Name,
			setCtrlRef:  controllerutil.SetControllerReference,
		}, intreconcile.StepNormalSucess("")

	case platform.HyperShift:
		cmKc := &corev1.ConfigMap{}
		if err := r.Client.Get(ctx, kcKey, cmKc); err != nil {
			return nil, FailedConfigMapUpdateStep(kcKey.Name, err)
		}

		nodePoolName := cmKc.Labels[HyperShiftNodePoolLabel]
		kcData := cmKc.Data[HyperShiftConfigMapConfigKey]
		mcoKc, err := kubeletconfig.DecodeFromData([]byte(kcData), r.Scheme)
		if err != nil {
			return nil, FailedConfigMapUpdateStep(kcKey.Name, err)
		}

		// nothing we care about, and we can't do much anyway
		if mcoKc.Spec.KubeletConfig == nil {
			klog.InfoS("detected KubeletConfig with empty payload, ignoring", "name", kcKey.Name)
			return nil, intreconcile.StepNormalSkip(fmt.Errorf("Invalid KubeletConfig %s", kcKey.Name))
		}
		return &kubeletConfigHandler{
			ownerObject: cmKc,
			mcoKc:       mcoKc,
			poolName:    nodePoolName,
			// the owner should be the KubeletConfig object and not the NUMAResourcesOperator CR
			// this means that when KubeletConfig will get deleted, the ConfigMap gets deleted as well
			// TODO on HyperShift there's a cross-namespaced owner references that need to be fixed.
			setCtrlRef: func(owner, controlled metav1.Object, scheme *runtime.Scheme, opts ...controllerutil.OwnerReferenceOption) error {
				return nil
			},
		}, intreconcile.StepNormalSucess("")
	}
	return nil, FailedConfigMapUpdateStep(kcKey.Name, fmt.Errorf("unsupported platform: %s", r.Platform))
}
func FailedConfigMapUpdateStep(objName string, err error) intreconcile.Step {
	return intreconcile.StepWarning(fmt.Errorf("Failed to update RTE config from kubelet config %s: %v", objName, err))
}

func (r *KubeletConfigReconciler) deleteConfigMap(ctx context.Context, instance *nropv1.NUMAResourcesOperator, kcKey client.ObjectKey) (*corev1.ConfigMap, bool, error) {
	cm := getDeletedOwner(kcKey, r.garbageCollectionFor)
	if cm == nil {
		return nil, false, nil
	}
	// we'll get to this flow only on hypershift
	// on openshift the deletion is done automatically by setting the owner reference on the dependent ConfigMap
	nodePoolName := cm.Labels[HyperShiftNodePoolLabel]
	generatedName := objectnames.GetComponentName(instance.Name, nodePoolName)
	dependentCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generatedName,
			Namespace: r.Namespace,
		},
	}
	if err := r.Delete(ctx, dependentCM); err != nil {
		if !apierrors.IsNotFound(err) {
			return cm, true, err
		}
		klog.V(2).InfoS("could not delete ConfigMap since it was not found", "configmapName", dependentCM.Name)
	}
	r.garbageCollectionFor = removeDeletedOwner(kcKey, r.garbageCollectionFor)
	return cm, true, nil
}

func (r *KubeletConfigReconciler) numaResourcesOperatorToKubeletConfig(ctx context.Context, object client.Object) []reconcile.Request {
	var requests []reconcile.Request
	if r.Platform == platform.OpenShift {
		kcList := &mcov1.KubeletConfigList{}
		if err := r.Client.List(ctx, kcList); err != nil {
			klog.ErrorS(err, "failed to list KubeletConfigs %v")
		}
		for _, kc := range kcList.Items {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKey{
				Name: kc.Name,
			}})
		}
	}
	if r.Platform == platform.HyperShift {
		cmList := &corev1.ConfigMapList{}
		opts := client.ListOptions{
			Namespace:     HyperShiftKubeletConfigConfigMapNamespace,
			LabelSelector: labels.SelectorFromSet(map[string]string{HypershiftKubeletConfigConfigMapLabel: "true"}),
		}
		if err := r.Client.List(ctx, cmList, &opts); err != nil {
			klog.ErrorS(err, "failed to list KubeletConfig ConfigMaps")
		}
		for _, cm := range cmList.Items {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKey{
				Name: cm.Name,
			}})
		}
	}
	return requests
}

func getDeletedOwner(kcKey client.ObjectKey, ownerConfigMaps []*corev1.ConfigMap) *corev1.ConfigMap {
	for i := range ownerConfigMaps {
		cm := ownerConfigMaps[i]
		if client.ObjectKeyFromObject(cm).String() == kcKey.String() {
			return cm
		}
	}
	return nil
}

func removeDeletedOwner(kcKey client.ObjectKey, ownerConfigMaps []*corev1.ConfigMap) []*corev1.ConfigMap {
	for i := range ownerConfigMaps {
		if client.ObjectKeyFromObject(ownerConfigMaps[i]) == kcKey {
			return slices.Delete(ownerConfigMaps, i, i+1)
		}
	}
	return ownerConfigMaps
}
