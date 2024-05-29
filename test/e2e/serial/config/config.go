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
	"fmt"
	"os"
	"strings"

	"k8s.io/klog/v2"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	. "github.com/onsi/gomega"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	nrtv1alpha2attr "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2/helper/attribute"

	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
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

const (
	tmPolicyAttr = "topologyManagerPolicy"
	tmScopeAttr  = "topologyManagerScope"

	tmPolicyDefault = "none"      // TODO: learn somehow from k8s
	tmScopeDefault  = "container" // TODO: learn somehow from k8s
)

// This suite holds the e2e tests which span across components,
// e.g. involve both the behavior of RTE and the scheduler.
// These tests are almost always disruptive, meaning they significantly
// alter the cluster state and need a very specific cluster state (which
// is each test responsibility to setup and cleanup).
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

func getTopologyConsistencyErrors(kconfigs map[string]*kubeletconfigv1beta1.KubeletConfiguration, nrts []nrtv1alpha2.NodeResourceTopology) map[string]error {
	ret := make(map[string]error)
	for nodeName, kconfig := range kconfigs {
		nrt, err := e2enrt.FindFromList(nrts, nodeName)
		if err != nil {
			ret[nodeName] = fmt.Errorf("Unable to find NRT for node %q", nodeName)
			continue
		}

		nrtTMPolicy, ok := nrtv1alpha2attr.Get(nrt.Attributes, tmPolicyAttr)
		if !ok {
			ret[nodeName] = fmt.Errorf("Attribute %q not reported on NRT %q", tmPolicyAttr, nodeName)
			continue
		}
		kconfTMPolicy := kconfig.TopologyManagerPolicy
		if kconfTMPolicy == "" {
			klog.Infof("Topology Manager Policy not set in kubeletconfig, fixing to %q", tmPolicyDefault)
			kconfTMPolicy = tmPolicyDefault
		}
		if nrtTMPolicy.Value != kconfTMPolicy {
			ret[nodeName] = fmt.Errorf("Inconsistent topology manager policy for node %q: NRT=%q KConfig=%q", nodeName, nrtTMPolicy.Value, kconfTMPolicy)
			continue
		}

		nrtTMScope, ok := nrtv1alpha2attr.Get(nrt.Attributes, tmScopeAttr)
		if !ok {
			ret[nodeName] = fmt.Errorf("Attribute %q not reported on NRT %q", tmScopeAttr, nodeName)
			continue
		}
		kconfTMScope := kconfig.TopologyManagerScope
		if kconfTMScope == "" {
			klog.Infof("Topology Manager Scope not set in kubeletconfig, fixing to %q", tmScopeDefault)
			kconfTMScope = tmScopeDefault
		}
		if nrtTMScope.Value != kconfTMScope {
			ret[nodeName] = fmt.Errorf("Inconsistent topology manager scope for node %q: NRT=%q KConfig=%q", nodeName, nrtTMScope.Value, kconfTMScope)
			continue
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
		errText := errorMapToString(errorMap)
		klog.Infof("incoeherent NRT/KubeletConfig data: %v", errText)
		return fmt.Errorf("Following nodes have incoherent info in KubeletConfig/NRT data:\n%#v\n", errText)
	}

	singleNUMANodeNRTs := e2enrt.FilterByTopologyManagerPolicy(nrtList.Items, intnrt.SingleNUMANode)
	if len(singleNUMANodeNRTs) < minNumberOfNodesWithSameTopology {
		return fmt.Errorf("Not enough nodes with %q topology (found:%d). Need at least %d", intnrt.SingleNUMANode, len(singleNUMANodeNRTs), minNumberOfNodesWithSameTopology)
	}

	return nil
}

func errorMapToString(errs map[string]error) string {
	var sb strings.Builder
	for node, err := range errs {
		fmt.Fprintf(&sb, "%s: %v\n", node, err)
	}
	return sb.String()
}
