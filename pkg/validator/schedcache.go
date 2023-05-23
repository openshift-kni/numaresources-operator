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

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
	deployervalidator "github.com/k8stopologyawareschedwg/deployer/pkg/validator"
	"github.com/openshift-kni/numaresources-operator/internal/nodes"
	"github.com/openshift-kni/numaresources-operator/internal/schedcache"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ValidatorSchedCache = "schedcache"
)

func CollectSchedCache(ctx context.Context, cli client.Client, data *ValidatorData) error {
	k8sCli, err := clientutil.NewK8s()
	if err != nil {
		return err
	}

	workers, err := nodes.GetWorkerNodes(cli, ctx)
	if err != nil {
		return err
	}

	_, unsynced, err := schedcache.HasSynced(ctx, cli, k8sCli, nodes.GetNames(workers))
	if err != nil {
		return err
	}

	data.unsynchedCaches = unsynced

	return nil
}

func ValidateSchedCache(data ValidatorData) ([]deployervalidator.ValidationResult, error) {
	var ret []deployervalidator.ValidationResult
	for nodeName := range data.unsynchedCaches {
		ret = append(ret, deployervalidator.ValidationResult{
			Area:      deployervalidator.AreaCluster,
			Component: "scheduler",
			Node:      nodeName,
			Setting:   "cache",
			Expected:  "clean",
			Detected:  "unsync",
		})
	}

	return ret, nil
}
