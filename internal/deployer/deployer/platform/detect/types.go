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
	"github.com/openshift-kni/numaresources-operator/internal/deployer/deployer/platform"
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

type ControlPlaneInfo struct {
	NodeCount int `json:"nodeCount"`
}

const (
	DetectedFromUser    string = "user-supplied"
	DetectedFromCluster string = "autodetected from cluster"
	DetectedFailure     string = "autodetection failed"
)
