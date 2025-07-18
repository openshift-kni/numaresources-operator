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
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/version"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
	deployervalidator "github.com/k8stopologyawareschedwg/deployer/pkg/validator"
	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/nodegroups"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
)

type Report struct {
	Succeeded bool                                 `json:"succeeded"`
	Errors    []deployervalidator.ValidationResult `json:"errors,omitempty"`
}

func Requested(what string) (sets.Set[string], error) {
	items := strings.FieldsFunc(what, func(c rune) bool {
		return c == ','
	})
	available := Available()

	// handle special case first
	for _, item := range items {
		if strings.ToLower(item) == "help" {
			return sets.New[string]("help"), nil
		}
		if strings.ToLower(item) == "all" {
			return available, nil
		}
	}

	ret := sets.New[string]()
	for _, item := range items {
		vd := strings.ToLower(strings.TrimSpace(item))
		if !available.Has(vd) {
			return nil, fmt.Errorf("unknown validator: %q", item)
		}
		ret.Insert(vd)
	}
	return ret, nil
}

func Available() sets.Set[string] {
	return sets.New[string](
		ValidatorKubeletConfig,
		ValidatorNodeResourceTopologies,
		ValidatorPodStatus,
		ValidatorSchedCache,
	)
}

type ValidatorData struct {
	tasEnabledNodeNames  sets.Set[string]
	nonRunningPodsByNode map[string]map[string]corev1.PodPhase
	kConfigs             map[string]*kubeletconfigv1beta1.KubeletConfiguration
	unsynchedCaches      map[string]sets.Set[string]
	nrtCrdMissing        bool
	nrtList              *nrtv1alpha2.NodeResourceTopologyList
	versionInfo          *version.Info
	what                 sets.Set[string]
}

type CollectFunc func(ctx context.Context, cli client.Client, data *ValidatorData) error

func Collectors() map[string]CollectFunc {
	return map[string]CollectFunc{
		ValidatorKubeletConfig:          CollectKubeletConfig,
		ValidatorNodeResourceTopologies: CollectNodeResourceTopologies,
		ValidatorPodStatus:              CollectPodStatus,
		ValidatorSchedCache:             CollectSchedCache,
	}
}

func Collect(ctx context.Context, cli client.Client, userLabels string, what sets.Set[string]) (ValidatorData, error) {
	collectors := Collectors()
	colFns := []CollectFunc{}
	for _, vd := range what.UnsortedList() {
		fn, ok := collectors[vd]
		if !ok {
			return ValidatorData{}, fmt.Errorf("unsupported collector: %q", vd)
		}
		colFns = append(colFns, fn)
	}

	data := ValidatorData{
		what: what,
	}

	ver, err := getClusterVersionInfo()
	if err != nil {
		return data, err
	}

	data.versionInfo = ver

	// Get Node Names for those nodes with TAS enabled
	var enabledNodeNames sets.Set[string]
	if userLabels != "" {
		enabledNodeNames, err = GetNodesByLabels(ctx, cli, userLabels)
	} else {
		enabledNodeNames, err = GetNodesByNRO(ctx, cli)
	}
	if err != nil {
		return data, err
	}

	fmt.Fprintf(os.Stderr, "INFO>>>>: inspecting nodes: %s\n", strings.Join(enabledNodeNames.UnsortedList(), ","))
	data.tasEnabledNodeNames = enabledNodeNames

	for _, helper := range colFns {
		err = helper(ctx, cli, &data)
		if err != nil {
			return data, err
		}
	}
	return data, nil
}

func GetNodesByLabels(ctx context.Context, cli client.Client, userLabels string) (sets.Set[string], error) {
	enabledNodeNames := sets.New[string]()
	sel, err := labels.Parse(userLabels)
	if err != nil {
		return enabledNodeNames, err
	}

	nodeList := &corev1.NodeList{}
	err = cli.List(ctx, nodeList, &client.ListOptions{LabelSelector: sel})
	if err != nil {
		return enabledNodeNames, err
	}

	enabledNodeNames.Insert(getNodeNames(nodeList.Items)...)
	return enabledNodeNames, err
}

func GetNodesByNRO(ctx context.Context, cli client.Client) (sets.Set[string], error) {
	enabledNodeNames := sets.New[string]()
	nroNamespacedName := types.NamespacedName{
		Name: objectnames.DefaultNUMAResourcesOperatorCrName,
	}
	nroInstance := &nropv1.NUMAResourcesOperator{}
	err := cli.Get(ctx, nroNamespacedName, nroInstance)
	if err != nil {
		return enabledNodeNames, err
	}
	nodes, err := nodegroups.GetNodesFrom(ctx, cli, nroInstance.Spec.NodeGroups)
	if err != nil {
		return enabledNodeNames, err
	}
	enabledNodeNames.Insert(getNodeNames(nodes)...)

	return enabledNodeNames, nil
}

type ValidateFunc func(data ValidatorData) ([]deployervalidator.ValidationResult, error)

func Validators() map[string]ValidateFunc {
	return map[string]ValidateFunc{
		ValidatorKubeletConfig:          ValidateKubeletConfig,
		ValidatorNodeResourceTopologies: ValidateNodeResourceTopologies,
		ValidatorPodStatus:              ValidatePodStatus,
		ValidatorSchedCache:             ValidateSchedCache,
	}
}

func Validate(data ValidatorData) (Report, error) {
	validators := Validators()
	valFns := []ValidateFunc{}
	for _, vd := range data.what.UnsortedList() {
		fn, ok := validators[vd]
		if !ok {
			return Report{}, fmt.Errorf("unsupported validator: %q", vd)
		}
		valFns = append(valFns, fn)
	}

	var ret []deployervalidator.ValidationResult
	for _, helper := range valFns {
		res, err := helper(data)
		if err != nil {
			return Report{}, err
		}
		ret = append(ret, res...)
	}

	return Report{
		Succeeded: len(ret) == 0,
		Errors:    ret,
	}, nil
}

func GetKubeletConfigurationsFromTASEnabledNodes(ctx context.Context, cli client.Client) (map[string]*kubeletconfigv1beta1.KubeletConfiguration, error) {
	tasEnabledNodeNames, err := GetNodesByNRO(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("error while trying to get TAS enabled nodes. error: %w", err)
	}

	validatorData := ValidatorData{
		tasEnabledNodeNames: tasEnabledNodeNames,
	}
	if err := CollectKubeletConfig(ctx, cli, &validatorData); err != nil {
		return nil, fmt.Errorf("error while collecting kubeletconfigs from TAS enabled nodes. error: %w", err)
	}

	return validatorData.kConfigs, nil
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
