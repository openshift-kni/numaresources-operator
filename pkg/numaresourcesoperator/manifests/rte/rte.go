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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	securityv1 "github.com/openshift/api/security/v1"

	selinuxassets "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/assets/selinux"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/manifests"
	ocpupdate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/objectupdate/ocp"
	rbacupdate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/objectupdate/rbac"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/objectupdate/rte"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/options"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/platform"
)

const (
	configDataField = "config.yaml"
)

type Manifests struct {
	ServiceAccount             *corev1.ServiceAccount
	Role                       *rbacv1.Role
	RoleBinding                *rbacv1.RoleBinding
	ClusterRole                *rbacv1.ClusterRole
	ClusterRoleBinding         *rbacv1.ClusterRoleBinding
	ConfigMap                  *corev1.ConfigMap
	DaemonSet                  *appsv1.DaemonSet
	DefaultNetworkPolicy       *networkingv1.NetworkPolicy
	APIServerNetworkPolicy     *networkingv1.NetworkPolicy
	MetricsServerNetworkPolicy *networkingv1.NetworkPolicy

	// OpenShift related components
	MachineConfig               *machineconfigv1.MachineConfig
	SecurityContextConstraint   *securityv1.SecurityContextConstraints
	SecurityContextConstraintV2 *securityv1.SecurityContextConstraints

	// internal fields
	plat platform.Platform
}

func (mf Manifests) Clone() Manifests {
	ret := Manifests{
		plat: mf.plat,
		// objects
		Role:                       mf.Role.DeepCopy(),
		RoleBinding:                mf.RoleBinding.DeepCopy(),
		ClusterRole:                mf.ClusterRole.DeepCopy(),
		ClusterRoleBinding:         mf.ClusterRoleBinding.DeepCopy(),
		DaemonSet:                  mf.DaemonSet.DeepCopy(),
		ServiceAccount:             mf.ServiceAccount.DeepCopy(),
		ConfigMap:                  mf.ConfigMap.DeepCopy(),
		DefaultNetworkPolicy:       mf.DefaultNetworkPolicy.DeepCopy(),
		APIServerNetworkPolicy:     mf.APIServerNetworkPolicy.DeepCopy(),
		MetricsServerNetworkPolicy: mf.MetricsServerNetworkPolicy.DeepCopy(),
	}

	if mf.plat == platform.OpenShift || mf.plat == platform.HyperShift {
		//  MachineConfig is obsolete starting from OCP v4.18
		if mf.MachineConfig != nil {
			ret.MachineConfig = mf.MachineConfig.DeepCopy()
		}
		ret.SecurityContextConstraint = mf.SecurityContextConstraint.DeepCopy()
		ret.SecurityContextConstraintV2 = mf.SecurityContextConstraintV2.DeepCopy()
	}

	return ret
}

func (mf Manifests) Render(opts options.UpdaterDaemon) (Manifests, error) {
	ret := mf.Clone()
	if ret.plat == platform.Kubernetes {
		if opts.Namespace != "" {
			ret.ServiceAccount.Namespace = opts.Namespace
		}
	}

	if opts.Name != "" {
		ret.RoleBinding.Name = opts.Name
		ret.ServiceAccount.Name = opts.Name
		ret.Role.Name = opts.Name
		ret.DaemonSet.Name = opts.Name
		ret.ClusterRole.Name = opts.Name
		ret.ClusterRoleBinding.Name = opts.Name
		ret.DefaultNetworkPolicy.Name = opts.Name
		ret.APIServerNetworkPolicy.Name = opts.Name
		ret.MetricsServerNetworkPolicy.Name = opts.Name
	}

	rbacupdate.RoleBinding(ret.RoleBinding, mf.ServiceAccount.Name, ret.ServiceAccount.Namespace)
	rbacupdate.ClusterRoleBinding(ret.ClusterRoleBinding, mf.ServiceAccount.Name, mf.ServiceAccount.Namespace)

	ret.DaemonSet.Spec.Template.Spec.ServiceAccountName = mf.ServiceAccount.Name

	rteConfigMapName := ""
	if len(opts.ConfigData) > 0 {
		ret.ConfigMap = CreateConfigMap(ret.DaemonSet.Namespace, rteupdate.RTEConfigMapName, opts.ConfigData)
	}

	if ret.ConfigMap != nil {
		rteConfigMapName = ret.ConfigMap.Name
	}
	rteupdate.DaemonSet(ret.DaemonSet, mf.plat, rteConfigMapName, opts.DaemonSet)

	if mf.plat == platform.OpenShift || mf.plat == platform.HyperShift {
		if mf.MachineConfig != nil {
			if opts.Name != "" {
				ret.MachineConfig.Name = ocpupdate.MakeMachineConfigName(opts.Name)
			}
			if opts.MachineConfigPoolSelector != nil {
				ret.MachineConfig.Labels = opts.MachineConfigPoolSelector.MatchLabels
			}
			// the MachineConfig installs this custom policy which is obsolete starting from OCP v4.18
		}
		ocpupdate.SecurityContextConstraint(ret.SecurityContextConstraint, ret.ServiceAccount)
		ocpupdate.SecurityContextConstraint(ret.SecurityContextConstraintV2, ret.ServiceAccount)
		rteupdate.SecurityContextWithOpts(
			ret.DaemonSet,
			rteupdate.SecurityContextOptions{
				SELinuxContextType:  selinuxTypeFromSCCVersion(opts.DaemonSet.SCCVersion, (mf.MachineConfig != nil)),
				SecurityContextName: mf.SecurityContextConstraint.Name,
			},
		)
	}

	return ret, nil
}

func selinuxTypeFromSCCVersion(ver options.SCCVersion, hasCustomPolicy bool) string {
	if ver == options.SCCV1 && hasCustomPolicy { // custom policy is the only vehicle which enables Legacy type
		return selinuxassets.RTEContextTypeLegacy
	}
	return selinuxassets.RTEContextType
}

func CreateConfigMap(namespace, name, configData string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		// TODO: why is this needed?
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			configDataField: configData,
		},
	}
	return cm
}

func (mf Manifests) ToObjects() []client.Object {
	var objs []client.Object

	if mf.ConfigMap != nil {
		objs = append(objs, mf.ConfigMap)
	}

	if mf.MachineConfig != nil {
		objs = append(objs, mf.MachineConfig)
	}

	if mf.SecurityContextConstraint != nil {
		objs = append(objs, mf.SecurityContextConstraint)
	}
	if mf.SecurityContextConstraintV2 != nil {
		objs = append(objs, mf.SecurityContextConstraintV2)
	}

	return append(objs,
		mf.Role,
		mf.RoleBinding,
		mf.ClusterRole,
		mf.ClusterRoleBinding,
		mf.DaemonSet,
		mf.ServiceAccount,
		mf.DefaultNetworkPolicy,
		mf.APIServerNetworkPolicy,
		mf.MetricsServerNetworkPolicy,
	)
}

func New(plat platform.Platform) Manifests {
	mf := Manifests{
		plat: plat,
	}

	return mf
}

func NewWithOptions(opts options.Render) (Manifests, error) {
	var err error
	mf := New(opts.Platform)

	if opts.Platform == platform.OpenShift && opts.CustomSELinuxPolicy {
		mf.MachineConfig, err = manifests.MachineConfig(opts.PlatformVersion, opts.EnableCRIHooks)
		if err != nil {
			return mf, err
		}
	}
	if opts.Platform != platform.Kubernetes {
		mf.SecurityContextConstraint, err = manifests.SecurityContextConstraint(opts.CustomSELinuxPolicy)
		if err != nil {
			return mf, err
		}
		mf.SecurityContextConstraintV2, err = manifests.SecurityContextConstraintV2()
		if err != nil {
			return mf, err
		}
	}

	mf.ServiceAccount, err = manifests.ServiceAccount(opts.Namespace)
	if err != nil {
		return mf, err
	}
	mf.Role, err = manifests.Role(opts.Namespace)
	if err != nil {
		return mf, err
	}
	mf.RoleBinding, err = manifests.RoleBinding(opts.Namespace)
	if err != nil {
		return mf, err
	}
	mf.ClusterRole, err = manifests.ClusterRole()
	if err != nil {
		return mf, err
	}
	mf.ClusterRoleBinding, err = manifests.ClusterRoleBinding()
	if err != nil {
		return mf, err
	}
	mf.DaemonSet, err = manifests.DaemonSet(opts.Namespace)
	if err != nil {
		return mf, err
	}
	mf.DefaultNetworkPolicy, err = manifests.NetworkPolicy(manifests.DefaultNetworkPolicy, opts.Namespace)
	if err != nil {
		return mf, err
	}
	mf.APIServerNetworkPolicy, err = manifests.NetworkPolicy(manifests.APIServerNetworkPolicy, opts.Namespace)
	if err != nil {
		return mf, err
	}
	mf.MetricsServerNetworkPolicy, err = manifests.NetworkPolicy(manifests.MetricsServerNetworkPolicy, opts.Namespace)
	if err != nil {
		return mf, err
	}
	return mf, nil
}

// GetManifests is deprecated, use NewWithOptions in new code
func GetManifests(plat platform.Platform, version platform.Version, namespace string, withCRIHooks, withCustomSELinuxPolicy bool) (Manifests, error) {
	return NewWithOptions(options.Render{
		Platform:            plat,
		PlatformVersion:     version,
		Namespace:           namespace,
		EnableCRIHooks:      withCRIHooks,
		CustomSELinuxPolicy: withCustomSELinuxPolicy,
	})
}
