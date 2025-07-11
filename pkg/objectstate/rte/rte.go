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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	securityv1 "github.com/openshift/api/security/v1"

	k8swgdepselinux "github.com/k8stopologyawareschedwg/deployer/pkg/assets/selinux"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	k8swgrteupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate/rte"
	"github.com/k8stopologyawareschedwg/deployer/pkg/options"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	rtemetrics "github.com/openshift-kni/numaresources-operator/pkg/metrics/manifests/monitor"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

const (
	// MachineConfigLabelKey contains the key of generated label for machine config
	MachineConfigLabelKey   = "machineconfiguration.openshift.io/role"
	HyperShiftNodePoolLabel = "hypershift.openshift.io/nodePool"
)

// TODO: ugly name. At least it's only internal
type rteHelper interface {
	Name() string
	UpdateFromClient(ctx context.Context, cli client.Client, tree nodegroupv1.Tree)
	FindState(mf Manifests, tree nodegroupv1.Tree) []objectstate.ObjectState
}

type daemonSetManifest struct {
	daemonSet      *appsv1.DaemonSet
	daemonSetError error
	rteConfigHash  string
}

type machineConfigManifest struct {
	machineConfig      *machineconfigv1.MachineConfig
	machineConfigError error
}

type Manifests struct {
	Core    rtemanifests.Manifests
	Metrics rtemetrics.Manifests
}

func (mf Manifests) securityContextOptions(legacyMode bool) k8swgrteupdate.SecurityContextOptions {
	if legacyMode {
		return k8swgrteupdate.SecurityContextOptions{
			SELinuxContextType:  k8swgdepselinux.RTEContextTypeLegacy,
			SecurityContextName: mf.Core.SecurityContextConstraint.Name,
		}
	}
	return k8swgrteupdate.SecurityContextOptions{
		SELinuxContextType:  k8swgdepselinux.RTEContextType,
		SecurityContextName: mf.Core.SecurityContextConstraintV2.Name,
	}
}

type Errors struct {
	Core struct {
		SCC                error
		SCCv2              error
		ServiceAccount     error
		Role               error
		RoleBinding        error
		ClusterRole        error
		ClusterRoleBinding error
	}
	Metrics struct {
		Service error
	}
}

type ExistingManifests struct {
	existing       Manifests
	errs           Errors
	daemonSets     map[string]daemonSetManifest
	machineConfigs map[string]machineConfigManifest
	// internal helpers
	plat      platform.Platform
	instance  *nropv1.NUMAResourcesOperator
	trees     []nodegroupv1.Tree
	namespace string
	updater   GenerateDesiredManifestUpdater
	helper    rteHelper
}

func getDaemonSetManifest(ctx context.Context, cli client.Client, namespace, name string) daemonSetManifest {
	key := client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}

	ds := appsv1.DaemonSet{}
	dsm := daemonSetManifest{}
	if dsm.daemonSetError = cli.Get(ctx, key, &ds); dsm.daemonSetError == nil {
		dsm.daemonSet = &ds
	}

	cm := corev1.ConfigMap{}
	if err := cli.Get(ctx, key, &cm); err == nil {
		// we're storing the updated hash only in the case that kubelet controller created a configmap
		hval := hash.ConfigMapData(&cm)
		klog.V(4).InfoS("rte configmap hash computed", "cm", key.String(), "hashValue", hval)
		dsm.rteConfigHash = hval
	}

	return dsm
}

func DaemonSetNamespacedNameFromObject(obj client.Object) (nropv1.NamespacedName, bool) {
	res := nropv1.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
	_, ok := obj.(*appsv1.DaemonSet)
	return res, ok
}

type GeneratedDesiredManifest struct {
	// context
	ClusterPlatform   platform.Platform
	MachineConfigPool *machineconfigv1.MachineConfigPool
	NodeGroup         *nropv1.NodeGroup
	SecOpts           k8swgrteupdate.SecurityContextOptions
	// generated manifests
	DaemonSet             *appsv1.DaemonSet
	RTEConfigHash         string
	ConfigMap             *corev1.ConfigMap
	IsCustomPolicyEnabled bool
}

type GenerateDesiredManifestUpdater func(mcpName string, gdm *GeneratedDesiredManifest) error

func SkipManifestUpdate(mcpName string, gdm *GeneratedDesiredManifest) error {
	return nil
}

func MutatedFromExisting(existingMF *ExistingManifests, defaultManifests Manifests, namespace string) (Manifests, error) {
	mf := Manifests{
		Core:    existingMF.existing.Core.Clone(),
		Metrics: existingMF.existing.Metrics.Clone(),
	}

	o := objectstate.ObjectState{Error: existingMF.errs.Core.ClusterRole}
	if o.IsNotFoundError() {
		mf.Core.ClusterRole = defaultManifests.Core.ClusterRole.DeepCopy()
	}

	o = objectstate.ObjectState{Error: existingMF.errs.Core.ClusterRoleBinding}
	if o.IsNotFoundError() {
		mf.Core.ClusterRoleBinding = defaultManifests.Core.ClusterRoleBinding.DeepCopy()
	}

	o = objectstate.ObjectState{Error: existingMF.errs.Core.Role}
	if o.IsNotFoundError() {
		mf.Core.Role = defaultManifests.Core.Role.DeepCopy()
	}

	o = objectstate.ObjectState{Error: existingMF.errs.Core.RoleBinding}
	if o.IsNotFoundError() {
		mf.Core.RoleBinding = defaultManifests.Core.RoleBinding.DeepCopy()
	}

	o = objectstate.ObjectState{Error: existingMF.errs.Core.ServiceAccount}
	if o.IsNotFoundError() {
		mf.Core.ServiceAccount = defaultManifests.Core.ServiceAccount.DeepCopy()
	}

	if defaultManifests.Core.SecurityContextConstraint != nil {
		o = objectstate.ObjectState{Error: existingMF.errs.Core.SCC}
		if o.IsNotFoundError() {
			mf.Core.SecurityContextConstraint = defaultManifests.Core.SecurityContextConstraint.DeepCopy()
		}
	}

	if defaultManifests.Core.SecurityContextConstraintV2 != nil {
		o = objectstate.ObjectState{Error: existingMF.errs.Core.SCCv2}
		if o.IsNotFoundError() {
			mf.Core.SecurityContextConstraintV2 = defaultManifests.Core.SecurityContextConstraintV2.DeepCopy()
		}
	}

	// there are multiple resources of DaemonSets
	// (one per nodeGroup), so we should use a default manifest + existing/mutated spec,
	// and the rest will be updated later
	mf.Core.DaemonSet = defaultManifests.Core.DaemonSet.DeepCopy()
	for _, ds := range existingMF.daemonSets {
		if ds.daemonSetError == nil {
			// use the spec from one of the existing so we won't end up with
			// diffs that derives from default values applied by the API server
			mf.Core.DaemonSet.Spec = *ds.daemonSet.Spec.DeepCopy()
		}
	}
	// Clear volumes and volume mounts before rendering to avoid duplicates
	// The Render() function will add them back based on the configuration
	clearVolumesAndVolumeMounts(mf.Core.DaemonSet)

	o = objectstate.ObjectState{Error: existingMF.errs.Metrics.Service}
	if o.IsNotFoundError() {
		mf.Metrics.Service = defaultManifests.Metrics.Service.DeepCopy()
	}

	var err error
	mf.Core, err = mf.Core.Render(options.UpdaterDaemon{
		Namespace: namespace,
		DaemonSet: options.DaemonSet{
			Verbose:            2,
			NotificationEnable: true,
			UpdateInterval:     10 * time.Second,
		},
	})
	if err != nil {
		return mf, err
	}
	return mf, err
}

func (em *ExistingManifests) State(mf Manifests) []objectstate.ObjectState {
	ret := []objectstate.ObjectState{
		{
			Existing: em.existing.Core.ServiceAccount,
			Error:    em.errs.Core.ServiceAccount,
			Desired:  mf.Core.ServiceAccount.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ServiceAccountForUpdate,
		},
		{
			Existing: em.existing.Core.Role,
			Error:    em.errs.Core.Role,
			Desired:  mf.Core.Role.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		{
			Existing: em.existing.Core.RoleBinding,
			Error:    em.errs.Core.RoleBinding,
			Desired:  mf.Core.RoleBinding.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		{
			Existing: em.existing.Core.ClusterRole,
			Error:    em.errs.Core.ClusterRole,
			Desired:  mf.Core.ClusterRole.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		{
			Existing: em.existing.Core.ClusterRoleBinding,
			Error:    em.errs.Core.ClusterRoleBinding,
			Desired:  mf.Core.ClusterRoleBinding.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
	}

	if mf.Core.SecurityContextConstraint != nil {
		ret = append(ret, objectstate.ObjectState{
			Existing: em.existing.Core.SecurityContextConstraint,
			Error:    em.errs.Core.SCC,
			Desired:  mf.Core.SecurityContextConstraint.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		})
	}
	if mf.Core.SecurityContextConstraintV2 != nil {
		ret = append(ret, objectstate.ObjectState{
			Existing: em.existing.Core.SecurityContextConstraintV2,
			Error:    em.errs.Core.SCCv2,
			Desired:  mf.Core.SecurityContextConstraintV2.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		})
	}

	klog.V(4).InfoS("RTE manifests processing trees", "method", em.helper.Name())

	for _, tree := range em.trees {
		ret = append(ret, em.helper.FindState(mf, tree)...)
	}

	// extra: metrics

	ret = append(ret, objectstate.ObjectState{
		Existing: em.existing.Metrics.Service,
		Error:    em.errs.Metrics.Service,
		Desired:  mf.Metrics.Service.DeepCopy(),
		Compare:  compare.Object,
		Merge:    merge.ServiceForUpdate,
	})

	return ret
}

func (em *ExistingManifests) WithManifestsUpdater(updater GenerateDesiredManifestUpdater) *ExistingManifests {
	em.updater = updater
	return em
}

func FromClient(ctx context.Context, cli client.Client, plat platform.Platform, mf Manifests, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree, namespace string) *ExistingManifests {
	ret := ExistingManifests{
		existing: Manifests{
			Core: rtemanifests.New(plat),
		},
		daemonSets: make(map[string]daemonSetManifest),
		plat:       plat,
		instance:   instance,
		trees:      trees,
		namespace:  namespace,
		updater:    SkipManifestUpdate,
	}

	keyFor := client.ObjectKeyFromObject // shortcut

	if plat == platform.OpenShift {
		ret.helper = machineConfigPoolFinder{
			em:        &ret,
			instance:  instance,
			namespace: namespace,
		}
	} else {
		ret.helper = nodeGroupFinder{
			em:        &ret,
			instance:  instance,
			namespace: namespace,
		}
	}

	// objects that should present in the single replica
	ro := &rbacv1.Role{}
	if ok := getObject(ctx, cli, keyFor(mf.Core.Role), ro, &ret.errs.Core.Role); ok {
		ret.existing.Core.Role = ro
	}

	rb := &rbacv1.RoleBinding{}
	if ok := getObject(ctx, cli, keyFor(mf.Core.RoleBinding), rb, &ret.errs.Core.RoleBinding); ok {
		ret.existing.Core.RoleBinding = rb
	}

	cro := &rbacv1.ClusterRole{}
	if ok := getObject(ctx, cli, keyFor(mf.Core.ClusterRole), cro, &ret.errs.Core.ClusterRole); ok {
		ret.existing.Core.ClusterRole = cro
	}

	crb := &rbacv1.ClusterRoleBinding{}
	if ok := getObject(ctx, cli, keyFor(mf.Core.ClusterRoleBinding), crb, &ret.errs.Core.ClusterRoleBinding); ok {
		ret.existing.Core.ClusterRoleBinding = crb
	}

	sa := &corev1.ServiceAccount{}
	if ok := getObject(ctx, cli, keyFor(mf.Core.ServiceAccount), sa, &ret.errs.Core.ServiceAccount); ok {
		ret.existing.Core.ServiceAccount = sa
	}

	klog.V(4).InfoS("RTE manifests processing trees", "method", ret.helper.Name())

	if plat != platform.Kubernetes {
		scc := &securityv1.SecurityContextConstraints{}
		if ok := getObject(ctx, cli, keyFor(mf.Core.SecurityContextConstraint), scc, &ret.errs.Core.SCC); ok {
			ret.existing.Core.SecurityContextConstraint = scc
		}
		sccv2 := &securityv1.SecurityContextConstraints{}
		if ok := getObject(ctx, cli, keyFor(mf.Core.SecurityContextConstraintV2), sccv2, &ret.errs.Core.SCCv2); ok {
			ret.existing.Core.SecurityContextConstraintV2 = sccv2
		}

		ret.machineConfigs = make(map[string]machineConfigManifest)
	}

	// should have the amount of resources equals to the amount of node groups
	for _, tree := range trees {
		ret.helper.UpdateFromClient(ctx, cli, tree)
	}

	// extra: metrics
	ser := &corev1.Service{}
	if ok := getObject(ctx, cli, keyFor(mf.Metrics.Service), ser, &ret.errs.Metrics.Service); ok {
		ret.existing.Metrics.Service = ser
	}

	return &ret
}

// getObject is a shortcut to don't type the error twice
func getObject(ctx context.Context, cli client.Client, key client.ObjectKey, obj client.Object, err *error) bool {
	*err = cli.Get(ctx, key, obj)
	return *err == nil
}

func clearVolumesAndVolumeMounts(ds *appsv1.DaemonSet) {
	podSpec := &ds.Spec.Template.Spec
	podSpec.Volumes = nil
	for i := range podSpec.Containers {
		podSpec.Containers[i].VolumeMounts = nil
	}
}
