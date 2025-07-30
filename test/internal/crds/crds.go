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

package crds

import (
	"context"

	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	NRTName = "noderesourcetopologies.topology.node.k8s.io"
	NROName = "numaresourcesoperators.nodetopology.openshift.io"
	NRSName = "numaresourcesschedulers.nodetopology.openshift.io"
)

func GetNRT(ctx context.Context, cli client.Client) (*apiextensionv1.CustomResourceDefinition, error) {
	return getByName(ctx, cli, NRTName)
}

func GetNRO(ctx context.Context, cli client.Client) (*apiextensionv1.CustomResourceDefinition, error) {
	return getByName(ctx, cli, NROName)
}

func GetNRS(ctx context.Context, cli client.Client) (*apiextensionv1.CustomResourceDefinition, error) {
	return getByName(ctx, cli, NRSName)
}

func getByName(ctx context.Context, cli client.Client, crdName string) (*apiextensionv1.CustomResourceDefinition, error) {
	crd := apiextensionv1.CustomResourceDefinition{}
	err := cli.Get(ctx, client.ObjectKey{Name: crdName}, &crd)
	return &crd, err
}
