/*
 * Copyright 2026 Red Hat, Inc.
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

package numazone

import (
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/pkg/numazoneresource"
	numazonemanifests "github.com/openshift-kni/numaresources-operator/pkg/numazoneresource/manifests/numazone"
	numazoneupdate "github.com/openshift-kni/numaresources-operator/pkg/numazoneresource/objectupdate/numazone"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
	rtestate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
)

func TreeAgnostic(ctx context.Context, cli client.Client, mf numazonemanifests.Manifests) []objectstate.ObjectState {
	var states []objectstate.ObjectState
	for _, desired := range mf.TreeAgnosticObjects() {
		state := fromClient(ctx, cli, desired.DeepCopyObject().(client.Object))
		if _, ok := desired.(*corev1.ServiceAccount); ok {
			state.Merge = merge.ServiceAccountForUpdate
		}
		states = append(states, state)
	}
	return states
}

func PerTree(ctx context.Context, cli client.Client, mf numazonemanifests.Manifests, plat platform.Platform, instanceName string, tree nodegroupv1.Tree, image string, pullPolicy corev1.PullPolicy) []objectstate.ObjectState {
	conf := tree.NodeGroup.NormalizeNumazoneConfig()
	rteConf := tree.NodeGroup.NormalizeConfig()
	var states []objectstate.ObjectState
	for _, poolName := range nodegroupv1.GetTreePoolsNames(tree) {
		ds := mf.DaemonSet.DeepCopy()
		ds.Name = numazoneresource.DaemonSetName(instanceName, poolName)
		err := numazoneupdate.DaemonSetSettings(ds, *conf.Mode, conf.LogLevel, image, pullPolicy, rteConf.Tolerations)
		if plat == platform.HyperShift {
			ds.Spec.Template.Spec.NodeSelector = map[string]string{rtestate.HyperShiftNodePoolLabel: poolName}
		} else {
			for _, mcp := range tree.MachineConfigPools {
				if mcp.Name != poolName {
					continue
				}
				if mcp.Spec.NodeSelector == nil {
					err = fmt.Errorf("the machine config pool %q does not have node selector", poolName)
				} else {
					ds.Spec.Template.Spec.NodeSelector = maps.Clone(mcp.Spec.NodeSelector.MatchLabels)
				}
				break
			}
		}
		state := fromClient(ctx, cli, ds)
		state.UpdateError = err
		states = append(states, state)
	}
	return states
}

func fromClient(ctx context.Context, cli client.Client, desired client.Object) objectstate.ObjectState {
	existing := desired.DeepCopyObject().(client.Object)
	state := objectstate.ObjectState{
		Desired: desired,
		Compare: compare.Object,
		Merge:   merge.ObjectForUpdate,
	}
	state.Error = cli.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if state.Error == nil {
		// Typed clients may omit TypeMeta; it must not trigger operand updates.
		desired.GetObjectKind().SetGroupVersionKind(existing.GetObjectKind().GroupVersionKind())
		state.Existing = existing
	}
	return state
}
