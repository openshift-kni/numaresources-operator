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

package configuration

import (
	"context"
	"fmt"
	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/openshift-kni/numaresources-operator/pkg/version"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
)

const (
	envVarMCPUpdateTimeout  = "E2E_NROP_MCP_UPDATE_TIMEOUT"
	envVarMCPUpdateInterval = "E2E_NROP_MCP_UPDATE_INTERVAL"
	envVarPlatform          = "E2E_NROP_PLATFORM"
	envVarPlatformVersion   = "E2E_NROP_PLATFORM_VERSION"
)

const (
	defaultMCPUpdateTimeout  = 60 * time.Minute
	defaultMCPUpdateInterval = 30 * time.Second
)

var (
	Plat                            platform.Platform
	PlatVersion                     platform.Version
	MachineConfigPoolUpdateTimeout  time.Duration
	MachineConfigPoolUpdateInterval time.Duration
)

func init() {
	var err error

	ctx := context.Background()

	MachineConfigPoolUpdateTimeout, err = getMachineConfigPoolUpdateValueFromEnv(envVarMCPUpdateTimeout, defaultMCPUpdateTimeout)
	if err != nil {
		panic(fmt.Errorf("failed to parse machine config pool update timeout: %w", err))
	}

	MachineConfigPoolUpdateInterval, err = getMachineConfigPoolUpdateValueFromEnv(envVarMCPUpdateInterval, defaultMCPUpdateInterval)
	if err != nil {
		panic(fmt.Errorf("failed to parse machine config pool update interval: %w", err))
	}

	Plat, PlatVersion, _ = version.DiscoverCluster(ctx, os.Getenv(envVarPlatform), os.Getenv(envVarPlatformVersion))
}

func getMachineConfigPoolUpdateValueFromEnv(envVar string, fallback time.Duration) (time.Duration, error) {
	val, ok := os.LookupEnv(envVar)
	if !ok {
		return fallback, nil
	}
	return time.ParseDuration(val)
}

// ValidateAndExtractRTEConfigData extracts and validates the RTE config from the given ConfigMap
func ValidateAndExtractRTEConfigData(cm *corev1.ConfigMap) (rteconfig.Config, error) {
	var cfg rteconfig.Config
	raw, ok := cm.Data[rteconfig.Key]
	if !ok {
		return cfg, fmt.Errorf("config.yaml not found in ConfigMap %s/%s", cm.Namespace, cm.Name)
	}

	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, fmt.Errorf("failed to unmarshal config.yaml: %w", err)
	}

	if cfg.Kubelet.TopologyManagerPolicy != "single-numa-node" {
		return cfg, fmt.Errorf("invalid topologyManagerPolicy: got %q, want \"single-numa-node\"", cfg.Kubelet.TopologyManagerPolicy)
	}

	return cfg, nil
}

func CheckTopologyManagerConfigMatching(nrt *nrtv1alpha2.NodeResourceTopology, cfg *rteconfig.Config) string {
	var matchingErr string
	for _, attr := range nrt.Attributes {
		if attr.Name == "topologyManagerPolicy" && attr.Value != cfg.Kubelet.TopologyManagerPolicy {
			matchingErr += fmt.Sprintf("%q value is different; want: %s got: %s\n", attr.Name, cfg.Kubelet.TopologyManagerPolicy, attr.Value)
		}
		if attr.Name == "topologyManagerScope" && cfg.Kubelet.TopologyManagerScope != "" && attr.Value != cfg.Kubelet.TopologyManagerScope {
			matchingErr += fmt.Sprintf("%q value is different; want: %s got: %s\n", attr.Name, cfg.Kubelet.TopologyManagerScope, attr.Value)
		}
	}
	return matchingErr
}
