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
	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/wait"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	"github.com/k8stopologyawareschedwg/deployer/pkg/tlog"
)

const (
	configDataField = "config.yaml"
)

type Manifests struct {
	ServiceAccount     *corev1.ServiceAccount
	Role               *rbacv1.Role
	RoleBinding        *rbacv1.RoleBinding
	ClusterRole        *rbacv1.ClusterRole
	ClusterRoleBinding *rbacv1.ClusterRoleBinding
	ConfigMap          *corev1.ConfigMap
	DaemonSet          *appsv1.DaemonSet

	// OpenShift related components
	MachineConfig             *machineconfigv1.MachineConfig
	SecurityContextConstraint *securityv1.SecurityContextConstraints

	// internal fields
	plat platform.Platform
}

func (mf Manifests) Clone() Manifests {
	ret := Manifests{
		plat: mf.plat,
		// objects
		Role:               mf.Role.DeepCopy(),
		RoleBinding:        mf.RoleBinding.DeepCopy(),
		ClusterRole:        mf.ClusterRole.DeepCopy(),
		ClusterRoleBinding: mf.ClusterRoleBinding.DeepCopy(),
		DaemonSet:          mf.DaemonSet.DeepCopy(),
		ServiceAccount:     mf.ServiceAccount.DeepCopy(),
		ConfigMap:          mf.ConfigMap.DeepCopy(),
	}

	if mf.plat == platform.OpenShift {
		ret.MachineConfig = mf.MachineConfig.DeepCopy()
		ret.SecurityContextConstraint = mf.SecurityContextConstraint.DeepCopy()
	}

	return ret
}

type RenderOptions struct {
	// DaemonSet options
	PullIfNotPresent bool
	NodeSelector     *metav1.LabelSelector

	// MachineConfig options
	MachineConfigPoolSelector *metav1.LabelSelector

	// Config Map options
	ConfigData string

	// General options
	Namespace string
	Name      string
}

func (mf Manifests) Render(options RenderOptions) Manifests {
	ret := mf.Clone()
	if ret.plat == platform.Kubernetes {
		if options.Namespace != "" {
			ret.ServiceAccount.Namespace = options.Namespace
		}
	}

	if options.Name != "" {
		ret.RoleBinding.Name = options.Name
		ret.ServiceAccount.Name = options.Name
		ret.Role.Name = options.Name
		ret.DaemonSet.Name = options.Name
		ret.ClusterRole.Name = options.Name
		ret.ClusterRoleBinding.Name = options.Name
	}

	manifests.UpdateRoleBinding(ret.RoleBinding, mf.ServiceAccount.Name, ret.ServiceAccount.Namespace)
	manifests.UpdateClusterRoleBinding(ret.ClusterRoleBinding, mf.ServiceAccount.Name, mf.ServiceAccount.Namespace)

	ret.DaemonSet.Spec.Template.Spec.ServiceAccountName = mf.ServiceAccount.Name

	rteConfigMapName := ""
	if len(options.ConfigData) > 0 {
		ret.ConfigMap = CreateConfigMap(ret.DaemonSet.Namespace, manifests.RTEConfigMapName, options.ConfigData)
	}

	if ret.ConfigMap != nil {
		rteConfigMapName = ret.ConfigMap.Name
	}
	manifests.UpdateResourceTopologyExporterDaemonSet(
		ret.DaemonSet, rteConfigMapName, options.PullIfNotPresent, options.NodeSelector)

	if mf.plat == platform.OpenShift {
		manifests.UpdateMachineConfig(ret.MachineConfig, options.Name, options.MachineConfigPoolSelector)
		manifests.UpdateSecurityContextConstraint(ret.SecurityContextConstraint, ret.ServiceAccount)
	}

	return ret
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

	return append(objs,
		mf.Role,
		mf.RoleBinding,
		mf.ClusterRole,
		mf.ClusterRoleBinding,
		mf.DaemonSet,
		mf.ServiceAccount,
	)
}

func (mf Manifests) ToCreatableObjects(hp *deployer.Helper, log tlog.Logger) []deployer.WaitableObject {
	var objs []deployer.WaitableObject
	if mf.ConfigMap != nil {
		objs = append(objs, deployer.WaitableObject{
			Obj: mf.ConfigMap,
		})
	}

	if mf.SecurityContextConstraint != nil {
		objs = append(objs, deployer.WaitableObject{
			Obj: mf.SecurityContextConstraint,
		})
	}

	if mf.MachineConfig != nil {
		// TODO: we should add functionality to wait for the MCP update
		objs = append(objs, deployer.WaitableObject{
			Obj: mf.MachineConfig,
		})
	}

	return append(objs,
		deployer.WaitableObject{Obj: mf.Role},
		deployer.WaitableObject{Obj: mf.RoleBinding},
		deployer.WaitableObject{Obj: mf.ClusterRole},
		deployer.WaitableObject{Obj: mf.ClusterRoleBinding},
		deployer.WaitableObject{Obj: mf.ServiceAccount},
		deployer.WaitableObject{
			Obj:  mf.DaemonSet,
			Wait: func() error { return wait.DaemonSetToBeRunning(hp, log, mf.DaemonSet.Namespace, mf.DaemonSet.Name) },
		},
	)
}

func (mf Manifests) ToDeletableObjects(hp *deployer.Helper, log tlog.Logger) []deployer.WaitableObject {
	objs := []deployer.WaitableObject{
		{
			Obj:  mf.DaemonSet,
			Wait: func() error { return wait.DaemonSetToBeGone(hp, log, mf.DaemonSet.Namespace, mf.DaemonSet.Name) },
		},
		{Obj: mf.Role},
		{Obj: mf.RoleBinding},
		{Obj: mf.ClusterRole},
		{Obj: mf.ClusterRoleBinding},
		{Obj: mf.ServiceAccount},
	}
	if mf.ConfigMap != nil {
		objs = append(objs, deployer.WaitableObject{Obj: mf.ConfigMap})
	}
	if mf.SecurityContextConstraint != nil {
		objs = append(objs, deployer.WaitableObject{
			Obj: mf.SecurityContextConstraint,
		})
	}
	if mf.MachineConfig != nil {
		objs = append(objs, deployer.WaitableObject{
			// TODO: we should add functionality to wait for the MCP update
			Obj: mf.MachineConfig,
		})
	}
	return objs
}

func New(plat platform.Platform) Manifests {
	mf := Manifests{
		plat: plat,
	}

	return mf
}

func GetManifests(plat platform.Platform, namespace string) (Manifests, error) {
	var err error
	mf := New(plat)

	if plat == platform.OpenShift {
		mf.MachineConfig, err = manifests.MachineConfig(manifests.ComponentResourceTopologyExporter)
		if err != nil {
			return mf, err
		}

		mf.SecurityContextConstraint, err = manifests.SecurityContextConstraint(manifests.ComponentResourceTopologyExporter)
		if err != nil {
			return mf, err
		}
	}

	mf.ServiceAccount, err = manifests.ServiceAccount(manifests.ComponentResourceTopologyExporter, "", namespace)
	if err != nil {
		return mf, err
	}
	mf.Role, err = manifests.Role(manifests.ComponentResourceTopologyExporter, "", namespace)
	if err != nil {
		return mf, err
	}
	mf.RoleBinding, err = manifests.RoleBinding(manifests.ComponentResourceTopologyExporter, "", namespace)
	if err != nil {
		return mf, err
	}
	mf.ClusterRole, err = manifests.ClusterRole(manifests.ComponentResourceTopologyExporter, "")
	if err != nil {
		return mf, err
	}
	mf.ClusterRoleBinding, err = manifests.ClusterRoleBinding(manifests.ComponentResourceTopologyExporter, "")
	if err != nil {
		return mf, err
	}
	mf.DaemonSet, err = manifests.DaemonSet(manifests.ComponentResourceTopologyExporter, plat, namespace)
	if err != nil {
		return mf, err
	}
	return mf, nil
}
