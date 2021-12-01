/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	apistate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/api"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	rtestate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/pkg/validation"
)

const (
	defaultNUMAResourcesOperatorCrName = "numaresourcesoperator"
	numaResourcesRetryPeriod           = 1 * time.Minute
)

// NUMAResourcesOperatorReconciler reconciles a NUMAResourcesOperator object
type NUMAResourcesOperatorReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Platform        platform.Platform
	APIManifests    apimanifests.Manifests
	RTEManifests    rtemanifests.Manifests
	Helper          *deployer.Helper
	Namespace       string
	ImageSpec       string
	ImagePullPolicy corev1.PullPolicy
}

// TODO: narrow down

// Namespace Scoped
// TODO

// Cluster Scoped
//+kubebuilder:rbac:groups=topology.node.k8s.io,resources=noderesourcetopologies,verbs=get;list;create;update
//+kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=list
//+kubebuilder:rbac:groups=machineconfiguration.openshift.io,resources=machineconfigs,verbs=*
//+kubebuilder:rbac:groups=machineconfiguration.openshift.io,resources=machineconfigpools,verbs=get;list;watch
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=*
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=*
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=*
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=*
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=nodetopology.openshift.io,resources=numaresourcesoperators,verbs=*
//+kubebuilder:rbac:groups=nodetopology.openshift.io,resources=numaresourcesoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nodetopology.openshift.io,resources=numaresourcesoperators/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *NUMAResourcesOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	klog.V(3).InfoS("Starting NUMAResourcesOperator reconcile loop", "object", req.NamespacedName)
	defer klog.V(3).InfoS("Finish NUMAResourcesOperator reconcile loop", "object", req.NamespacedName)

	instance := &nropv1alpha1.NUMAResourcesOperator{}
	err := r.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	if req.Name != defaultNUMAResourcesOperatorCrName {
		message := fmt.Sprintf("incorrect NUMAResourcesOperator resource name: %s", instance.Name)
		return r.updateStatus(ctx, instance, status.ConditionDegraded, status.ConditionTypeIncorrectNUMAResourcesOperatorResourceName, message)
	}

	if err := validation.NodeGroups(instance.Spec.NodeGroups); err != nil {
		return r.updateStatus(ctx, instance, status.ConditionDegraded, validation.NodeGroupsError, err.Error())
	}

	mcps, err := getNodeGroupsMCPs(ctx, r.Client, instance.Spec.NodeGroups)
	if err != nil {
		return r.updateStatus(ctx, instance, status.ConditionDegraded, validation.NodeGroupsError, err.Error())
	}

	if err := validation.MachineConfigPoolDuplicates(mcps); err != nil {
		return r.updateStatus(ctx, instance, status.ConditionDegraded, validation.NodeGroupsError, err.Error())
	}

	result, condition, err := r.reconcileResource(ctx, instance, mcps)
	if condition != "" {
		// TODO: use proper reason
		reason, message := condition, messageFromError(err)
		if err := status.Update(ctx, r.Client, instance, condition, reason, message); err != nil {
			klog.InfoS("Failed to update numaresourcesoperator status", "Desired condition", condition, "error", err)
		}
	}
	return result, err
}

func (r *NUMAResourcesOperatorReconciler) updateStatus(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator, condition string, reason string, message string) (ctrl.Result, error) {
	klog.Error(message)

	if err := status.Update(ctx, r.Client, instance, condition, reason, message); err != nil {
		klog.InfoS("Failed to update numaresourcesoperator status", "Desired condition", status.ConditionDegraded, "error", err)
		return ctrl.Result{}, err
	}

	// we do not return an error here because to pass the validation error a user will need to update NRO CR
	// that will anyway initiate to reconcile loop
	return ctrl.Result{}, nil
}

func messageFromError(err error) string {
	if err == nil {
		return ""
	}
	unwErr := errors.Unwrap(err)
	if unwErr == nil {
		return ""
	}
	return unwErr.Error()
}

func (r *NUMAResourcesOperatorReconciler) reconcileResource(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator, mcps []*machineconfigv1.MachineConfigPool) (ctrl.Result, string, error) {
	var err error
	err = r.syncNodeResourceTopologyAPI()
	if err != nil {
		return ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "FailedAPISync")
	}

	if r.Platform == platform.OpenShift {
		// we need to create machine configs first and wait for the MachineConfigPool updates
		// before creating additional components
		if err := r.syncMachineConfigs(ctx, instance, mcps); err != nil {
			return ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "failed to sync machine configs")
		}

		if !isMachineConfigPoolsUpdated(instance, mcps) {
			// the Machine Config Pool still did not apply the machine config, wait for one minute
			return ctrl.Result{RequeueAfter: numaResourcesRetryPeriod}, status.ConditionProgressing, nil
		}
	}

	daemonSetsInfo, err := r.syncNUMAResourcesOperatorResources(ctx, instance, mcps)
	if err != nil {
		return ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "FailedRTESync")
	}

	instance.Status.DaemonSets = []nropv1alpha1.NamespacedName{}
	for _, nname := range daemonSetsInfo {
		ok, err := r.Helper.IsDaemonSetRunning(nname.Namespace, nname.Name)
		if err != nil {
			return ctrl.Result{}, status.ConditionDegraded, err
		}
		if !ok {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, status.ConditionProgressing, nil
		}

		instance.Status.DaemonSets = append(instance.Status.DaemonSets, nname)
	}

	return ctrl.Result{}, status.ConditionAvailable, nil
}

func (r *NUMAResourcesOperatorReconciler) syncNodeResourceTopologyAPI() error {
	klog.Info("APISync start")

	existing := apistate.FromClient(context.TODO(), r.Client, r.Platform, r.APIManifests)

	for _, objState := range existing.State(r.APIManifests) {
		if _, err := apply.ApplyObject(context.TODO(), r.Client, objState); err != nil {
			return errors.Wrapf(err, "could not create %s", objState.Desired.GetObjectKind().GroupVersionKind().String())
		}
	}
	return nil
}

func (r *NUMAResourcesOperatorReconciler) syncMachineConfigs(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator, mcps []*machineconfigv1.MachineConfigPool) error {
	klog.Info("Machine Config Sync start")

	existing := rtestate.FromClient(ctx, r.Client, r.Platform, r.RTEManifests, instance, mcps, r.Namespace)

	// create MC objects first
	for _, objState := range existing.MachineConfigsState(r.RTEManifests, instance, mcps) {
		if err := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme); err != nil {
			return errors.Wrapf(err, "Failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
		_, err := apply.ApplyObject(ctx, r.Client, objState)
		if err != nil {
			return errors.Wrapf(err, "could not apply (%s) %s/%s", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
	}

	return nil
}

func (r *NUMAResourcesOperatorReconciler) syncNUMAResourcesOperatorResources(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator, mcps []*machineconfigv1.MachineConfigPool) ([]nropv1alpha1.NamespacedName, error) {
	klog.Info("RTESync start")

	rtestate.UpdateDaemonSetUserImageSettings(r.RTEManifests.DaemonSet, instance.Spec.ExporterImage, r.ImageSpec, r.ImagePullPolicy)

	var daemonSetsNName []nropv1alpha1.NamespacedName
	existing := rtestate.FromClient(ctx, r.Client, r.Platform, r.RTEManifests, instance, mcps, r.Namespace)
	for _, objState := range existing.State(r.RTEManifests, r.Platform, instance, mcps) {
		if err := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme); err != nil {
			return nil, errors.Wrapf(err, "Failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
		obj, err := apply.ApplyObject(ctx, r.Client, objState)
		if err != nil {
			return nil, errors.Wrapf(err, "could not apply (%s) %s/%s", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName())
		}

		if nname, ok := rte.DaemonSetNamespacedNameFromObject(obj); ok {
			daemonSetsNName = append(daemonSetsNName, nname)
		}
	}
	return daemonSetsNName, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NUMAResourcesOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// we want to initate reconcile loop only on change under labels or spec of the object
	p := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if !validateUpdateEvent(&e) {
				return false
			}

			return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration() ||
				!apiequality.Semantic.DeepEqual(e.ObjectNew.GetLabels(), e.ObjectOld.GetLabels())
		},
	}

	mcpPredicates := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if !validateUpdateEvent(&e) {
				return false
			}

			mcpOld := e.ObjectOld.(*machineconfigv1.MachineConfigPool)
			mcpNew := e.ObjectNew.(*machineconfigv1.MachineConfigPool)

			// we only interested in updates related to MachineConfigPool label, machineConfigSelector, nodeSelector or status conditions
			return !reflect.DeepEqual(mcpOld.Status.Conditions, mcpNew.Status.Conditions) ||
				!apiequality.Semantic.DeepEqual(mcpOld.Labels, mcpNew.Labels) ||
				!apiequality.Semantic.DeepEqual(mcpOld.Spec.MachineConfigSelector, mcpNew.Spec.MachineConfigSelector) ||
				!apiequality.Semantic.DeepEqual(mcpOld.Spec.NodeSelector, mcpNew.Spec.NodeSelector)
		},
	}

	b := ctrl.NewControllerManagedBy(mgr).For(&nropv1alpha1.NUMAResourcesOperator{})
	if r.Platform == platform.OpenShift {
		b = b.Owns(&securityv1.SecurityContextConstraints{}).
			Owns(&machineconfigv1.MachineConfig{}, builder.WithPredicates(p))
	}
	return b.Owns(&apiextensionv1.CustomResourceDefinition{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.Role{}).
		Owns(&appsv1.DaemonSet{}, builder.WithPredicates(p)).
		Watches(
			&source.Kind{Type: &machineconfigv1.MachineConfigPool{}},
			handler.EnqueueRequestsFromMapFunc(r.mcpToNUMAResourceOperator),
			builder.WithPredicates(mcpPredicates)).
		Complete(r)
}

func (r *NUMAResourcesOperatorReconciler) mcpToNUMAResourceOperator(mcpObj client.Object) []reconcile.Request {
	mcp := &machineconfigv1.MachineConfigPool{}

	key := client.ObjectKey{
		Namespace: mcpObj.GetNamespace(),
		Name:      mcpObj.GetName(),
	}
	if err := r.Get(context.TODO(), key, mcp); err != nil {
		klog.Errorf("failed to get the machine config pool %+v", key)
		return nil
	}

	nros := &nropv1alpha1.NUMAResourcesOperatorList{}
	if err := r.List(context.TODO(), nros); err != nil {
		klog.Error("failed to get numa-resources operator")
		return nil
	}

	var requests []reconcile.Request
	for i := range nros.Items {
		nro := &nros.Items[i]
		mcpLabels := labels.Set(mcp.Labels)
		for _, nodeGroup := range nro.Spec.NodeGroups {
			if nodeGroup.MachineConfigPoolSelector == nil {
				continue
			}

			nodeGroupSelector, err := metav1.LabelSelectorAsSelector(nodeGroup.MachineConfigPoolSelector)
			if err != nil {
				klog.Errorf("failed to parse the selector %v", mcp.Spec.NodeSelector)
				return nil
			}

			if nodeGroupSelector.Matches(mcpLabels) {
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKey{
						Name: nro.Name,
					},
				})
			}
		}
	}

	return requests
}

func validateUpdateEvent(e *event.UpdateEvent) bool {
	if e.ObjectOld == nil {
		klog.Error("Update event has no old runtime object to update")
		return false
	}
	if e.ObjectNew == nil {
		klog.Error("Update event has no new runtime object for update")
		return false
	}

	return true
}

func getNodeGroupsMCPs(ctx context.Context, cli client.Client, nodeGroups []nropv1alpha1.NodeGroup) ([]*machineconfigv1.MachineConfigPool, error) {
	mcps := &machineconfigv1.MachineConfigPoolList{}
	if err := cli.List(ctx, mcps); err != nil {
		return nil, err
	}

	var result []*machineconfigv1.MachineConfigPool
	for _, nodeGroup := range nodeGroups {
		found := false

		// handled by validation
		if nodeGroup.MachineConfigPoolSelector == nil {
			continue
		}

		for i := range mcps.Items {
			mcp := &mcps.Items[i]

			selector, err := metav1.LabelSelectorAsSelector(nodeGroup.MachineConfigPoolSelector)
			// handled by validation
			if err != nil {
				klog.Errorf("bad node group machine config pool selector %q", nodeGroup.MachineConfigPoolSelector.String())
				continue
			}

			mcpLabels := labels.Set(mcp.Labels)
			if selector.Matches(mcpLabels) {
				found = true
				result = append(result, mcp)
			}
		}

		if !found {
			return nil, fmt.Errorf("failed to find MachineConfigPool for the node group with the selector %q", nodeGroup.MachineConfigPoolSelector.String())
		}
	}

	return result, nil
}

func isMachineConfigPoolsUpdated(instance *nropv1alpha1.NUMAResourcesOperator, mcps []*machineconfigv1.MachineConfigPool) bool {
	instance.Status.MachineConfigPools = []nropv1alpha1.MachineConfigPool{}
	for _, mcp := range mcps {
		// update MCP conditions under the NRO
		instance.Status.MachineConfigPools = append(instance.Status.MachineConfigPools, nropv1alpha1.MachineConfigPool{
			Name:       mcp.Name,
			Conditions: mcp.Status.Conditions,
		})

		mcName := rte.GetMachineConfigName(instance.Name, mcp.Name)
		existing := false
		for _, s := range mcp.Status.Configuration.Source {
			if s.Name == mcName {
				existing = true
				break
			}
		}

		// the Machine Config Pool still did not apply the machine config wait for one minute
		if !existing || machineconfigv1.IsMachineConfigPoolConditionFalse(mcp.Status.Conditions, machineconfigv1.MachineConfigPoolUpdated) {
			return false
		}
	}

	return true
}
