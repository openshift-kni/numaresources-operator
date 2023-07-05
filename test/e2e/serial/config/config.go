/*
 * Copyright 2022 Red Hat, Inc.
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

package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	. "github.com/onsi/gomega"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	"github.com/openshift-kni/numaresources-operator/pkg/validator"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
)

const (
	MultiNUMALabel                   = "numa.hardware.openshift-kni.io/cell-count"
	nropTestCIImage                  = "quay.io/openshift-kni/resource-topology-exporter:test-ci"
	SchedulerTestName                = "test-topology-scheduler"
	minNumberOfNodesWithSameTopology = 2
)

// This suite holds the e2e tests which span across components,
// e.g. involve both the behaviour of RTE and the scheduler.
// These tests are almost always disruptive, meaning they significantly
// alter the cluster state and need a very specific cluster state (which
// is each test responsability to setup and cleanup).
// Hence we call this suite serial, implying each test should run alone
// and indisturbed on the cluster. No concurrency at all is possible,
// each test "owns" the cluster - but again, must leave no leftovers.

func Setup() {
	err := SetupFixture()
	Expect(err).ToNot(HaveOccurred())
	Expect(Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")
	SetupInfra(Config.Fixture, Config.NROOperObj, Config.infraNRTList)

	err = Config.RecordNRTReference()
	Expect(err).ToNot(HaveOccurred(), "error while recording the reference NRT data")
}

func Teardown() {
	if _, ok := os.LookupEnv("E2E_NROP_INFRA_TEARDOWN_SKIP"); ok {
		return
	}
	// backward compatibility
	if _, ok := os.LookupEnv("E2E_INFRA_NO_TEARDOWN"); ok {
		return
	}
	if !Config.Ready() { // nothing to do
		return
	}
	TeardownInfra(Config.Fixture, Config.infraNRTList)
	// numacell daemonset automatically cleaned up when we remove the namespace
	err := TeardownFixture()
	Expect(err).NotTo(HaveOccurred())
}

func GetRteCiImage() string {
	if pullSpec, ok := os.LookupEnv("E2E_RTE_CI_IMAGE"); ok {
		return pullSpec
	}
	return nropTestCIImage
}

func contains(items []string, st string) bool {
	for _, item := range items {
		if item == st {
			return true
		}
	}
	return false
}

var policyScopeToTmPolicy = map[string]nrtv1alpha2.TopologyManagerPolicy{
	"restricted:pod":             nrtv1alpha2.RestrictedPodLevel,
	"restricted:container":       nrtv1alpha2.RestrictedContainerLevel,
	"best-effort:pod":            nrtv1alpha2.BestEffortPodLevel,
	"best-effort:container":      nrtv1alpha2.BestEffortContainerLevel,
	"none:":                      nrtv1alpha2.None,
	"single-numa-node:pod":       nrtv1alpha2.SingleNUMANodePodLevel,
	"single-numa-node:container": nrtv1alpha2.SingleNUMANodeContainerLevel,
}

func toTopologyPolicy(topologyManagerPolicy, topologyManagerScope string) (nrtv1alpha2.TopologyManagerPolicy, error) {
	chain := topologyManagerPolicy + ":" + topologyManagerScope
	nrtPolicy, ok := policyScopeToTmPolicy[chain]
	if !ok {
		return "", fmt.Errorf("Unable to convert %q to TmPolicy", chain)
	}
	return nrtPolicy, nil
}

func getTopologyConsistencyErrors(kconfigs map[string]*kubeletconfigv1beta1.KubeletConfiguration, nrts []nrtv1alpha2.NodeResourceTopology) map[string]error {
	ret := make(map[string]error)
	for nodeName, kconfig := range kconfigs {
		nrt, err := e2enrt.FindFromList(nrts, nodeName)
		if err != nil {
			ret[nodeName] = fmt.Errorf("Unable to find NRT for node %q", nrt.Name)
			continue
		}

		tmPolicy, err := toTopologyPolicy(kconfig.TopologyManagerPolicy, kconfig.TopologyManagerScope)
		if err != nil {
			ret[nodeName] = fmt.Errorf("Unable to convert kc.policy/kc.scope to TopologyManagerPolicy. node:%q, err: %w", nodeName, err)
			continue
		}

		if !contains(nrt.TopologyPolicies, string(tmPolicy)) {
			ret[nodeName] = fmt.Errorf("Incoherent KubeletConfig/NRT for node %q", nodeName)
		}

	}

	for _, nrt := range nrts {
		if _, ok := kconfigs[nrt.Name]; !ok {
			ret[nrt.Name] = fmt.Errorf("Unable to find KubeletConfig for node %q", nrt.Name)
		}
	}

	return ret
}

func CheckNodesTopology(ctx context.Context) error {
	kconfigs, err := validator.GetKubeletConfigurationsFromTASEnabledNodes(ctx, e2eclient.Client)
	if err != nil {
		return fmt.Errorf("error while trying to get KubeletConfigurations. error: %w", err)
	}

	var nrtList nrtv1alpha2.NodeResourceTopologyList
	err = e2eclient.Client.List(context.TODO(), &nrtList)
	if err != nil {
		return fmt.Errorf("error while trying to get NodeResourceTopology objects. error: %w", err)
	}

	errorMap := getTopologyConsistencyErrors(kconfigs, nrtList.Items)
	if len(errorMap) != 0 {
		klog.Infof("incoeherent NRT/KubeletConfig data: %v", errorMap)
		prettyMap, err := json.MarshalIndent(errorMap, "", "  ")
		if err != nil {
			return fmt.Errorf("Found some nodes with incoherent info in KubeletConfig/NRT data")
		}
		return fmt.Errorf("Following nodes have incoherent info in KubeletConfig/NRT data:\n%s\n", string(prettyMap))
	}

	snnPodLevelNRTs := e2enrt.FilterTopologyManagerPolicy(nrtList.Items, nrtv1alpha2.SingleNUMANodePodLevel)
	numSnnPodLevelNRTs := len(snnPodLevelNRTs)

	snnContainerLevelNRTs := e2enrt.FilterTopologyManagerPolicy(nrtList.Items, nrtv1alpha2.SingleNUMANodeContainerLevel)
	numSnnContainerLevelNRTs := len(snnContainerLevelNRTs)

	if numSnnPodLevelNRTs < minNumberOfNodesWithSameTopology && numSnnContainerLevelNRTs < minNumberOfNodesWithSameTopology {
		return fmt.Errorf("Not enough nodes with either %q topology (found:%d) nor %q topology (found: %d). Need at least %d", nrtv1alpha2.SingleNUMANodePodLevel, numSnnPodLevelNRTs, nrtv1alpha2.SingleNUMANodeContainerLevel, numSnnContainerLevelNRTs, minNumberOfNodesWithSameTopology)
	}

	return nil
}
