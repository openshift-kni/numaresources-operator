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
	"reflect"
	"time"

	"github.com/k8stopologyawareschedwg/deployer/pkg/assets/selinux"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	k8swgrteupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate/rte"
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

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	"github.com/openshift-kni/numaresources-operator/internal/relatedobjects"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	"github.com/openshift-kni/numaresources-operator/pkg/images"
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
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
	Images          images.Data
	ImagePullPolicy corev1.PullPolicy
	Recorder        record.EventRecorder
	ForwardMCPConds bool
}

type mcpWaitForUpdatedFunc func(string, *machineconfigv1.MachineConfigPool) bool

// TODO: narrow down

// Namespace Scoped
// TODO

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
		message := fmt.Sprintf("incorrect NUMAResourcesOperator resource name: %s", instance.Name)
		return r.updateStatus(ctx, instance, status.ConditionDegraded, status.ConditionTypeIncorrectNUMAResourcesOperatorResourceName, message)
	}

	if err := validation.NodeGroups(instance.Spec.NodeGroups); err != nil {
		return r.updateStatus(ctx, instance, status.ConditionDegraded, validation.NodeGroupsError, err.Error())
	}

	trees, err := getTreesByNodeGroup(ctx, r.Client, instance.Spec.NodeGroups)
	if err != nil {
		return r.updateStatus(ctx, instance, status.ConditionDegraded, validation.NodeGroupsError, err.Error())
	}

	if err := validation.MachineConfigPoolDuplicates(trees); err != nil {
		return r.updateStatus(ctx, instance, status.ConditionDegraded, validation.NodeGroupsError, err.Error())
	}

	for idx := range trees {
		conf := trees[idx].NodeGroup.NormalizeConfig()
		trees[idx].NodeGroup.Config = &conf
	}

	result, condition, err := r.reconcileResource(ctx, instance, trees)
	if condition != "" {
		// TODO: use proper reason
		reason, message := condition, messageFromError(err)
		_, _ = r.updateStatus(ctx, instance, condition, reason, message)
	}
	return result, err
}

func (r *NUMAResourcesOperatorReconciler) updateStatus(ctx context.Context, instance *nropv1.NUMAResourcesOperator, condition string, reason string, message string) (ctrl.Result, error) {
	klog.InfoS("updateStatus", "condition", condition, "reason", reason, "message", message)

	if _, err := updateStatus(ctx, r.Client, instance, condition, reason, message); err != nil {
		klog.InfoS("Failed to update numaresourcesoperator status", "Desired condition", status.ConditionDegraded, "error", err)
		return ctrl.Result{}, err
	}
	// we do not return an error here because to pass the validation error a user will need to update NRO CR
	// that will anyway initiate to reconcile loop
	return ctrl.Result{}, nil
}

func updateStatus(ctx context.Context, cli client.Client, instance *nropv1.NUMAResourcesOperator, condition string, reason string, message string) (bool, error) {
	conditions, ok := status.GetUpdatedConditions(instance.Status.Conditions, condition, reason, message)
	if !ok {
		return false, nil
	}
	instance.Status.Conditions = conditions

	if err := cli.Status().Update(ctx, instance); err != nil {
		return false, errors.Wrapf(err, "could not update status for object %s", client.ObjectKeyFromObject(instance))
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

func (r *NUMAResourcesOperatorReconciler) reconcileResourceAPI(ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) (bool, ctrl.Result, string, error) {
	applied, err := r.syncNodeResourceTopologyAPI(ctx)
	if err != nil {
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "FailedCRDInstall", "Failed to install Node Resource Topology CRD: %v", err)
		return true, ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "FailedAPISync")
	}
	if applied {
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "SuccessfulCRDInstall", "Node Resource Topology CRD installed")
	}
	return false, ctrl.Result{}, "", nil
}

func (r *NUMAResourcesOperatorReconciler) reconcileResourceMachineConfig(ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) (bool, ctrl.Result, string, error) {
	// we need to sync machine configs first and wait for the MachineConfigPool updates
	// before checking additional components for updates
	mcpUpdatedFunc, err := r.syncMachineConfigs(ctx, instance, trees)
	if err != nil {
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "FailedMCSync", "Failed to set up machine configuration for worker nodes: %v", err)
		return true, ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "failed to sync machine configs")
	}
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "SuccessfulMCSync", "Enabled machine configuration for worker nodes")

	// MCO needs to update the SELinux context removal and other stuff, and need to trigger a reboot.
	// It can take a while.
	mcpStatuses, allMCPsUpdated := syncMachineConfigPoolsStatuses(instance.Name, trees, r.ForwardMCPConds, mcpUpdatedFunc)
	instance.Status.MachineConfigPools = mcpStatuses
	if !allMCPsUpdated {
		// the Machine Config Pool still did not apply the machine config, wait for one minute
		return true, ctrl.Result{RequeueAfter: numaResourcesRetryPeriod}, status.ConditionProgressing, nil
	}

	instance.Status.MachineConfigPools = syncMachineConfigPoolNodeGroupConfigStatuses(instance.Status.MachineConfigPools, trees)
	return false, ctrl.Result{}, "", nil
}

func (r *NUMAResourcesOperatorReconciler) reconcileResourceDaemonSet(ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) (bool, ctrl.Result, string, error) {
	daemonSetsInfo, err := r.syncNUMAResourcesOperatorResources(ctx, instance, trees)
	if err != nil {
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "FailedRTECreate", "Failed to create Resource-Topology-Exporter DaemonSets: %v", err)
		return true, ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "FailedRTESync")
	}
	if len(daemonSetsInfo) == 0 {
		return false, ctrl.Result{}, "", nil
	}

	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "SuccessfulRTECreate", "Created Resource-Topology-Exporter DaemonSets")

	dsStatuses, allDSsUpdated, err := r.syncDaemonSetsStatuses(ctx, r.Client, daemonSetsInfo)
	instance.Status.DaemonSets = dsStatuses
	instance.Status.RelatedObjects = relatedobjects.ResourceTopologyExporter(r.Namespace, dsStatuses)
	if err != nil {
		return true, ctrl.Result{}, status.ConditionDegraded, err
	}
	if !allDSsUpdated {
		return true, ctrl.Result{RequeueAfter: 5 * time.Second}, status.ConditionProgressing, nil
	}

	return false, ctrl.Result{}, "", nil
}

func (r *NUMAResourcesOperatorReconciler) reconcileResource(ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) (ctrl.Result, string, error) {
	if done, res, cond, err := r.reconcileResourceAPI(ctx, instance, trees); done {
		return res, cond, err
	}

	if r.Platform == platform.OpenShift {
		if done, res, cond, err := r.reconcileResourceMachineConfig(ctx, instance, trees); done {
			return res, cond, err
		}
	}

	if done, res, cond, err := r.reconcileResourceDaemonSet(ctx, instance, trees); done {
		return res, cond, err
	}

	return ctrl.Result{}, status.ConditionAvailable, nil
}

func (r *NUMAResourcesOperatorReconciler) syncDaemonSetsStatuses(ctx context.Context, rd client.Reader, daemonSetsInfo []nropv1.NamespacedName) ([]nropv1.NamespacedName, bool, error) {
	dsStatuses := []nropv1.NamespacedName{}
	for _, nname := range daemonSetsInfo {
		ds := appsv1.DaemonSet{}
		dsKey := client.ObjectKey{
			Namespace: nname.Namespace,
			Name:      nname.Name,
		}
		err := rd.Get(ctx, dsKey, &ds)
		if err != nil {
			return dsStatuses, false, err
		}

		if !isDaemonSetReady(&ds) {
			return dsStatuses, false, nil
		}
		dsStatuses = append(dsStatuses, nname)
	}
	return dsStatuses, true, nil
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
			err = errors.Wrapf(err2, "could not create %s", objState.Desired.GetObjectKind().GroupVersionKind().String())
			break
		}
		if !updated {
			continue
		}
		updatedCount++
	}
	return (updatedCount == len(objStates)), err
}

func (r *NUMAResourcesOperatorReconciler) syncMachineConfigs(ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) (mcpWaitForUpdatedFunc, error) {
	klog.V(4).InfoS("Machine Config Sync start", "trees", len(trees))
	defer klog.V(4).Info("Machine Config Sync stop")

	existing := rtestate.FromClient(ctx, r.Client, r.Platform, r.RTEManifests, instance, trees, r.Namespace)

	var err error
	var mcpUpdatedFunc mcpWaitForUpdatedFunc
	objStates := existing.MachineConfigsState(r.RTEManifests)
	// Since 4.18 we're using a built-in SELinux policy,
	// so the MachineConfig which applies the custom policy is no longer necessary.
	// In case of operator upgrade from 4.1X → 4.18, it's necessary to remove the old MachineConfig,
	// unless an emergency annotation is provided which forces the operator to use custom policy
	if !annotations.IsCustomPolicyEnabled(instance.Annotations) {
		for _, objState := range objStates {
			if !objState.IsNotFoundError() {
				klog.V(4).InfoS("delete Machine Config", "MachineConfig", objState.Desired.GetName())
				if err2 := r.Client.Delete(ctx, objState.Desired); err2 != nil {
					err = errors.Wrapf(err2, "could not delete MachineConfig %s", objState.Desired.GetName())
				}
				klog.V(4).InfoS("Machine Config deleted successfully", "MachineConfig", objState.Desired.GetName())
			} // if not found, it's a fresh installation of 4.18+ (no upgrade)
		}
		mcpUpdatedFunc = IsMachineConfigPoolUpdatedAfterDeletion
	} else {
		for _, objState := range objStates {
			if err2 := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme); err2 != nil {
				err = errors.Wrapf(err2, "failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
				break
			}

			if err2 := validateMachineConfigLabels(objState.Desired, trees); err2 != nil {
				err = errors.Wrapf(err2, "machine conig %q labels validation failed", objState.Desired.GetName())
				break
			}

			_, _, err2 := apply.ApplyObject(ctx, r.Client, objState)
			if err2 != nil {
				err = errors.Wrapf(err2, "could not apply (%s) %s/%s", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName())
				break
			}
		}
		mcpUpdatedFunc = IsMachineConfigPoolUpdated
	}
	return mcpUpdatedFunc, err
}

func syncMachineConfigPoolsStatuses(instanceName string, trees []nodegroupv1.Tree, forwardMCPConds bool, updatedFunc mcpWaitForUpdatedFunc) ([]nropv1.MachineConfigPool, bool) {
	klog.V(4).InfoS("Machine Config Status Sync start", "trees", len(trees))
	defer klog.V(4).Info("Machine Config Status Sync stop")

	mcpStatuses := []nropv1.MachineConfigPool{}
	for _, tree := range trees {
		for _, mcp := range tree.MachineConfigPools {
			mcpStatuses = append(mcpStatuses, extractMCPStatus(mcp, forwardMCPConds))

			isUpdated := updatedFunc(instanceName, mcp)
			klog.V(5).InfoS("Machine Config Pool state", "name", mcp.Name, "instance", instanceName, "updated", isUpdated)

			if !isUpdated {
				return mcpStatuses, false
			}
		}
	}
	return mcpStatuses, true
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

func (r *NUMAResourcesOperatorReconciler) syncNUMAResourcesOperatorResources(ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) ([]nropv1.NamespacedName, error) {
	klog.V(4).InfoS("RTESync start", "trees", len(trees))
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
	var daemonSetsNName []nropv1.NamespacedName

	err = rteupdate.DaemonSetUserImageSettings(r.RTEManifests.DaemonSet, instance.Spec.ExporterImage, r.Images.Preferred(), r.ImagePullPolicy)
	if err != nil {
		return daemonSetsNName, err
	}

	err = rteupdate.DaemonSetPauseContainerSettings(r.RTEManifests.DaemonSet)
	if err != nil {
		return daemonSetsNName, err
	}

	err = loglevel.UpdatePodSpec(&r.RTEManifests.DaemonSet.Spec.Template.Spec, manifests.ContainerNameRTE, instance.Spec.LogLevel)
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
	rteupdate.SecurityContextConstraint(r.RTEManifests.SecurityContextConstraint, annotations.IsCustomPolicyEnabled(instance.Annotations))

	existing := rtestate.FromClient(ctx, r.Client, r.Platform, r.RTEManifests, instance, trees, r.Namespace)
	for _, objState := range existing.State(r.RTEManifests, daemonsetUpdater, annotations.IsCustomPolicyEnabled(instance.Annotations)) {
		if objState.Error != nil {
			// We are likely in the bootstrap scenario. In this case, which is expected once, everything is fine.
			// If it happens past bootstrap, still carry on. We know what to do, and we do want to enforce the desired state.
			klog.Warningf("error loading object: %v", objState.Error)
		}
		if objState.UpdateError != nil {
			// this is an internal error. Should not happen. But if it happen, we don't want to send garbage to the cluster, so we abort
			return nil, errors.Wrapf(err, "failed to update (%s) %s/%s", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
		err := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
		obj, _, err := apply.ApplyObject(ctx, r.Client, objState)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to apply (%s) %s/%s", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName())
		}

		if nname, ok := rtestate.DaemonSetNamespacedNameFromObject(obj); ok {
			daemonSetsNName = append(daemonSetsNName, nname)
		}
	}
	return daemonSetsNName, nil
}

func (r *NUMAResourcesOperatorReconciler) deleteUnusedDaemonSets(ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) []error {
	klog.V(3).Info("Delete Daemonsets start")
	defer klog.V(3).Info("Delete Daemonsets end")
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

func (r *NUMAResourcesOperatorReconciler) deleteUnusedMachineConfigs(ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) []error {
	klog.V(3).Info("Delete Machineconfigs start")
	defer klog.V(3).Info("Delete Machineconfigs end")
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

	b := ctrl.NewControllerManagedBy(mgr).For(&nropv1.NUMAResourcesOperator{})
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
			&machineconfigv1.MachineConfigPool{},
			handler.EnqueueRequestsFromMapFunc(r.mcpToNUMAResourceOperator),
			builder.WithPredicates(mcpPredicates)).
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

func daemonsetUpdater(mcpName string, gdm *rtestate.GeneratedDesiredManifest) error {
	rteupdate.DaemonSetTolerations(gdm.DaemonSet, gdm.NodeGroup.Config.Tolerations)

	err := rteupdate.DaemonSetArgs(gdm.DaemonSet, *gdm.NodeGroup.Config)
	if err != nil {
		klog.V(5).InfoS("DaemonSet update: cannot update arguments", "mcp", mcpName, "daemonset", gdm.DaemonSet.Name, "error", err)
		return err
	}

	// on kubernetes we can just mount the kubeletconfig (no SCC/Selinux),
	// so handling the kubeletconfig configmap is not needed at all.
	// We cannot do this at GetManifests time because we need to mount
	// a specific configmap for each daemonset, whose name we know only
	// when we instantiate the daemonset from the MCP.
	if gdm.ClusterPlatform != platform.OpenShift {
		klog.V(5).InfoS("DaemonSet update: unsupported platform", "mcp", mcpName, "platform", gdm.ClusterPlatform)
		// nothing to do!
		return nil
	}
	err = rteupdate.ContainerConfig(gdm.DaemonSet, gdm.DaemonSet.Name)
	if err != nil {
		// intentionally info because we want to keep going
		klog.V(5).InfoS("DaemonSet update: cannot update config", "mcp", mcpName, "daemonset", gdm.DaemonSet.Name, "error", err)
		return err
	}
	if gdm.ClusterPlatform != platform.Kubernetes {
		if gdm.IsCustomPolicyEnabled && gdm.ClusterPlatform == platform.OpenShift {
			k8swgrteupdate.SecurityContext(gdm.DaemonSet, selinux.RTEContextTypeLegacy)
			klog.V(5).InfoS("DaemonSet update: selinux options", "container", manifests.ContainerNameRTE, "context", selinux.RTEContextTypeLegacy)
		} else {
			k8swgrteupdate.SecurityContext(gdm.DaemonSet, selinux.RTEContextType)
			klog.V(5).InfoS("DaemonSet update: selinux options", "container", manifests.ContainerNameRTE, "context", selinux.RTEContextType)
		}
	}
	return nil
}

func isDaemonSetReady(ds *appsv1.DaemonSet) bool {
	ok := (ds.Status.DesiredNumberScheduled > 0 && ds.Status.DesiredNumberScheduled == ds.Status.NumberReady)
	klog.V(5).InfoS("daemonset", "namespace", ds.Namespace, "name", ds.Name, "desired", ds.Status.DesiredNumberScheduled, "current", ds.Status.CurrentNumberScheduled, "ready", ds.Status.NumberReady)
	return ok
}

func getTreesByNodeGroup(ctx context.Context, cli client.Client, nodeGroups []nropv1.NodeGroup) ([]nodegroupv1.Tree, error) {
	mcps := &machineconfigv1.MachineConfigPoolList{}
	if err := cli.List(ctx, mcps); err != nil {
		return nil, err
	}
	return nodegroupv1.FindTrees(mcps, nodeGroups)
}
