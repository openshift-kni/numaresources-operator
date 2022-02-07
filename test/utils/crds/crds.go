/*
Copyright 2022 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package crds

import (
	"context"

	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	CrdNRTName  = "noderesourcetopologies.topology.node.k8s.io"
	CrdNROName  = "numaresourcesoperators.nodetopology.openshift.io"
	CrdNROSName = "numaresourcesschedulers.nodetopology.openshift.io"
)

func GetByName(aclient client.Client, crdName string) (*apiextensionv1.CustomResourceDefinition, error) {
	crd := &apiextensionv1.CustomResourceDefinition{}
	key := client.ObjectKey{
		Name: crdName,
	}
	err := aclient.Get(context.TODO(), key, crd)
	return crd, err
}
