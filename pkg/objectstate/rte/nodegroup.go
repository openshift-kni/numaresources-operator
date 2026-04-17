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
 * Copyright 2025 Red Hat, Inc.
 */

package rte

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

type nodeGroupFinder struct {
	em        *ExistingManifests
	instance  *nropv1.NUMAResourcesOperator
	namespace string
}

func (obj nodeGroupFinder) Name() string {
	return "nodeGroup"
}

func (obj nodeGroupFinder) UpdateFromClient(ctx context.Context, cli client.Client, tree nodegroupv1.Tree) {
	generatedName := objectnames.GetComponentName(obj.instance.Name, *tree.NodeGroup.PoolName)
	daemonConfigName := objectnames.GetDaemonConfigName(obj.instance.Name, *tree.NodeGroup.PoolName)
	obj.em.daemonSets[generatedName] = getDaemonSetManifest(ctx, cli, obj.namespace, generatedName, daemonConfigName)

	dcmKey := client.ObjectKey{Namespace: obj.namespace, Name: daemonConfigName}
	dcm := &corev1.ConfigMap{}
	dcmm := daemonConfigManifest{}
	if dcmm.configMapError = cli.Get(ctx, dcmKey, dcm); dcmm.configMapError == nil {
		dcmm.configMap = dcm
	}
	obj.em.daemonConfigs[daemonConfigName] = dcmm
}

func (obj nodeGroupFinder) FindState(mf Manifests, tree nodegroupv1.Tree) []objectstate.ObjectState {
	var ret []objectstate.ObjectState
	var existingDs client.Object
	var loadError error
	var rteConfigHash string
	var daemonConfigHash string

	poolName := *tree.NodeGroup.PoolName

	generatedName := objectnames.GetComponentName(obj.instance.Name, poolName)
	daemonConfigName := objectnames.GetDaemonConfigName(obj.instance.Name, poolName)
	existingDaemonSet, ok := obj.em.daemonSets[generatedName]
	if ok {
		existingDs = existingDaemonSet.daemonSet
		loadError = existingDaemonSet.daemonSetError
		rteConfigHash = existingDaemonSet.rteConfigHash
		daemonConfigHash = existingDaemonSet.daemonConfigHash
	} else {
		loadError = fmt.Errorf("failed to find daemon set %s/%s", mf.Core.DaemonSet.Namespace, mf.Core.DaemonSet.Name)
	}

	desiredDaemonSet := mf.Core.DaemonSet.DeepCopy()
	desiredDaemonSet.Name = generatedName

	var updateError error
	desiredDaemonSet.Spec.Template.Spec.NodeSelector = map[string]string{
		HyperShiftNodePoolLabel: poolName,
	}

	gdm := GeneratedDesiredManifest{
		ClusterPlatform:       obj.em.plat,
		MachineConfigPool:     nil,
		NodeGroup:             tree.NodeGroup.DeepCopy(),
		DaemonSet:             desiredDaemonSet,
		RTEConfigHash:         rteConfigHash,
		DaemonConfigHash:      daemonConfigHash,
		IsCustomPolicyEnabled: annotations.IsCustomPolicyEnabled(tree.NodeGroup.Annotations),
		SecOpts:               mf.securityContextOptions(annotations.IsCustomPolicyEnabled(tree.NodeGroup.Annotations)),
	}

	err := obj.em.updater(poolName, &gdm)
	if err != nil {
		updateError = fmt.Errorf("daemonset for pool %q: update failed: %w", poolName, err)
	}

	ret = append(ret, objectstate.ObjectState{
		Existing:    existingDs,
		Error:       loadError,
		UpdateError: updateError,
		Desired:     desiredDaemonSet,
		Compare:     compare.Object,
		Merge:       merge.ObjectForUpdate,
	})

	if gdm.DaemonConfigMap != nil {
		existingDCM, _ := obj.em.daemonConfigs[daemonConfigName]
		ret = append(ret, objectstate.ObjectState{
			Existing: existingDCM.configMap,
			Error:    existingDCM.configMapError,
			Desired:  gdm.DaemonConfigMap,
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		})
	}

	return ret
}
