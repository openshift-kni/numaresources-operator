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

package detect

import (
	"encoding/json"
	"fmt"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

type PlatformInfo struct {
	AutoDetected platform.Platform `json:"autoDetected"`
	UserSupplied platform.Platform `json:"userSupplied"`
	Discovered   platform.Platform `json:"discovered"`
}

type VersionInfo struct {
	AutoDetected platform.Version `json:"autoDetected"`
	UserSupplied platform.Version `json:"userSupplied"`
	Discovered   platform.Version `json:"discovered"`
}

type ClusterInfo struct {
	Platform PlatformInfo `json:"platform"`
	Version  VersionInfo  `json:"version"`
}

func (ci ClusterInfo) String() string {
	return fmt.Sprintf("%s:%s", ci.Platform.Discovered, ci.Version.Discovered)
}

func (ci ClusterInfo) ToJSON() string {
	data, err := json.Marshal(ci)
	if err != nil {
		return `{"error":` + fmt.Sprintf("%q", err) + `}`
	}
	return string(data)
}

type ControlPlaneInfo struct {
	NodeCount int `json:"nodeCount"`
}

func (cpi ControlPlaneInfo) String() string {
	return fmt.Sprintf("nodes=%d", cpi.NodeCount)
}

func (cpi ControlPlaneInfo) ToJSON() string {
	data, err := json.Marshal(cpi)
	if err != nil {
		return `{"error":` + fmt.Sprintf("%q", err) + `}`
	}
	return string(data)
}

const (
	DetectedFromUser    string = "user-supplied"
	DetectedFromCluster string = "autodetected from cluster"
	DetectedFailure     string = "autodetection failed"
)
