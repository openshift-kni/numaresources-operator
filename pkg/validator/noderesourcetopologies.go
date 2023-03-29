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
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	topologyclientset "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/generated/clientset/versioned"

	deployervalidator "github.com/k8stopologyawareschedwg/deployer/pkg/validator"
)

const (
	ValidatorNodeResourceTopologies = "nrt"
)

func CollectNodeResourceTopologies(ctx context.Context, _ client.Client, data *ValidatorData) error {

	cli, err := getTopologyClient()
	if err != nil {
		return err
	}

	nrtList, err := cli.TopologyV1alpha2().NodeResourceTopologies().List(ctx, metav1.ListOptions{})
	if err != nil {
		if IsNodeResourceTopologyCRDMissing(err) {
			data.nrtCrdMissing = true
			return nil
		}
		return err
	}

	data.nrtList = nrtList
	return nil
}

func ValidateNodeResourceTopologies(data ValidatorData) ([]deployervalidator.ValidationResult, error) {
	var ret []deployervalidator.ValidationResult
	if data.nrtCrdMissing {
		ret = append(ret, deployervalidator.ValidationResult{
			Area:      deployervalidator.AreaCluster,
			Component: "NodeResourceTopology",
			Setting:   "installed",
			Expected:  "true",
			Detected:  "false",
		})
		return ret, nil
	}
	if len(data.nrtList.Items) == 0 {
		ret = append(ret, deployervalidator.ValidationResult{
			Area:      deployervalidator.AreaCluster,
			Component: "NodeResourceTopology",
			Setting:   "items",
			Expected:  "> 0",
			Detected:  "0",
		})
		return ret, nil
	}
	if len(data.nrtList.Items) != data.tasEnabledNodeNames.Len() {
		ret = append(ret, deployervalidator.ValidationResult{
			Area:      deployervalidator.AreaCluster,
			Component: "NodeResourceTopology",
			Setting:   "items",
			Expected:  fmt.Sprintf("%d", data.tasEnabledNodeNames.Len()),
			Detected:  fmt.Sprintf("%d", len(data.nrtList.Items)),
		})
		return ret, nil
	}
	return ret, nil
}

func IsNodeResourceTopologyCRDMissing(err error) bool {
	if status := apierrors.APIStatus(nil); errors.As(err, &status) {
		st := status.Status()
		if st.Details.Group == "topology.node.k8s.io" && st.Details.Kind == "noderesourcetopologies" {
			return true
		}
	}
	return false
}

func getTopologyClient() (*topologyclientset.Clientset, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	topologyClient, err := topologyclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return topologyClient, nil
}
