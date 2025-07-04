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
	"fmt"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
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

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	securityv1 "github.com/openshift/api/security/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"
	k8swgrteupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate/rte"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/api/v1/helper/namespacedname"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	"github.com/openshift-kni/numaresources-operator/internal/dangling"
	intreconcile "github.com/openshift-kni/numaresources-operator/internal/reconcile"
	"github.com/openshift-kni/numaresources-operator/internal/relatedobjects"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	"github.com/openshift-kni/numaresources-operator/pkg/images"
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	apistate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/api"
	rtestate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/rte"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/pkg/status/conditioninfo"
	"github.com/openshift-kni/numaresources-operator/pkg/validation"
	"github.com/openshift-kni/numaresources-operator/rte/pkg/config"
)

const numaResourcesRetryPeriod = 1 * time.Minute

// poolDaemonSet a struct to hold the target MCP of a configured node group and its created respective RTE daemonset
type poolDaemonSet struct {
	PoolName  string
	DaemonSet nropv1.NamespacedName
}

// NUMAResourcesOperatorReconciler reconciles a NUMAResourcesOperator object
type NUMAResourcesOperatorReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Platform        platform.Platform
	APIManifests    apimanifests.Manifests
	RTEManifests    rtestate.Manifests
	Namespace       string
	Images          images.Data
	ImagePullPolicy corev1.PullPolicy
	Recorder        record.EventRecorder
	ForwardMCPConds bool
}

// TODO: narrow down

// Namespace Scoped
//+kubebuilder:rbac:groups="",resources=services,verbs=*,namespace="numaresources"

// Cluster Scoped
//+kubebuilder:rbac:groups=topology.node.k8s.io,resources=noderesourcetopologies,verbs=get;list;create;update
//+kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=list
//+kubebuilder:rbac:groups=config.openshift.io,resources=clusteroperators,verbs=get
//+kubebuilder:rbac:groups=config.openshift.io,resources=infrastructures,verbs=get
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
//+kubebuilder:rbac:groups="",resources=nodes,verbs=list
//+kubebuilder:rbac:groups=nodetopology.openshift.io,resources=numaresourcesoperators,verbs=*
//+kubebuilder:rbac:groups=nodetopology.openshift.io,resources=numaresourcesoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nodetopology.openshift.io,resources=numaresourcesoperators/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *NUMAResourcesOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.V(3).InfoS("Starting NUMAResourcesOperator reconcile loop", "object", req.NamespacedName)
	defer klog.V(3).InfoS("Finish NUMAResourcesOperator reconcile loop", "object", req.NamespacedName)

	instance := &nropv1.NUMAResourcesOperator{}
	err := r.Get(ctx, req.NamespacedName, instance)
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
		err := fmt.Errorf("incorrect NUMAResourcesOperator resource name: %s", instance.Name)
		return r.degradeStatus(ctx, instance, status.ConditionTypeIncorrectNUMAResourcesOperatorResourceName, err)
	}

	if annotations.IsPauseReconciliationEnabled(instance.Annotations) {
		klog.V(2).InfoS("Pause reconciliation enabled", "object", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	if err := validation.NodeGroups(instance.Spec.NodeGroups, r.Platform); err != nil {
		return r.degradeStatus(ctx, instance, validation.NodeGroupsError, err)
	}

	trees, err := getTreesByNodeGroup(ctx, r.Client, instance.Spec.NodeGroups, r.Platform)
	if err != nil {
		return r.degradeStatus(ctx, instance, validation.NodeGroupsError, err)
	}

	tolerable, err := validation.NodeGroupsTree(instance, trees, r.Platform)
	if err != nil {
		return r.degradeStatus(ctx, instance, validation.NodeGroupsError, err)
	}

	for idx := range trees {
		conf := trees[idx].NodeGroup.NormalizeConfig()
		trees[idx].NodeGroup.Config = &conf
	}

	curStatus := instance.Status.DeepCopy()

	step := r.reconcileResource(ctx, instance, trees)

	if step.Done() && tolerable != nil {
		return r.degradeStatus(ctx, instance, tolerable.Reason, tolerable.Error)
	}

	if !status.IsUpdatedNUMAResourcesOperator(curStatus, &instance.Status) {
		return step.Result, step.Error
	}

	updErr := r.Client.Status().Update(ctx, instance)
	if updErr != nil {
		klog.InfoS("Failed to update numaresourcesoperator status", "error", updErr)
		return ctrl.Result{}, fmt.Errorf("could not update status for object %s: %w", client.ObjectKeyFromObject(instance), updErr)
	}

	return step.Result, step.Error
}

// updateStatusConditionsIfNeeded returns true if conditions were updated.
func updateStatusConditionsIfNeeded(instance *nropv1.NUMAResourcesOperator, cond conditioninfo.ConditionInfo) {
	if cond.Type == "" { // backward (=legacy) compatibility
		return
	}
	klog.InfoS("updateStatus", "condition", cond.Type, "reason", cond.Reason, "message", cond.Message)
	conditions, ok := status.UpdateConditions(instance.Status.Conditions, cond.Type, cond.Reason, cond.Message)
	if ok {
		instance.Status.Conditions = conditions
	}
}

func (r *NUMAResourcesOperatorReconciler) degradeStatus(ctx context.Context, instance *nropv1.NUMAResourcesOperator, reason string, stErr error) (ctrl.Result, error) {
	info := conditioninfo.DegradedFromError(stErr)
	if reason != "" { // intentionally overwrite
		info.Reason = reason
	}

	updateStatusConditionsIfNeeded(instance, info)
	// TODO: if we keep being degraded, we likely (= if we don't, it's too implicit) keep sending possibly redundant updates to the apiserver

	err := r.Client.Status().Update(ctx, instance)
	if err != nil {
		klog.InfoS("Failed to update numaresourcesoperator status", "error", err)
		return ctrl.Result{}, fmt.Errorf("could not update status for object %s: %w", client.ObjectKeyFromObject(instance), err)
	}

	// we do not return an error here because to pass the validation error a user will need to update NRO CR
	// that will anyway initiate to reconcile loop
	return ctrl.Result{}, nil
}

func (r *NUMAResourcesOperatorReconciler) reconcileResourceAPI(ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) intreconcile.Step {
	if annotations.IsNRTAPIDefinitionCluster(instance.Annotations) {
		// should never happen, so let's be vocal. Very vocal.
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "SkipCRDInstall", "Skipped to install Node Resource Topology CRD: caused by annotation")
		return intreconcile.StepSuccess()
	}
	applied, err := r.syncNodeResourceTopologyAPI(ctx)
	if err != nil {
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "FailedCRDInstall", "Failed to install Node Resource Topology CRD: %v", err)
		err = fmt.Errorf("FailedAPISync: %w", err)
		return intreconcile.StepFailed(err)
	}
	if applied {
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "SuccessfulCRDInstall", "Node Resource Topology CRD installed")
	}
	return intreconcile.StepSuccess()
}

func (r *NUMAResourcesOperatorReconciler) reconcileResourceMachineConfig(ctx context.Context, instance *nropv1.NUMAResourcesOperator, existing *rtestate.ExistingManifests, trees []nodegroupv1.Tree) intreconcile.Step {
	// we need to sync machine configs first and wait for the MachineConfigPool updates
	// before checking additional components for updates
	mcpUpdatedFunc, err := r.syncMachineConfigs(ctx, instance, existing, trees)
	if err != nil {
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "FailedMCSync", "Failed to set up machine configuration for worker nodes: %v", err)
		err = fmt.Errorf("failed to sync machine configs: %w", err)
		return intreconcile.StepFailed(err)
	}
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "SuccessfulMCSync", "Enabled machine configuration for worker nodes")

	// MCO needs to update the SELinux context removal and other stuff, and need to trigger a reboot.
	// It can take a while.
	mcpStatuses, mcpNamePending := syncMachineConfigPoolsStatuses(instance.Name, trees, r.ForwardMCPConds, mcpUpdatedFunc)
	instance.Status.MachineConfigPools = mcpStatuses

	if mcpNamePending != "" {
		// the Machine Config Pool still did not apply the machine config, wait for one minute
		return intreconcile.StepOngoing(numaResourcesRetryPeriod).WithReason("MachineConfigPoolIsUpdating").WithMessage(mcpNamePending + " is updating")
	}
	instance.Status.MachineConfigPools = syncMachineConfigPoolNodeGroupConfigStatuses(instance.Status.MachineConfigPools, trees)

	return intreconcile.StepSuccess()
}

func (r *NUMAResourcesOperatorReconciler) reconcileResourceDaemonSet(ctx context.Context, instance *nropv1.NUMAResourcesOperator, existing *rtestate.ExistingManifests, trees []nodegroupv1.Tree) ([]poolDaemonSet, intreconcile.Step) {
	daemonSetsInfoPerPool, err := r.syncNUMAResourcesOperatorResources(ctx, instance, existing, trees)
	if err != nil {
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "FailedRTECreate", "Failed to create Resource-Topology-Exporter DaemonSets: %v", err)
		err = fmt.Errorf("FailedRTESync: %w", err)
		return nil, intreconcile.StepFailed(err)
	}

	if len(daemonSetsInfoPerPool) == 0 {
		return nil, intreconcile.StepSuccess()
	}

	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "SuccessfulRTECreate", "Created Resource-Topology-Exporter DaemonSets")

	dssWithReadyStatus, dsNamePending, err := r.syncDaemonSetsStatuses(ctx, r.Client, daemonSetsInfoPerPool)
	instance.Status.DaemonSets = dssWithReadyStatus
	instance.Status.RelatedObjects = relatedobjects.ResourceTopologyExporter(r.Namespace, dssWithReadyStatus)
	if err != nil {
		return nil, intreconcile.StepFailed(err)
	}
	if dsNamePending != "" {
		return nil, intreconcile.StepOngoing(5 * time.Second).WithReason("DaemonSetIsUpdating").WithMessage(dsNamePending + " is updating")
	}

	return daemonSetsInfoPerPool, intreconcile.StepSuccess()
}

func (r *NUMAResourcesOperatorReconciler) reconcileResource(ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) intreconcile.Step {
	if step := r.reconcileResourceAPI(ctx, instance, trees); step.EarlyStop() {
		updateStatusConditionsIfNeeded(instance, step.ConditionInfo)
		return step
	}

	existing := rtestate.FromClient(ctx, r.Client, r.Platform, r.RTEManifests, instance, trees, r.Namespace)

	if r.Platform == platform.OpenShift {
		if step := r.reconcileResourceMachineConfig(ctx, instance, existing, trees); step.EarlyStop() {
			updateStatusConditionsIfNeeded(instance, step.ConditionInfo)
			return step
		}
	}

	dsPerPool, step := r.reconcileResourceDaemonSet(ctx, instance, existing, trees)
	if step.EarlyStop() {
		updateStatusConditionsIfNeeded(instance, step.ConditionInfo)
		return step
	}

	// all fields of NodeGroupStatus are required so publish the status only when all daemonset and MCPs are updated which
	// is a certain thing if we got to this point otherwise the function would have returned already
	instance.Status.NodeGroups = syncNodeGroupsStatus(instance, dsPerPool)

	updateStatusConditionsIfNeeded(instance, conditioninfo.Available())
	return intreconcile.Step{
		Result:        ctrl.Result{},
		ConditionInfo: conditioninfo.Available(),
	}
}

func (r *NUMAResourcesOperatorReconciler) syncDaemonSetsStatuses(ctx context.Context, rd client.Reader, daemonSetsInfo []poolDaemonSet) ([]nropv1.NamespacedName, string, error) {
	dssWithReadyStatus := []nropv1.NamespacedName{}
	for _, dsInfo := range daemonSetsInfo {
		ds := appsv1.DaemonSet{}
		dsKey := client.ObjectKey{
			Namespace: dsInfo.DaemonSet.Namespace,
			Name:      dsInfo.DaemonSet.Name,
		}
		err := rd.Get(ctx, dsKey, &ds)
		if err != nil {
			return dssWithReadyStatus, dsKey.String(), err
		}

		if !isDaemonSetReady(&ds) {
			return dssWithReadyStatus, dsKey.String(), nil
		}
		dssWithReadyStatus = append(dssWithReadyStatus, dsInfo.DaemonSet)
	}
	return dssWithReadyStatus, "", nil
}

func syncNodeGroupsStatus(instance *nropv1.NUMAResourcesOperator, dsPerPool []poolDaemonSet) []nropv1.NodeGroupStatus {
	ngStatuses := []nropv1.NodeGroupStatus{}

	if len(instance.Status.MachineConfigPools) == 0 {
		for _, group := range instance.Spec.NodeGroups {
			for _, info := range dsPerPool {
				if *group.PoolName != info.PoolName {
					continue
				}
				status := nropv1.NodeGroupStatus{
					PoolName:  info.PoolName,
					Config:    *group.Config,
					DaemonSet: info.DaemonSet,
				}
				ngStatuses = append(ngStatuses, status)
			}
		}
	}

	for _, mcp := range instance.Status.MachineConfigPools {
		for _, info := range dsPerPool {
			if mcp.Name != info.PoolName {
				continue
			}

			status := nropv1.NodeGroupStatus{
				PoolName:  mcp.Name,
				Config:    *mcp.Config,
				DaemonSet: info.DaemonSet,
			}
			ngStatuses = append(ngStatuses, status)
		}
	}
	return ngStatuses
}

func (r *NUMAResourcesOperatorReconciler) syncNodeResourceTopologyAPI(ctx context.Context) (bool, error) {
	klog.V(4).Info("APISync start")
	defer klog.V(4).Info("APISync stop")

	existing := apistate.FromClient(ctx, r.Client, r.Platform, r.APIManifests)

	var err error
	var updatedCount int
	objStates := existing.State(r.APIManifests)
	for _, objState := range objStates {
		_, updated, err2 := apply.ApplyObject(ctx, r.Client, objState)
		if err2 != nil {
			err = fmt.Errorf("could not create %s: %w", objState.Desired.GetObjectKind().GroupVersionKind().String(), err2)
			break
		}
		if !updated {
			continue
		}
		updatedCount++
	}
	return (updatedCount == len(objStates)), err
}

func (r *NUMAResourcesOperatorReconciler) syncMachineConfigs(ctx context.Context, instance *nropv1.NUMAResourcesOperator, existing *rtestate.ExistingManifests, trees []nodegroupv1.Tree) (rtestate.MCPWaitForUpdatedFunc, error) {
	klog.V(4).InfoS("Machine Config Sync start", "trees", len(trees))
	defer klog.V(4).Info("Machine Config Sync stop")

	var err error
	// Since 4.18 we're using a built-in SELinux policy,
	// so the MachineConfig which applies the custom policy is no longer necessary.
	// In case of operator upgrade from 4.1X → 4.18, it's necessary to remove the old MachineConfig,
	// unless an emergency annotation is provided which forces the operator to use custom policy

	objStates, waitFunc := existing.MachineConfigsState(r.RTEManifests)
	for _, objState := range objStates {
		klog.InfoS("objState", "desired", objState.Desired, "existing", objState.Existing, "createOrUpdate", objState.IsCreateOrUpdate())
		if objState.IsCreateOrUpdate() {
			if err2 := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme); err2 != nil {
				err = fmt.Errorf("failed to set controller reference to %s %s: %w", objState.Desired.GetNamespace(), objState.Desired.GetName(), err2)
				break
			}

			if err2 := validateMachineConfigLabels(objState.Desired, trees); err2 != nil {
				err = err2
				break
			}
		}
		_, _, err2 := apply.ApplyState(ctx, r.Client, objState)
		if err2 != nil {
			err = err2
			break
		}
	}
	return waitFunc, err
}

func syncMachineConfigPoolsStatuses(instanceName string, trees []nodegroupv1.Tree, forwardMCPConds bool, updatedFunc rtestate.MCPWaitForUpdatedFunc) ([]nropv1.MachineConfigPool, string) {
	klog.V(4).InfoS("Machine Config Status Sync start", "trees", len(trees))
	defer klog.V(4).Info("Machine Config Status Sync stop")

	mcpStatuses := []nropv1.MachineConfigPool{}
	for _, tree := range trees {
		for _, mcp := range tree.MachineConfigPools {
			mcpStatuses = append(mcpStatuses, extractMCPStatus(mcp, forwardMCPConds))

			isUpdated := updatedFunc(instanceName, mcp)
			klog.V(5).InfoS("Machine Config Pool state", "name", mcp.Name, "instance", instanceName, "updated", isUpdated)

			if !isUpdated {
				return mcpStatuses, mcp.Name
			}
		}
	}
	return mcpStatuses, ""
}

func extractMCPStatus(mcp *machineconfigv1.MachineConfigPool, forwardMCPConds bool) nropv1.MachineConfigPool {
	mcpStatus := nropv1.MachineConfigPool{
		Name: mcp.Name,
	}
	if !forwardMCPConds {
		return mcpStatus
	}
	mcpStatus.Conditions = mcp.Status.Conditions
	return mcpStatus
}

func syncMachineConfigPoolNodeGroupConfigStatuses(mcpStatuses []nropv1.MachineConfigPool, trees []nodegroupv1.Tree) []nropv1.MachineConfigPool {
	klog.V(4).InfoS("Machine Config Pool Node Group Status Sync start", "mcpStatuses", len(mcpStatuses), "trees", len(trees))
	defer klog.V(4).Info("Machine Config Pool Node Group Status Sync stop")

	updatedMcpStatuses := []nropv1.MachineConfigPool{}
	for _, tree := range trees {
		klog.V(5).InfoS("Machine Config Pool Node Group tree update", "mcps", len(tree.MachineConfigPools))

		for _, mcp := range tree.MachineConfigPools {
			mcpStatus := getMachineConfigPoolStatusByName(mcpStatuses, mcp.Name)

			var confSource string
			if tree.NodeGroup != nil && tree.NodeGroup.Config != nil {
				confSource = "spec"
				mcpStatus.Config = tree.NodeGroup.Config.DeepCopy()
			} else {
				confSource = "default"
				ngc := nropv1.DefaultNodeGroupConfig()
				mcpStatus.Config = &ngc
			}

			klog.V(6).InfoS("Machine Config Pool Node Group updated status config", "mcp", mcp.Name, "source", confSource, "data", mcpStatus.Config.ToString())

			updatedMcpStatuses = append(updatedMcpStatuses, mcpStatus)
		}
	}
	return updatedMcpStatuses
}

func getMachineConfigPoolStatusByName(mcpStatuses []nropv1.MachineConfigPool, name string) nropv1.MachineConfigPool {
	for _, mcpStatus := range mcpStatuses {
		if mcpStatus.Name == name {
			return mcpStatus
		}
	}
	return nropv1.MachineConfigPool{Name: name}
}

func (r *NUMAResourcesOperatorReconciler) syncNUMAResourcesOperatorResources(ctx context.Context, instance *nropv1.NUMAResourcesOperator, existing *rtestate.ExistingManifests, trees []nodegroupv1.Tree) ([]poolDaemonSet, error) {
	klog.V(4).InfoS("RTESync start", "trees", len(trees))
	defer klog.V(4).Info("RTESync stop")

	err := dangling.DeleteUnusedDaemonSets(r.Client, ctx, instance, trees)
	if err != nil {
		klog.ErrorS(err, "failed to deleted unused daemonsets")
	}

	if r.Platform == platform.OpenShift {
		err = dangling.DeleteUnusedMachineConfigs(r.Client, ctx, instance, trees)
		if err != nil {
			klog.ErrorS(err, "failed to deleted unused machineconfigs")
		}
	}

	// using a slice of poolDaemonSet instead of a map because Go maps assignment order is not consistent and non-deterministic
	dsPoolPairs := []poolDaemonSet{}
	// mutatedManifests are rendered manifests which their base derives from the existing resources
	// found on the cluster.
	// those manifests were mutated by the API server and other controllers running on the cluster, hence the name.
	// this way it minimizes the diffs between the desired and existing state only to the fields we care about.
	// in case the manifest/resource was not found on the cluster (should be created)
	// it uses the local stored manifest.
	mutatedManifests, err := rtestate.MutatedFromExisting(existing, r.RTEManifests, r.Namespace)
	if err != nil {
		return dsPoolPairs, err
	}
	err = rteupdate.DaemonSetUserImageSettings(mutatedManifests.Core.DaemonSet, instance.Spec.ExporterImage, r.Images.Preferred(), r.ImagePullPolicy)
	if err != nil {
		return dsPoolPairs, err
	}

	err = rteupdate.DaemonSetPauseContainerSettings(mutatedManifests.Core.DaemonSet)
	if err != nil {
		return dsPoolPairs, err
	}

	err = loglevel.UpdatePodSpec(&mutatedManifests.Core.DaemonSet.Spec.Template.Spec, manifests.ContainerNameRTE, instance.Spec.LogLevel)
	if err != nil {
		return dsPoolPairs, err
	}
	rteupdate.SecurityContextConstraint(mutatedManifests.Core.SecurityContextConstraint, true) // force to legacy context
	// SCC v2 needs no updates

	existing = existing.WithManifestsUpdater(func(poolName string, gdm *rtestate.GeneratedDesiredManifest) error {
		err := daemonsetUpdater(poolName, gdm)
		if err != nil {
			return err
		}
		dsPoolPairs = append(dsPoolPairs, poolDaemonSet{poolName, namespacedname.FromObject(gdm.DaemonSet)})
		return nil
	})

	for _, objState := range existing.State(mutatedManifests) {
		if objState.Error != nil {
			// We are likely in the bootstrap scenario. In this case, which is expected once, everything is fine.
			// If it happens past bootstrap, still carry on. We know what to do, and we do want to enforce the desired state.
			klog.Warningf("error loading object: %v", objState.Error)
		}
		if objState.UpdateError != nil {
			// this is an internal error. Should not happen. But if it happen, we don't want to send garbage to the cluster, so we abort
			return nil, fmt.Errorf("failed to update (%s) %s/%s: %w", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName(), err)
		}
		err := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme)
		if err != nil {
			return nil, fmt.Errorf("failed to set controller reference to %s %s: %w", objState.Desired.GetNamespace(), objState.Desired.GetName(), err)
		}
		_, _, err = apply.ApplyObject(ctx, r.Client, objState)
		if err != nil {
			return nil, fmt.Errorf("failed to apply (%s) %s/%s: %w", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName(), err)
		}
	}

	if len(dsPoolPairs) < len(trees) {
		klog.Warningf("daemonset and tree size mismatch: expected %d got in daemonsets %d", len(trees), len(dsPoolPairs))
	}
	return dsPoolPairs, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NUMAResourcesOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// we want to initiate reconcile loop only on change under labels or spec of the object
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

	configMapPredicates := predicate.NewPredicateFuncs(func(object client.Object) bool {
		cm := object.(*corev1.ConfigMap)
		cmLabels := cm.GetLabels()
		// return only configmap with the operator name label
		_, ok := cmLabels[config.LabelOperatorName]
		return ok
	})

	b := ctrl.NewControllerManagedBy(mgr).For(&nropv1.NUMAResourcesOperator{})
	if r.Platform == platform.OpenShift {
		b.Watches(
			&machineconfigv1.MachineConfigPool{},
			handler.EnqueueRequestsFromMapFunc(r.mcpToNUMAResourceOperator),
			builder.WithPredicates(mcpPredicates)).
			Owns(&securityv1.SecurityContextConstraints{}).
			Owns(&machineconfigv1.MachineConfig{}, builder.WithPredicates(p))
	}
	return b.Watches(&corev1.ConfigMap{},
		handler.EnqueueRequestsFromMapFunc(r.configMapToNUMAResourceOperator),
		builder.WithPredicates(configMapPredicates)).
		Owns(&apiextensionv1.CustomResourceDefinition{}).
		Owns(&corev1.ServiceAccount{}, builder.WithPredicates(p)).
		Owns(&rbacv1.RoleBinding{}, builder.WithPredicates(p)).
		Owns(&rbacv1.Role{}, builder.WithPredicates(p)).
		Owns(&appsv1.DaemonSet{}, builder.WithPredicates(p)).
		Complete(r)
}

func (r *NUMAResourcesOperatorReconciler) mcpToNUMAResourceOperator(ctx context.Context, mcpObj client.Object) []reconcile.Request {
	mcp := &machineconfigv1.MachineConfigPool{}

	key := client.ObjectKey{
		Namespace: mcpObj.GetNamespace(),
		Name:      mcpObj.GetName(),
	}

	if err := r.Get(ctx, key, mcp); err != nil {
		klog.Errorf("failed to get the machine config pool %+v", key)
		return nil
	}

	nros := &nropv1.NUMAResourcesOperatorList{}
	if err := r.List(ctx, nros); err != nil {
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

func (r *NUMAResourcesOperatorReconciler) configMapToNUMAResourceOperator(ctx context.Context, cmObj client.Object) []reconcile.Request {
	cm := &corev1.ConfigMap{}

	key := client.ObjectKey{
		Namespace: cmObj.GetNamespace(),
		Name:      cmObj.GetName(),
	}

	if err := r.Get(ctx, key, cm); err != nil {
		klog.Errorf("failed to get configmap %+v; %v", key, err)
		return nil
	}
	name, ok := cm.Labels[config.LabelOperatorName]
	if !ok {
		return nil
	}
	key = client.ObjectKey{
		Name: name,
	}
	nro := &nropv1.NUMAResourcesOperator{}
	if err := r.Get(ctx, key, nro); err != nil {
		klog.Error("failed to get numa-resources operator")
		return nil
	}

	return []reconcile.Request{{NamespacedName: client.ObjectKey{
		Name: nro.Name,
	}}}
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

func validateMachineConfigLabels(mc client.Object, trees []nodegroupv1.Tree) error {
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

func daemonsetUpdater(poolName string, gdm *rtestate.GeneratedDesiredManifest) error {
	rteupdate.DaemonSetTolerations(gdm.DaemonSet, gdm.NodeGroup.Config.Tolerations)

	err := rteupdate.DaemonSetArgs(gdm.DaemonSet, *gdm.NodeGroup.Config)
	if err != nil {
		klog.V(5).InfoS("DaemonSet update: cannot update arguments", "pool name", poolName, "daemonset", gdm.DaemonSet.Name, "error", err)
		return err
	}

	// on kubernetes we can just mount the kubeletconfig (no SCC/Selinux),
	// so handling the kubeletconfig configmap is not needed at all.
	// We cannot do this at GetManifests time because we need to mount
	// a specific configmap for each daemonset, whose name we know only
	// when we instantiate the daemonset from the MCP.
	if gdm.ClusterPlatform != platform.OpenShift && gdm.ClusterPlatform != platform.HyperShift {
		klog.V(5).InfoS("DaemonSet update: unsupported platform", "pool name", poolName, "platform", gdm.ClusterPlatform)
		// nothing to do!
		return nil
	}
	err = rteupdate.ContainerConfig(gdm.DaemonSet, gdm.DaemonSet.Name)
	if err != nil {
		// intentionally info because we want to keep going
		klog.V(5).InfoS("DaemonSet update: cannot update config", "pool name", poolName, "daemonset", gdm.DaemonSet.Name, "error", err)
		return err
	}
	if gdm.ClusterPlatform != platform.Kubernetes {
		klog.V(4).InfoS("DaemonSet update: selinux options", "contextType", gdm.SecOpts.SELinuxContextType, "contextName", gdm.SecOpts.SecurityContextName)
		k8swgrteupdate.SecurityContextWithOpts(gdm.DaemonSet, gdm.SecOpts)
	}
	// it's possible that the hash will be empty if kubelet controller hasn't created a configmap
	rteupdate.DaemonSetHashAnnotation(gdm.DaemonSet, gdm.RTEConfigHash)
	return nil
}

func isDaemonSetReady(ds *appsv1.DaemonSet) bool {
	klog.V(5).InfoS("daemonset", "namespace", ds.Namespace, "name", ds.Name, "desired", ds.Status.DesiredNumberScheduled, "current", ds.Status.CurrentNumberScheduled, "ready", ds.Status.NumberReady)
	if ds.Status.DesiredNumberScheduled == 0 {
		return true
	}
	return ds.Status.DesiredNumberScheduled > 0 && ds.Status.DesiredNumberScheduled == ds.Status.NumberReady
}

func getTreesByNodeGroup(ctx context.Context, cli client.Client, nodeGroups []nropv1.NodeGroup, platf platform.Platform) ([]nodegroupv1.Tree, error) {
	switch platf {
	case platform.OpenShift:
		mcps := &machineconfigv1.MachineConfigPoolList{}
		if err := cli.List(ctx, mcps); err != nil {
			return nil, err
		}
		return nodegroupv1.FindTreesOpenshift(mcps, nodeGroups)
	case platform.HyperShift:
		return nodegroupv1.FindTreesHypershift(nodeGroups), nil
	default:
		return nil, fmt.Errorf("unsupported platform")
	}
}
