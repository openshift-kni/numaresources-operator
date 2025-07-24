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

package dangling

import (
	"context"
	"errors"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
)

func DeleteUnusedDaemonSets(cli client.Client, ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) error {
	klog.V(3).Info("Delete dangling Daemonsets start")
	defer klog.V(3).Info("Delete dangling Daemonsets end")

	var daemonSetList appsv1.DaemonSetList
	if err := cli.List(ctx, &daemonSetList, &client.ListOptions{Namespace: instance.Namespace}); err != nil {
		klog.ErrorS(err, "error while getting Daemonset list")
		return err
	}

	expectedDaemonSetNames := buildDaemonSetNames(instance, trees)

	var errs error
	deleted := 0
	for _, ds := range daemonSetList.Items {
		if !isOwnedBy(ds.GetObjectMeta(), instance) {
			continue
		}
		if expectedDaemonSetNames.Has(ds.Name) {
			continue
		}
		if err := cli.Delete(ctx, &ds); err != nil {
			klog.ErrorS(err, "error while deleting dangling daemonset", "DaemonSet", ds.Namespace+"/"+ds.Name)
			errs = errors.Join(errs, err)
			continue
		}
		klog.V(3).InfoS("dangling Daemonset deleted", "name", ds.Name)
		deleted += 1
	}
	if deleted > 0 {
		klog.V(2).InfoS("Delete dangling Daemonsets", "deletedCount", deleted)
	}
	return errs
}

func DeleteUnusedMachineConfigs(cli client.Client, ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) error {
	klog.V(3).Info("Delete dangling Machineconfigs start")
	defer klog.V(3).Info("Delete dangling Machineconfigs end")
	var machineConfigList machineconfigv1.MachineConfigList
	if err := cli.List(ctx, &machineConfigList); err != nil {
		klog.ErrorS(err, "error while getting MachineConfig list")
		return err
	}

	expectedMachineConfigNames := sets.NewString()
	for _, tree := range trees {
		for _, mcp := range tree.MachineConfigPools {
			expectedMachineConfigNames = expectedMachineConfigNames.Insert(objectnames.GetMachineConfigName(instance.Name, mcp.Name))
		}
	}

	var errs error
	deleted := 0
	for _, mc := range machineConfigList.Items {
		if !isOwnedBy(mc.GetObjectMeta(), instance) {
			continue
		}
		if expectedMachineConfigNames.Has(mc.Name) {
			continue
		}
		if err := cli.Delete(ctx, &mc); err != nil {
			klog.ErrorS(err, "error while deleting dangling machineconfig", "MachineConfig", mc.Name)
			errs = errors.Join(errs, err)
			continue
		}
		klog.V(3).InfoS("dangling Machineconfig deleted", "name", mc.Name)
		deleted += 1
	}
	if deleted > 0 {
		klog.V(2).InfoS("Delete dangling Machineconfigs", "deletedCount", deleted)
	}
	return errs
}

func isOwnedBy(element metav1.Object, owner metav1.Object) bool {
	for _, ref := range element.GetOwnerReferences() {
		if ref.UID == owner.GetUID() {
			return true
		}
	}
	return false
}

func buildDaemonSetNames(instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) sets.Set[string] {
	expectedDaemonSetNames := sets.New[string]()
	for _, tree := range trees {
		// the earlier validation step ensures that if poolName is not nil, then it's not empty either
		poolName := tree.NodeGroup.PoolName // shortcut
		if poolName != nil {
			expectedDaemonSetNames.Insert(objectnames.GetComponentName(instance.Name, *poolName))
			continue
		}
		for _, mcp := range tree.MachineConfigPools {
			expectedDaemonSetNames = expectedDaemonSetNames.Insert(objectnames.GetComponentName(instance.Name, mcp.Name))
		}
	}
	return expectedDaemonSetNames
}
