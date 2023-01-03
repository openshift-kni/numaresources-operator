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
 * Copyright 2021 Red Hat, Inc.
 */

package validator

import (
	"fmt"

	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"

	"github.com/go-logr/logr"
	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
)

const (
	AreaCluster = "cluster"
	AreaKubelet = "kubelet"
)

type Validator struct {
	Log logr.Logger

	results       []ValidationResult
	serverVersion *version.Info
}

func NewValidatorWithDiscoveryClient(logger logr.Logger, cli *discovery.DiscoveryClient) (*Validator, error) {
	vd := &Validator{
		Log: logger,
	}
	_, err := vd.ValidateClusterVersion(cli)
	if err != nil {
		return nil, err
	}
	return vd, nil
}

func NewValidator(logger logr.Logger) (*Validator, error) {
	cli, err := clientutil.NewDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return NewValidatorWithDiscoveryClient(logger, cli)
}

func (vd *Validator) Results() []ValidationResult {
	return vd.results
}

type ValidationResult struct {
	Node      string `json:"node"`
	Area      string `json:"area"`
	Component string `json:"component"`
	Setting   string `json:"setting"`
	Expected  string `json:"expected"`
	Detected  string `json:"detected"`
}

func (vr ValidationResult) String() string {
	if vr.Area == AreaCluster {
		return fmt.Sprintf("Incorrect configuration of cluster: component %q setting %q: expected %q detected %q",
			vr.Component, vr.Setting, vr.Expected, vr.Detected)
	}
	return fmt.Sprintf("Incorrect configuration of node %q area %q component %q setting %q: expected %q detected %q",
		vr.Node, vr.Area, vr.Component, vr.Setting, vr.Expected, vr.Detected)
}
