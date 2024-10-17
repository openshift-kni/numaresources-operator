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

	"sigs.k8s.io/controller-runtime/pkg/client"

	deployervalidator "github.com/k8stopologyawareschedwg/deployer/pkg/validator"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"

	"github.com/openshift-kni/numaresources-operator/internal/dangling"
)

const (
	ValidatorDangling = "objdangling"
)

func CollectDangling(ctx context.Context, cli client.Client, data *ValidatorData) error {
	nroKey := client.ObjectKey{
		Name: objectnames.DefaultNUMAResourcesOperatorCrName,
	}
	nroObj := &nropv1.NUMAResourcesOperator{}
	err := cli.Get(ctx, nroKey, nroObj)
	if err != nil {
		return err
	}

	data.danglingObjects = []client.ObjectKey{}

	dsKeys, err := dangling.DaemonSetKeys(cli, ctx, nroObj)
	if err != nil {
		return err
	}
	data.danglingObjects = append(data.danglingObjects, dsKeys...)

	mcKeys, err := dangling.MachineConfigKeys(cli, ctx, nroObj)
	if err != nil {
		return err
	}
	data.danglingObjects = append(data.danglingObjects, mcKeys...)

	return nil
}

func ValidateDangling(data ValidatorData) ([]deployervalidator.ValidationResult, error) {
	var ret []deployervalidator.ValidationResult
	for _, objKey := range data.danglingObjects {
		ret = append(ret, deployervalidator.ValidationResult{
			Area:      deployervalidator.AreaCluster,
			Component: "object",
			Setting:   objKey.String(),
			Expected:  "absent",
			Detected:  "present",
		})
	}
	return ret, nil
}
