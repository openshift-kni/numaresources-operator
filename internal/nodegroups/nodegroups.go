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

package nodegroups

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	mcov1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform/detect"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/hypershift/consts"
	"github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
)

func GetNodesFrom(ctx context.Context, cli client.Client, nodeGroups []nropv1.NodeGroup) ([]corev1.Node, error) {
	plat, err := detect.Platform(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make([]corev1.Node, 0)
	if plat == platform.HyperShift {
		nodeList := corev1.NodeList{}
		for _, nodeGroup := range nodeGroups {
			opts := &client.ListOptions{
				LabelSelector: labels.SelectorFromSet(map[string]string{
					consts.NodePoolNameLabel: *nodeGroup.PoolName,
				}),
			}
			if err := cli.List(ctx, &nodeList, opts); err != nil {
				return nodes, err
			}
			nodes = append(nodes, nodeList.Items...)
		}
		return nodes, nil
	}

	nroMcps, err := machineconfigpools.GetListByNodeGroupsV1(ctx, cli, nodeGroups)
	if err != nil {
		return nodes, err
	}
	for _, mcp := range nroMcps {
		mcpNodes, err := machineconfigpools.GetNodesFrom(ctx, cli, mcp)
		if err != nil {
			return nodes, err
		}
		nodes = append(nodes, mcpNodes...)
	}
	return nodes, nil
}

func GetPoolNamesFrom(ctx context.Context, cli client.Client, nodeGroups []nropv1.NodeGroup) ([]string, error) {
	poolNames := make([]string, 0, len(nodeGroups))
	for _, nodeGroup := range nodeGroups {
		// this should cover HyperShift cases and OpenShift cases when PoolName in use
		if nodeGroup.PoolName != nil && *nodeGroup.PoolName != "" {
			poolNames = append(poolNames, *nodeGroup.PoolName)
			continue
		}
		// fallback to MCPs if PoolName is not in use
		mcps, err := machineconfigpools.GetListByNodeGroupsV1(ctx, cli, []nropv1.NodeGroup{nodeGroup})
		if err != nil {
			return poolNames, err
		}
		for _, mcp := range mcps {
			if mcp.Spec.NodeSelector == nil {
				klog.Warningf("the machine config pool %q does not have node selector", mcp.Name)
				continue
			}
			poolNames = append(poolNames, mcp.Name)
		}
	}
	return poolNames, nil
}

func NodeSelectorFromPoolName(ctx context.Context, cli client.Client, poolName string) (map[string]string, error) {
	plat, err := detect.Platform(ctx)
	if err != nil {
		return nil, err
	}
	if plat == platform.HyperShift {
		return map[string]string{
			consts.NodePoolNameLabel: poolName,
		}, nil
	}
	mcp := &mcov1.MachineConfigPool{}
	err = cli.Get(ctx, client.ObjectKey{Name: poolName}, mcp)
	if err != nil {
		return nil, err
	}
	return mcp.Spec.NodeSelector.MatchLabels, nil
}
