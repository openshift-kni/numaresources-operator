/*
 * Copyright 2022 Red Hat, Inc.
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

package validator

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/version"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
	"github.com/k8stopologyawareschedwg/deployer/pkg/validator"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"
)

type ValidatorData struct {
	tasEnabledNodeNames sets.String
	kConfigs            map[string]*kubeletconfigv1beta1.KubeletConfiguration
	nrtCrdMissing       bool
	nrtList             *nrtv1alpha1.NodeResourceTopologyList
	versionInfo         *version.Info
}

type collectHelper func(ctx context.Context, cli client.Client, data *ValidatorData) error

func Collect(ctx context.Context, cli client.Client) (ValidatorData, error) {
	data := ValidatorData{}

	ver, err := getClusterVersionInfo()
	if err != nil {
		return data, err
	}

	data.versionInfo = ver

	// Get Node Names for those nodes with TAS enabled
	nroNamespacedName := types.NamespacedName{
		Name: objectnames.DefaultNUMAResourcesOperatorCrName,
	}
	nroInstance := &nropv1alpha1.NUMAResourcesOperator{}
	err = cli.Get(ctx, nroNamespacedName, nroInstance)
	if err != nil {
		return data, err
	}

	nroMcps, err := machineconfigpools.GetListByNodeGroups(ctx, cli, nroInstance.Spec.NodeGroups)
	if err != nil {
		return data, err
	}

	enabledNodeNames := sets.NewString()
	for _, mcp := range nroMcps {
		nodes, err := machineconfigpools.GetNodeListFromMachineConfigPool(ctx, cli, *mcp)
		if err != nil {
			return data, err
		}
		enabledNodeNames.Insert(getNodeNames(nodes)...)
	}
	data.tasEnabledNodeNames = enabledNodeNames

	for _, helper := range []collectHelper{
		CollectKubeletConfig,
		CollectNodeResourceTopologies,
	} {
		err = helper(ctx, cli, &data)
		if err != nil {
			return data, err
		}
	}
	return data, nil
}

type validateHelper func(data ValidatorData) ([]validator.ValidationResult, error)

func Validate(data ValidatorData) ([]validator.ValidationResult, error) {
	var ret []validator.ValidationResult
	for _, helper := range []validateHelper{
		ValidateKubeletConfig,
		ValidateNodeResourceTopologies,
	} {
		res, err := helper(data)
		if err != nil {
			return ret, err
		}
		ret = append(ret, res...)
	}
	return ret, nil
}

func getClusterVersionInfo() (*version.Info, error) {
	cli, err := clientutil.NewDiscoveryClient()
	if err != nil {
		return nil, err
	}
	ver, err := cli.ServerVersion()
	if err != nil {
		return nil, err
	}
	return ver, nil
}

func getNodeNames(nodes []corev1.Node) []string {
	var nodeNames []string
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}

	return nodeNames
}
