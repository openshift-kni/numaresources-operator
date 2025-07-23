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

// Package dangling takes care of detecting and removing dangling object from removed NodeGroups.
// If we edit nodegroups in such a way that a set of nodes previously managed is no longer managed, the related MachineConfig is left lingering.
// We need to explicit cleanup objects with are 1:1 with NodeGroups because NodeGroups are not a separate object, and the main NRO object is set as
// the owner of all the generated object. But in this scenario (NodeGroup no longer managed), the main NRO object is NOT deleted, so the dependant
// objects are left unnecessarily lingering. In hindsight, we should probably have set a NUMAResourcesOperator own NodeGroups, or just allow more than
// a NUMAResourcesOperator object, but that ship as sailed and now a NUMAResourcesOperator object is 1:N to NodeGroups (and the latter are not K8S objects).
package dangling

import (
	"context"
	"errors"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
)

func DeleteUnusedDaemonSets(cli client.Client, ctx context.Context, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree) ([]appsv1.DaemonSet, error) {
	klog.V(3).Info("Delete dangling Daemonsets start")
	defer klog.V(3).Info("Delete dangling Daemonsets end")

	var daemonSetList appsv1.DaemonSetList
	if err := cli.List(ctx, &daemonSetList, &client.ListOptions{Namespace: instance.Namespace}); err != nil {
		klog.ErrorS(err, "error while getting Daemonset list")
		return nil, err
	}

	expectedDaemonSetNames := buildDaemonSetNames(instance, trees)

	var activeDaemonSets []appsv1.DaemonSet
	var errs error
	deleted := 0
	for _, ds := range daemonSetList.Items {
		if !isOwnedBy(ds.GetObjectMeta(), instance) {
			continue
		}
		if expectedDaemonSetNames.Has(ds.Name) {
			activeDaemonSets = append(activeDaemonSets, ds)
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
	return activeDaemonSets, errs
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

func DeleteUnusedNodeResourcesTopologies(cli client.Client, ctx context.Context, activeDaemonSets []appsv1.DaemonSet) error {
	klog.V(3).Info("Delete dangling NodeResourcesTopologies start")
	defer klog.V(3).Info("Delete dangling NodeResourcesTopologies end")

	var nodeResourcesTopologiesList v1alpha2.NodeResourceTopologyList
	if err := cli.List(ctx, &nodeResourcesTopologiesList); err != nil {
		klog.ErrorS(err, "error while getting NodeResourcesTopologies list")
		return err
	}

	nrtsToDelete := nodeResourcesTopologiesList.DeepCopy().Items
	if len(activeDaemonSets) != 0 {
		nrtsToDelete = []v1alpha2.NodeResourceTopology{}
		var targetNodes []v1.Node
		for _, ds := range activeDaemonSets {
			var dsNodes v1.NodeList
			labelSelector := metav1.LabelSelector{MatchLabels: ds.Spec.Template.Spec.NodeSelector}
			sel, err := metav1.LabelSelectorAsSelector(&labelSelector)
			if err != nil {
				klog.ErrorS(err, "failed to parse label selector", "daemonset", ds.Name)
				return err
			}

			if err := cli.List(ctx, &dsNodes, &client.ListOptions{LabelSelector: sel}); err != nil {
				klog.ErrorS(err, "error while getting nodes list that are targeted by RTE DaemonSet")
				return err
			}
			targetNodes = append(targetNodes, dsNodes.Items...)
		}

		targetNodeNames := sets.New[string]()
		for _, node := range targetNodes {
			targetNodeNames = targetNodeNames.Insert(node.Name)
		}

		for _, nrt := range nodeResourcesTopologiesList.Items {
			if !targetNodeNames.Has(nrt.Name) {
				nrtsToDelete = append(nrtsToDelete, nrt)
			}
		}
	}

	var errs error
	deleted := 0
	for _, nrt := range nrtsToDelete {
		if err := cli.Delete(ctx, &nrt); err != nil {
			klog.ErrorS(err, "error while deleting dangling noderesourcetopology", "NodeResourcesTopology", nrt.Name)
			errs = errors.Join(errs, err)
			continue
		}
		klog.V(3).InfoS("dangling noderesourcetopology deleted", "name", nrt.Name)
		deleted += 1
	}
	if deleted > 0 {
		klog.V(2).InfoS("Delete dangling noderesourcetopologies", "deletedCount", deleted)
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
