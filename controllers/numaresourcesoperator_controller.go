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
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
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
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
	"github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	apistate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/api"
	rtestate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/rte"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/pkg/validation"
)

const numaResourcesRetryPeriod = 1 * time.Minute

// NUMAResourcesOperatorReconciler reconciles a NUMAResourcesOperator object
type NUMAResourcesOperatorReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Platform        platform.Platform
	APIManifests    apimanifests.Manifests
	RTEManifests    rtemanifests.Manifests
	Namespace       string
	ImageSpec       string
	ImagePullPolicy corev1.PullPolicy
	Recorder        record.EventRecorder
}

// TODO: narrow down

// Namespace Scoped
// TODO

// Cluster Scoped
//+kubebuilder:rbac:groups=topology.node.k8s.io,resources=noderesourcetopologies,verbs=get;list;create;update
//+kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=list
//+kubebuilder:rbac:groups=config.openshift.io,resources=clusteroperators,verbs=get
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

	if req.Name != objectnames.DefaultNUMAResourcesOperatorCrName {
		message := fmt.Sprintf("incorrect NUMAResourcesOperator resource name: %s", instance.Name)
		return r.updateStatus(ctx, instance, status.ConditionDegraded, status.ConditionTypeIncorrectNUMAResourcesOperatorResourceName, message)
	}

	if err := validation.NodeGroups(instance.Spec.NodeGroups); err != nil {
		return r.updateStatus(ctx, instance, status.ConditionDegraded, validation.NodeGroupsError, err.Error())
	}

	trees, err := machineconfigpools.GetTreesByNodeGroup(ctx, r.Client, instance.Spec.NodeGroups)
	if err != nil {
		return r.updateStatus(ctx, instance, status.ConditionDegraded, validation.NodeGroupsError, err.Error())
	}

	if err := validation.MachineConfigPoolDuplicates(trees); err != nil {
		return r.updateStatus(ctx, instance, status.ConditionDegraded, validation.NodeGroupsError, err.Error())
	}

	result, condition, err := r.reconcileResource(ctx, instance, trees)
	if condition != "" {
		// TODO: use proper reason
		reason, message := condition, messageFromError(err)
		_, _ = r.updateStatus(ctx, instance, condition, reason, message)
	}
	return result, err
}

func (r *NUMAResourcesOperatorReconciler) updateStatus(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator, condition string, reason string, message string) (ctrl.Result, error) {
	klog.InfoS("updateStatus", "condition", condition, "reason", reason, "message", message)

	if _, err := updateStatus(ctx, r.Client, instance, condition, reason, message); err != nil {
		klog.InfoS("Failed to update numaresourcesoperator status", "Desired condition", status.ConditionDegraded, "error", err)
		return ctrl.Result{}, err
	}
	// we do not return an error here because to pass the validation error a user will need to update NRO CR
	// that will anyway initiate to reconcile loop
	return ctrl.Result{}, nil
}

func updateStatus(ctx context.Context, cli client.Client, rte *nropv1alpha1.NUMAResourcesOperator, condition string, reason string, message string) (bool, error) {

	conditions, ok := status.GetUpdatedConditions(rte.Status.Conditions, condition, reason, message)
	if !ok {
		return false, nil
	}
	rte.Status.Conditions = conditions

	if err := cli.Status().Update(ctx, rte); err != nil {
		return false, errors.Wrapf(err, "could not update status for object %s", client.ObjectKeyFromObject(rte))
	}
	return true, nil
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

func (r *NUMAResourcesOperatorReconciler) reconcileResource(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator, trees []machineconfigpools.NodeGroupTree) (ctrl.Result, string, error) {
	var err error
	err = r.syncNodeResourceTopologyAPI()
	if err != nil {
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "FailedCRDInstall", "Failed to install Node Resource Topology CRD: %v", err)
		return ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "FailedAPISync")
	}
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "SuccessfulCRDInstall", "Node Resource Topology CRD installed")

	if r.Platform == platform.OpenShift {
		// we need to create machine configs first and wait for the MachineConfigPool updates
		// before creating additional components
		if err := r.syncMachineConfigs(ctx, instance, trees); err != nil {
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "FailedMCSync", "Failed to set up machine configuration for worker nodes: %v", err)
			return ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "failed to sync machine configs")
		}
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "SuccessfulMCSync", "Enabled machine configuration for worker nodes")

		// MCO need to update SELinux context and other stuff, and need to trigger a reboot.
		// It can take a while.
		if allMCPsUpdated := r.syncMachineConfigPoolsStatuses(instance, trees); !allMCPsUpdated {
			// the Machine Config Pool still did not apply the machine config, wait for one minute
			return ctrl.Result{RequeueAfter: numaResourcesRetryPeriod}, status.ConditionProgressing, nil
		}
	}

	daemonSetsInfo, err := r.syncNUMAResourcesOperatorResources(ctx, instance, trees)
	if err != nil {
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "FailedRTECreate", "Failed to create Resource-Topology-Exporter DaemonSets: %v", err)
		return ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "FailedRTESync")
	}
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "SuccessfulRTECreate", "Created Resource-Topology-Exporter DaemonSets")

	instance.Status.DaemonSets = []nropv1alpha1.NamespacedName{}
	for _, nname := range daemonSetsInfo {
		ds := appsv1.DaemonSet{}
		dsKey := client.ObjectKey{
			Namespace: nname.Namespace,
			Name:      nname.Name,
		}
		err = r.Client.Get(ctx, dsKey, &ds)
		if err != nil {
			return ctrl.Result{}, status.ConditionDegraded, err
		}

		if !isDaemonSetReady(&ds) {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, status.ConditionProgressing, nil
		}
		instance.Status.DaemonSets = append(instance.Status.DaemonSets, nname)
	}

	return ctrl.Result{}, status.ConditionAvailable, nil
}

func (r *NUMAResourcesOperatorReconciler) syncNodeResourceTopologyAPI() error {
	klog.V(4).Info("APISync start")
	defer klog.V(4).Info("APISync stop")

	existing := apistate.FromClient(context.TODO(), r.Client, r.Platform, r.APIManifests)

	for _, objState := range existing.State(r.APIManifests) {
		if _, err := apply.ApplyObject(context.TODO(), r.Client, objState); err != nil {
			return errors.Wrapf(err, "could not create %s", objState.Desired.GetObjectKind().GroupVersionKind().String())
		}
	}
	return nil
}

func (r *NUMAResourcesOperatorReconciler) syncMachineConfigs(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator, trees []machineconfigpools.NodeGroupTree) error {
	klog.V(4).Info("Machine Config Sync start")
	defer klog.V(4).Info("Machine Config Sync stop")

	existing := rtestate.FromClient(ctx, r.Client, r.Platform, r.RTEManifests, instance, trees, r.Namespace)

	// create MC objects first
	for _, objState := range existing.MachineConfigsState(r.RTEManifests) {
		if err := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme); err != nil {
			return errors.Wrapf(err, "failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
		}

		if err := validateMachineConfigLabels(objState.Desired, trees); err != nil {
			return errors.Wrapf(err, "machine conig %q labels validation failed", objState.Desired.GetName())
		}

		_, err := apply.ApplyObject(ctx, r.Client, objState)
		if err != nil {
			return errors.Wrapf(err, "could not apply (%s) %s/%s", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
	}

	return nil
}

func (r *NUMAResourcesOperatorReconciler) syncMachineConfigPoolsStatuses(instance *nropv1alpha1.NUMAResourcesOperator, trees []machineconfigpools.NodeGroupTree) bool {
	klog.V(4).Info("Machine Config Status Sync start")
	defer klog.V(4).Info("Machine Config Status Sync stop")

	instance.Status.MachineConfigPools = []nropv1alpha1.MachineConfigPool{}
	for _, tree := range trees {
		for _, mcp := range tree.MachineConfigPools {
			// update MCP conditions under the NRO
			instance.Status.MachineConfigPools = append(instance.Status.MachineConfigPools, nropv1alpha1.MachineConfigPool{
				Name:       mcp.Name,
				Conditions: mcp.Status.Conditions,
			})

			isUpdated := IsMachineConfigPoolUpdated(instance.Name, mcp)
			klog.V(5).InfoS("Machine Config Pool state", "name", mcp.Name, "instance", instance.Name, "updated", isUpdated)

			if !isUpdated {
				return false
			}
		}
	}
	return true
}

func (r *NUMAResourcesOperatorReconciler) syncNUMAResourcesOperatorResources(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator, trees []machineconfigpools.NodeGroupTree) ([]nropv1alpha1.NamespacedName, error) {
	klog.V(4).Info("RTESync start")
	defer klog.V(4).Info("RTESync stop")

	errorList := r.deleteUnusedDaemonSets(ctx, instance, trees)
	if len(errorList) > 0 {
		klog.ErrorS(fmt.Errorf("failed to delete unused daemonsets"), "errors", errorList)
	}

	errorList = r.deleteUnusedMachineConfigs(ctx, instance, trees)
	if len(errorList) > 0 {
		klog.ErrorS(fmt.Errorf("failed to delete unused machineconfigs"), "errors", errorList)
	}

	var err error
	var daemonSetsNName []nropv1alpha1.NamespacedName

	err = rteupdate.DaemonSetUserImageSettings(r.RTEManifests.DaemonSet, instance.Spec.ExporterImage, r.ImageSpec, r.ImagePullPolicy)
	if err != nil {
		return daemonSetsNName, err
	}

	err = rteupdate.DaemonSetPauseContainerSettings(r.RTEManifests.DaemonSet)
	if err != nil {
		return daemonSetsNName, err
	}

	err = loglevel.UpdatePodSpec(&r.RTEManifests.DaemonSet.Spec.Template.Spec, instance.Spec.LogLevel)
	if err != nil {
		return daemonSetsNName, err
	}

	// ConfigMap should be provided by the kubeletconfig reconciliation loop
	if r.RTEManifests.ConfigMap != nil {
		cmHash, err := hash.ComputeCurrentConfigMap(ctx, r.Client, r.RTEManifests.ConfigMap)
		if err != nil {
			return daemonSetsNName, err
		}
		rteupdate.DaemonSetHashAnnotation(r.RTEManifests.DaemonSet, cmHash)
	}

	existing := rtestate.FromClient(ctx, r.Client, r.Platform, r.RTEManifests, instance, trees, r.Namespace)
	for _, objState := range existing.State(r.RTEManifests, daemonsetUpdater) {
		if err := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme); err != nil {
			return nil, errors.Wrapf(err, "Failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
		obj, err := apply.ApplyObject(ctx, r.Client, objState)
		if err != nil {
			return nil, errors.Wrapf(err, "could not apply (%s) %s/%s", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName())
		}

		if nname, ok := rtestate.DaemonSetNamespacedNameFromObject(obj); ok {
			daemonSetsNName = append(daemonSetsNName, nname)
		}
	}
	return daemonSetsNName, nil
}

func (r *NUMAResourcesOperatorReconciler) deleteUnusedDaemonSets(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator, trees []machineconfigpools.NodeGroupTree) []error {
	klog.V(3).Info("Delete Daemonsets start")
	var errors []error
	var daemonSetList appsv1.DaemonSetList
	if err := r.List(ctx, &daemonSetList, &client.ListOptions{Namespace: instance.Namespace}); err != nil {
		klog.ErrorS(err, "error while getting Daemonset list")
		return append(errors, err)
	}

	expectedDaemonSetNames := sets.NewString()
	for _, tree := range trees {
		for _, mcp := range tree.MachineConfigPools {
			expectedDaemonSetNames = expectedDaemonSetNames.Insert(objectnames.GetComponentName(instance.Name, mcp.Name))
		}
	}

	for _, ds := range daemonSetList.Items {
		if !expectedDaemonSetNames.Has(ds.Name) {
			if isOwnedBy(ds.GetObjectMeta(), instance) {

				if err := r.Client.Delete(ctx, &ds); err != nil {
					klog.ErrorS(err, "error while deleting daemonset", "DaemonSet", ds.Name)
					errors = append(errors, err)
				} else {
					klog.V(3).InfoS("Daemonset deleted", "name", ds.Name)
				}
			}
		}
	}
	return errors
}

func (r *NUMAResourcesOperatorReconciler) deleteUnusedMachineConfigs(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator, trees []machineconfigpools.NodeGroupTree) []error {
	klog.V(3).Info("Delete Machineconfigs start")
	var errors []error
	var machineConfigList machineconfigv1.MachineConfigList
	if err := r.List(ctx, &machineConfigList); err != nil {
		klog.ErrorS(err, "error while getting MachineConfig list")
		return append(errors, err)
	}

	expectedMachineConfigNames := sets.NewString()
	for _, tree := range trees {
		for _, mcp := range tree.MachineConfigPools {
			expectedMachineConfigNames = expectedMachineConfigNames.Insert(objectnames.GetMachineConfigName(instance.Name, mcp.Name))
		}
	}

	for _, mc := range machineConfigList.Items {
		if !expectedMachineConfigNames.Has(mc.Name) {
			if isOwnedBy(mc.GetObjectMeta(), instance) {
				if err := r.Client.Delete(ctx, &mc); err != nil {
					klog.ErrorS(err, "error while deleting machineconfig", "MachineConfig", mc.Name)
					errors = append(errors, err)
				} else {
					klog.V(3).InfoS("Machineconfig deleted", "name", mc.Name)
				}
			}
		}
	}
	return errors
}

func isOwnedBy(element metav1.Object, owner metav1.Object) bool {
	for _, ref := range element.GetOwnerReferences() {
		if ref.UID == owner.GetUID() {
			return true
		}
	}
	return false
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
		Owns(&corev1.ServiceAccount{}, builder.WithPredicates(p)).
		Owns(&rbacv1.RoleBinding{}, builder.WithPredicates(p)).
		Owns(&rbacv1.Role{}, builder.WithPredicates(p)).
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

func IsMachineConfigPoolUpdated(instanceName string, mcp *machineconfigv1.MachineConfigPool) bool {
	existing := isMachineConfigExists(instanceName, mcp)

	// the Machine Config Pool still did not apply the machine config wait for one minute
	if !existing || machineconfigv1.IsMachineConfigPoolConditionFalse(mcp.Status.Conditions, machineconfigv1.MachineConfigPoolUpdated) {
		return false
	}

	return true
}

func IsMachineConfigPoolUpdatedAfterDeletion(instanceName string, mcp *machineconfigv1.MachineConfigPool) bool {
	existing := isMachineConfigExists(instanceName, mcp)

	// the Machine Config Pool still has the machine config return false
	if existing || machineconfigv1.IsMachineConfigPoolConditionFalse(mcp.Status.Conditions, machineconfigv1.MachineConfigPoolUpdated) {
		return false
	}

	return true
}

func isMachineConfigExists(instanceName string, mcp *machineconfigv1.MachineConfigPool) bool {
	mcName := objectnames.GetMachineConfigName(instanceName, mcp.Name)
	for _, s := range mcp.Status.Configuration.Source {
		if s.Name == mcName {
			return true
		}
	}
	return false
}

func validateMachineConfigLabels(mc client.Object, trees []machineconfigpools.NodeGroupTree) error {
	mcLabels := mc.GetLabels()
	v, ok := mcLabels[rtestate.MachineConfigLabelKey]
	// the machine config does not have generated label, meaning the machine config pool has the matchLabels under
	// the machine config selector, no need to validate
	if !ok {
		return nil
	}

	for _, tree := range trees {
		for _, mcp := range tree.MachineConfigPools {
			if v != mcp.Name {
				continue
			}

			mcLabels := labels.Set(mcLabels)
			mcSelector, err := metav1.LabelSelectorAsSelector(mcp.Spec.MachineConfigSelector)
			if err != nil {
				return fmt.Errorf("failed to represent machine config pool %q machine config selector as selector: %w", mcp.Name, err)
			}

			if !mcSelector.Matches(mcLabels) {
				return fmt.Errorf("machine config %q labels does not match the machine config pool %q machine config selector", mc.GetName(), mcp.Name)
			}
		}
	}
	return nil
}

func daemonsetUpdater(gdm *rtestate.GeneratedDesiredManifest) error {
	// on kubernetes we can just mount the kubeletconfig (no SCC/Selinux),
	// so handling the kubeletconfig configmap is not needed at all.
	// We cannot do this at GetManifests time because we need to mount
	// a specific configmap for each daemonset, whose name we know only
	// when we instantiate the daemonset from the MCP.
	if gdm.ClusterPlatform != platform.OpenShift {
		klog.V(5).InfoS("DaemonSet update: unsupported platform", "platform", gdm.ClusterPlatform)
		// nothing to do!
		return nil
	}
	err := rteupdate.ContainerConfig(gdm.DaemonSet, gdm.DaemonSet.Name)
	if err != nil {
		// intentionally info because we want to keep going
		klog.V(5).InfoS("DaemonSet update: cannot update config", "daemonset", gdm.DaemonSet.Name, "error", err)
		return err
	}

	if !isPodFingerprintEnabled(gdm.NodeGroup) {
		klog.V(5).InfoS("DaemonSet update: pod fingerprinting not enabled", "daemonset", gdm.DaemonSet.Name)
		return nil
	}
	return rteupdate.DaemonSetArgs(gdm.DaemonSet)
}

func isPodFingerprintEnabled(ng *nropv1alpha1.NodeGroup) bool {
	// this feature is supposed to be enabled most of the times,
	// so we turn on by default and we offer a way to disable it.
	if ng == nil {
		return false
	}
	if ng.DisablePodsFingerprinting == nil {
		return false
	}
	if *ng.DisablePodsFingerprinting {
		return false
	}
	return true
}

func isDaemonSetReady(ds *appsv1.DaemonSet) bool {
	ok := (ds.Status.DesiredNumberScheduled > 0 && ds.Status.DesiredNumberScheduled == ds.Status.NumberReady)
	klog.V(5).InfoS("daemonset", "namespace", ds.Namespace, "name", ds.Name, "desired", ds.Status.DesiredNumberScheduled, "current", ds.Status.CurrentNumberScheduled, "ready", ds.Status.NumberReady)
	return ok
}
