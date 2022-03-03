/*
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
 *
 * Copyright 2021 Red Hat, Inc.
 */

package rte

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

// MachineConfigLabelKey contains the key of generated label for machine config
const MachineConfigLabelKey = "machineconfiguration.openshift.io/role"

type daemonSetManifest struct {
	daemonSet      *appsv1.DaemonSet
	daemonSetError error
}

type machineConfigManifest struct {
	machineConfig      *machineconfigv1.MachineConfig
	machineConfigError error
}

type ExistingManifests struct {
	existing                rtemanifests.Manifests
	daemonSets              map[string]daemonSetManifest
	machineConfigs          map[string]machineConfigManifest
	sccError                error
	serviceAccountError     error
	roleError               error
	roleBindingError        error
	clusterRoleError        error
	clusterRoleBindingError error
}

func (em *ExistingManifests) MachineConfigsState(mf rtemanifests.Manifests, instance *nropv1alpha1.NUMAResourcesOperator, mcps []*machineconfigv1.MachineConfigPool) []objectstate.ObjectState {
	var ret []objectstate.ObjectState
	for _, mcp := range mcps {
		if mf.MachineConfig != nil {
			mcName := objectnames.GetMachineConfigName(instance.Name, mcp.Name)
			if mcp.Spec.MachineConfigSelector == nil {
				klog.Warningf("the machine config pool %q does not have machine config selector", mcp.Name)
				continue
			}
			desiredMachineConfig := mf.MachineConfig.DeepCopy()
			// prefix machine config name to guarantee that we will have an option to override it
			desiredMachineConfig.Name = mcName
			desiredMachineConfig.Labels = GetMachineConfigLabel(mcp)

			existingMachineConfig, ok := em.machineConfigs[mcName]
			if !ok {
				klog.Warningf("failed to find machine config %q under the namespace %q", mcName, desiredMachineConfig.Namespace)
				continue
			}

			ret = append(ret,
				objectstate.ObjectState{
					Existing: existingMachineConfig.machineConfig,
					Error:    existingMachineConfig.machineConfigError,
					Desired:  desiredMachineConfig,
					Compare:  compare.Object,
					Merge:    merge.ObjectForUpdate,
				},
			)
		}
	}

	return ret
}

// GetMachineConfigLabel returns machine config labels that should be used under the machine config pool
// machine config selector
func GetMachineConfigLabel(mcp *machineconfigv1.MachineConfigPool) map[string]string {
	if len(mcp.Spec.MachineConfigSelector.MatchLabels) > 0 {
		return mcp.Spec.MachineConfigSelector.MatchLabels
	}

	// true only for custom machine config pools
	klog.Warningf("no match labels was found under the machine config pool %q machine config selector", mcp.Name)
	labels := map[string]string{
		"machineconfiguration.openshift.io/role": mcp.Name,
	}
	klog.Warningf("generated labels %v, make sure the label is selected by the machine config pool %q", labels, mcp.Name)
	return labels
}

func (em *ExistingManifests) State(mf rtemanifests.Manifests, plat platform.Platform, instance *nropv1alpha1.NUMAResourcesOperator, mcps []*machineconfigv1.MachineConfigPool) []objectstate.ObjectState {
	ret := []objectstate.ObjectState{
		// service account
		{
			Existing: em.existing.ServiceAccount,
			Error:    em.serviceAccountError,
			Desired:  mf.ServiceAccount.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ServiceAccountForUpdate,
		},
		// role
		{
			Existing: em.existing.Role,
			Error:    em.roleError,
			Desired:  mf.Role.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		// role binding
		{
			Existing: em.existing.RoleBinding,
			Error:    em.roleBindingError,
			Desired:  mf.RoleBinding.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		// cluster role
		{
			Existing: em.existing.ClusterRole,
			Error:    em.clusterRoleError,
			Desired:  mf.ClusterRole.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		// cluster role binding
		{
			Existing: em.existing.ClusterRoleBinding,
			Error:    em.clusterRoleBindingError,
			Desired:  mf.ClusterRoleBinding.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
	}

	if mf.SecurityContextConstraint != nil {
		ret = append(ret, objectstate.ObjectState{
			Existing: em.existing.SecurityContextConstraint,
			Error:    em.sccError,
			Desired:  mf.SecurityContextConstraint.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		})
	}

	for _, mcp := range mcps {
		if mcp.Spec.NodeSelector == nil {
			klog.Warningf("the machine config pool %q does not have node selector", mcp.Name)
			continue
		}

		generatedName := objectnames.GetComponentName(instance.Name, mcp.Name)
		desiredDaemonSet := mf.DaemonSet.DeepCopy()
		desiredDaemonSet.Name = generatedName
		desiredDaemonSet.Spec.Template.Spec.NodeSelector = mcp.Spec.NodeSelector.MatchLabels

		// on kubernetes we can just mount the kubeletconfig (no SCC/Selinux),
		// so handling the kubeletconfig configmap is not needed at all.
		// We cannot do this at GetManifests time because we need to mount
		// a specific configmap for each daemonset, whose nome we know only
		// when we instantiate the daemonset from the MCP.
		if plat == platform.OpenShift {
			// TODO: actually check for the right container, don't just use "0"
			manifests.UpdateResourceTopologyExporterContainerConfig(
				&desiredDaemonSet.Spec.Template.Spec,
				&desiredDaemonSet.Spec.Template.Spec.Containers[0],
				generatedName)
		}

		existingDaemonSet, ok := em.daemonSets[generatedName]
		if !ok {
			klog.Warningf("failed to find daemon set %q under the namespace %q", generatedName, desiredDaemonSet.Namespace)
			continue
		}

		ret = append(ret,
			objectstate.ObjectState{
				Existing: existingDaemonSet.daemonSet,
				Error:    existingDaemonSet.daemonSetError,
				Desired:  desiredDaemonSet,
				Compare:  compare.Object,
				Merge:    merge.ObjectForUpdate,
			},
		)
	}

	return ret
}

func FromClient(
	ctx context.Context,
	cli client.Client,
	plat platform.Platform,
	mf rtemanifests.Manifests,
	instance *nropv1alpha1.NUMAResourcesOperator,
	mcps []*machineconfigv1.MachineConfigPool,
	namespace string,
) ExistingManifests {
	ret := ExistingManifests{
		existing: rtemanifests.New(plat),
	}

	// objects that should present in the single replica
	ro := &rbacv1.Role{}
	if ret.roleError = cli.Get(ctx, client.ObjectKeyFromObject(mf.Role), ro); ret.roleError == nil {
		ret.existing.Role = ro
	}

	rb := &rbacv1.RoleBinding{}
	if ret.roleBindingError = cli.Get(ctx, client.ObjectKeyFromObject(mf.RoleBinding), rb); ret.roleBindingError == nil {
		ret.existing.RoleBinding = rb
	}

	cro := &rbacv1.ClusterRole{}
	if ret.clusterRoleError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ClusterRole), cro); ret.clusterRoleError == nil {
		ret.existing.ClusterRole = cro
	}

	crb := &rbacv1.ClusterRoleBinding{}
	if ret.clusterRoleBindingError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ClusterRoleBinding), crb); ret.clusterRoleBindingError == nil {
		ret.existing.ClusterRoleBinding = crb
	}

	sa := &corev1.ServiceAccount{}
	if ret.serviceAccountError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ServiceAccount), sa); ret.serviceAccountError == nil {
		ret.existing.ServiceAccount = sa
	}

	if plat == platform.OpenShift {
		scc := &securityv1.SecurityContextConstraints{}
		if ret.sccError = cli.Get(ctx, client.ObjectKeyFromObject(mf.SecurityContextConstraint), scc); ret.sccError == nil {
			ret.existing.SecurityContextConstraint = scc
		}
	}

	// should have the amount of resources equals to the amount of node groups
	for _, mcp := range mcps {
		generatedName := objectnames.GetComponentName(instance.Name, mcp.Name)
		key := client.ObjectKey{
			Name:      generatedName,
			Namespace: namespace,
		}

		if ret.daemonSets == nil {
			ret.daemonSets = map[string]daemonSetManifest{}
		}

		ds := &appsv1.DaemonSet{}
		if err := cli.Get(ctx, key, ds); err == nil {
			ret.daemonSets[generatedName] = daemonSetManifest{
				daemonSet: ds,
			}
		} else {
			ret.daemonSets[generatedName] = daemonSetManifest{
				daemonSetError: err,
			}
		}

		if plat == platform.OpenShift {
			if ret.machineConfigs == nil {
				ret.machineConfigs = map[string]machineConfigManifest{}
			}

			mcName := objectnames.GetMachineConfigName(instance.Name, mcp.Name)
			key := client.ObjectKey{
				Name: mcName,
			}
			mc := &machineconfigv1.MachineConfig{}
			if err := cli.Get(ctx, key, mc); err == nil {
				ret.machineConfigs[mcName] = machineConfigManifest{
					machineConfig: mc,
				}
			} else {
				ret.machineConfigs[mcName] = machineConfigManifest{
					machineConfigError: err,
				}
			}
		}
	}

	return ret
}

func DaemonSetNamespacedNameFromObject(obj client.Object) (nropv1alpha1.NamespacedName, bool) {
	res := nropv1alpha1.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
	_, ok := obj.(*appsv1.DaemonSet)
	return res, ok
}

func UpdateDaemonSetPauseContainerSettings(ds *appsv1.DaemonSet) error {
	// TODO: better match by name than assume container#0 is RTE proper (not minion)
	rteCnt := &ds.Spec.Template.Spec.Containers[0]
	// TODO: better match by name than assume container#1 is the RTE minion
	cnt := &ds.Spec.Template.Spec.Containers[1]
	cnt.Image = rteCnt.Image
	cnt.ImagePullPolicy = rteCnt.ImagePullPolicy
	cnt.Command = []string{
		"/bin/sh",
		"-c",
		"--",
	}
	cnt.Args = []string{
		"while true; do sleep 30s; done",
	}
	return nil
}

func UpdateDaemonSetUserImageSettings(ds *appsv1.DaemonSet, userImageSpec, builtinImageSpec string, builtinPullPolicy corev1.PullPolicy) error {
	// TODO: better match by name than assume container#0 is RTE proper (not minion)
	cnt := &ds.Spec.Template.Spec.Containers[0]
	if userImageSpec != "" {
		// we don't really know what's out there, so we minimize the changes.
		cnt.Image = userImageSpec
		klog.V(3).InfoS("Exporter image", "reason", "user-provided", "pullSpec", userImageSpec)
		return nil
	}

	if builtinImageSpec == "" {
		return fmt.Errorf("missing built-in image spec, no user image provided")
	}

	cnt.Image = builtinImageSpec
	cnt.ImagePullPolicy = builtinPullPolicy
	klog.V(3).InfoS("Exporter image", "reason", "builtin", "pullSpec", builtinImageSpec, "pullPolicy", builtinPullPolicy)
	// if we run with operator-as-operand, we know we NEED this.
	UpdateDaemonSetRunAsIDs(ds)

	return nil
}

// UpdateDaemonSetRunAsIDs bump the ds container privileges to 0/0.
// We need this in the operator-as-operand flow because the operator image itself
// is built to run with non-root user/group, and we should keep it like this.
// OTOH, the rte image needs to have access to the files using *both* DAC and MAC;
// the SCC/SELinux context take cares of the MAC (when needed, e.g. on OCP), while
// we take care of DAC here.
func UpdateDaemonSetRunAsIDs(ds *appsv1.DaemonSet) {
	// TODO: better match by name than assume container#0 is RTE proper (not minion)
	cnt := &ds.Spec.Template.Spec.Containers[0]
	if cnt.SecurityContext == nil {
		cnt.SecurityContext = &corev1.SecurityContext{}
	}
	var rootID int64 = 0
	cnt.SecurityContext.RunAsUser = &rootID
	cnt.SecurityContext.RunAsGroup = &rootID
	klog.InfoS("RTE container elevated privileges", "container", cnt.Name, "user", rootID, "group", rootID)
}

func UpdateDaemonSetHashAnnotation(ds *appsv1.DaemonSet, cmHash string) {
	template := &ds.Spec.Template
	if template.Annotations == nil {
		template.Annotations = map[string]string{}
	}
	template.Annotations[hash.ConfigMapAnnotation] = cmHash
}
