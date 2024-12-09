/*
 * Copyright 2024 Red Hat, Inc.
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

package version

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/lib/version"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
)

func OfOperator(ctx context.Context, cli client.Client, namespace string) (*version.OperatorVersion, error) {
	csvList := &operatorsv1alpha1.ClusterServiceVersionList{}

	err := cli.List(ctx, csvList, &client.ListOptions{
		Namespace: namespace,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to fetch ClusterServiceVersion")
	}
	if len(csvList.Items) != 1 {
		return nil, fmt.Errorf("expect only one CSV object under %s namespace", namespace)
	}
	return &csvList.Items[0].Spec.Version, nil
}
