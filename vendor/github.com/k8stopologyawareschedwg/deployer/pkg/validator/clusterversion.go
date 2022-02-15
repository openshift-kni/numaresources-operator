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
 * Copyright 2022 Red Hat, Inc.
 */

package validator

import (
	"k8s.io/client-go/discovery"

	goversion "github.com/aquasecurity/go-version/pkg/version"
)

const (
	ExpectedMinKubeVersion = "1.21"
)

const (
	ComponentAPIVersion = "API Version"
)

func (vd *Validator) ValidateClusterVersion(cli *discovery.DiscoveryClient) ([]ValidationResult, error) {
	ver, err := cli.ServerVersion()
	if err != nil {
		return nil, err
	}
	vd.serverVersion = ver
	vrs := ValidateClusterVersion(ver.GitVersion)
	vd.results = append(vd.results, vrs...)
	return vrs, nil
}

func ValidateClusterVersion(clusterVersion string) []ValidationResult {
	ok, err := isAPIVersionAtLeast(clusterVersion, ExpectedMinKubeVersion)
	if err != nil {
		return []ValidationResult{
			{
				/* no specific nodes: all are affected! */
				Area:      AreaCluster,
				Component: ComponentAPIVersion,
				/* no specific Setting: implicit in the component! */
				Expected: "valid version",
				Detected: err.Error(),
			},
		}
	}
	if !ok {
		return []ValidationResult{
			{
				/* no specific nodes: all are affected! */
				Area:      AreaCluster,
				Component: ComponentAPIVersion,
				/* no specific Setting: implicit in the component! */
				Expected: ExpectedMinKubeVersion,
				Detected: clusterVersion,
			},
		}
	}
	return nil
}

func isAPIVersionAtLeast(server, refver string) (bool, error) {
	ref, err := goversion.Parse(refver)
	if err != nil {
		return false, err
	}
	ser, err := goversion.Parse(server)
	if err != nil {
		return false, err
	}
	return ser.Compare(ref) >= 0, nil
}
