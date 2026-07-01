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

package config

import (
	"os"

	"k8s.io/klog/v2"

	metricssrv "github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/metrics/server"

	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podres/middleware/sharedcpuspool"
)

func HostNameFromEnv() string {
	if val, ok := os.LookupEnv("NODE_NAME"); ok {
		return val
	}
	return ""
}

func TopologyManagerPolicyFromEnv() string {
	if val, ok := os.LookupEnv("TOPOLOGY_MANAGER_POLICY"); ok {
		return val
	}
	// empty string is a valid value here, so just keep going
	return ""
}

func TopologyManagerScopeFromEnv() string {
	if val, ok := os.LookupEnv("TOPOLOGY_MANAGER_SCOPE"); ok {
		return val
	}
	// empty string is a valid value here, so just keep going
	return ""
}

func PodSetFingerprintStatusFileFromEnv() string {
	if val, ok := os.LookupEnv("PFP_STATUS_FILE"); ok {
		return val
	}
	return ""
}

func FromEnv(pArgs *ProgArgs) {
	before := pArgs.Clone()

	if pArgs.NRTupdater.Hostname == "" {
		pArgs.NRTupdater.Hostname = HostNameFromEnv()
		if before.NRTupdater.Hostname != pArgs.NRTupdater.Hostname {
			klog.V(4).Infof("environment override: hostname %q -> %q", before.NRTupdater.Hostname, pArgs.NRTupdater.Hostname)
		}
	}
	if pArgs.RTE.TopologyManagerPolicy == "" {
		pArgs.RTE.TopologyManagerPolicy = TopologyManagerPolicyFromEnv()
		if before.RTE.TopologyManagerPolicy != pArgs.RTE.TopologyManagerPolicy {
			klog.V(4).Infof("environment override: topologyManagerPolicy %q -> %q", before.RTE.TopologyManagerPolicy, pArgs.RTE.TopologyManagerPolicy)
		}
	}
	if pArgs.RTE.TopologyManagerScope == "" {
		pArgs.RTE.TopologyManagerScope = TopologyManagerScopeFromEnv()
		if before.RTE.TopologyManagerScope != pArgs.RTE.TopologyManagerScope {
			klog.V(4).Infof("environment override: topologyManagerScope %q -> %q", before.RTE.TopologyManagerScope, pArgs.RTE.TopologyManagerScope)
		}
	}
	if pArgs.RTE.ReferenceContainer.IsEmpty() {
		pArgs.RTE.ReferenceContainer = sharedcpuspool.ContainerIdentFromEnv()
		if before.RTE.ReferenceContainer.String() != pArgs.RTE.ReferenceContainer.String() {
			klog.V(4).Infof("environment override: referenceContainer %q -> %q", before.RTE.ReferenceContainer.String(), pArgs.RTE.ReferenceContainer.String())
		}
	}
	if pArgs.RTE.MetricsPort == 0 {
		pArgs.RTE.MetricsPort = metricssrv.PortFromEnv()
		if before.RTE.MetricsPort != pArgs.RTE.MetricsPort {
			klog.V(4).Infof("environment override: metricsPort %d -> %d", before.RTE.MetricsPort, pArgs.RTE.MetricsPort)
		}
	}
	if pArgs.RTE.MetricsAddress == "" {
		pArgs.RTE.MetricsAddress = metricssrv.AddressFromEnv()
		if before.RTE.MetricsAddress != pArgs.RTE.MetricsAddress {
			klog.V(4).Infof("environment override: metricsAddress %q -> %q", before.RTE.MetricsAddress, pArgs.RTE.MetricsAddress)
		}
	}
	if pArgs.Resourcemonitor.PodSetFingerprintStatusFile == "" {
		pArgs.Resourcemonitor.PodSetFingerprintStatusFile = PodSetFingerprintStatusFileFromEnv()
		if before.Resourcemonitor.PodSetFingerprintStatusFile != pArgs.Resourcemonitor.PodSetFingerprintStatusFile {
			klog.V(4).Infof("environment override: podSetFingerprintStatusFile %q -> %q", before.Resourcemonitor.PodSetFingerprintStatusFile, pArgs.Resourcemonitor.PodSetFingerprintStatusFile)
		}
	}
}
