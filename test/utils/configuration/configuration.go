/*
Copyright 2021 The Kubernetes Authors.

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

package configuration

import (
	"fmt"
	"os"
	"time"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform/detect"
)

const (
	envVarMCPUpdateTimeout  = "NROP_E2E_MCP_UPDATE_TIMEOUT"
	envVarMCPUpdateInterval = "NROP_E2E_MCP_UPDATE_INTERVAL"
)

const (
	defaultMCPUpdateTimeout  = 30 * time.Minute
	defaultMCPUpdateInterval = 30 * time.Second
)

var (
	Platform                        platform.Platform
	MachineConfigPoolUpdateTimeout  time.Duration
	MachineConfigPoolUpdateInterval time.Duration
)

func init() {
	var err error

	MachineConfigPoolUpdateTimeout, err = getMachineConfigPoolUpdateValueFromEnv(envVarMCPUpdateTimeout, defaultMCPUpdateTimeout)
	if err != nil {
		panic(fmt.Errorf("failed to parse machine config pool update timeout: %w", err))
	}

	MachineConfigPoolUpdateInterval, err = getMachineConfigPoolUpdateValueFromEnv(envVarMCPUpdateInterval, defaultMCPUpdateInterval)
	if err != nil {
		panic(fmt.Errorf("failed to parse machine config pool update interval: %w", err))
	}

	Platform, err = detect.Detect()
	if err != nil {
		panic(fmt.Errorf("failed to detect a platform: %w", err))
	}
}

func getMachineConfigPoolUpdateValueFromEnv(envVar string, fallback time.Duration) (time.Duration, error) {
	val, ok := os.LookupEnv(envVar)
	if !ok {
		return fallback, nil
	}
	return time.ParseDuration(val)
}
