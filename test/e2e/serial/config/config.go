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
	"errors"
	"fmt"
	"os"
	"strconv"

	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil/nodes"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	nrtv1alpha2attr "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2/helper/attribute"

	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	"github.com/openshift-kni/numaresources-operator/nrovalidate/validator"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"

	. "github.com/onsi/gomega"
)

const (
	MultiNUMALabel    = "numa.hardware.openshift-kni.io/cell-count"
	SchedulerTestName = "test-topology-scheduler"
	DefaultVerbosity  = 4
)

const (
	nropTestCIImage                  = "quay.io/openshift-kni/resource-topology-exporter:test-ci"
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
	config := textlogger.NewConfig(textlogger.Verbosity(GetVerbosity()))
	ctrl.SetLogger(textlogger.NewLogger(config))

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

func GetVerbosity() int {
	rawLevel, ok := os.LookupEnv("E2E_NROP_VERBOSE")
	if !ok {
		return DefaultVerbosity
	}
	level, err := strconv.Atoi(rawLevel)
	if err != nil {
		// TODO: log how?
		return DefaultVerbosity
	}
	return level
}

func GetRteCiImage() string {
	if pullSpec, ok := os.LookupEnv("E2E_RTE_CI_IMAGE"); ok {
		return pullSpec
	}
	return nropTestCIImage
}

func validateTopologyManagerConfiguration(kconfigs map[string]*kubeletconfigv1beta1.KubeletConfiguration, nrts []nrtv1alpha2.NodeResourceTopology) error {
	var errs []error
	for nodeName, kconfig := range kconfigs {
		nrt, err := e2enrt.FindFromList(nrts, nodeName)
		if err != nil {
			errs = append(errs, fmt.Errorf("Unable to find NRT for node %q", nodeName))
			continue
		}

		nrtTMPolicy, ok := nrtv1alpha2attr.Get(nrt.Attributes, tmPolicyAttr)
		if !ok {
			errs = append(errs, fmt.Errorf("Attribute %q not reported on NRT %q", tmPolicyAttr, nodeName))
			continue
		}
		kconfTMPolicy := kconfig.TopologyManagerPolicy
		if kconfTMPolicy == "" {
			klog.InfoS("Topology Manager Policy not set in kubeletconfig, fixing", "policy", tmPolicyDefault)
			kconfTMPolicy = tmPolicyDefault
		}
		if nrtTMPolicy.Value != kconfTMPolicy {
			errs = append(errs, fmt.Errorf("Inconsistent topology manager policy for node %q: NRT=%q KConfig=%q", nodeName, nrtTMPolicy.Value, kconfTMPolicy))
			continue
		}

		nrtTMScope, ok := nrtv1alpha2attr.Get(nrt.Attributes, tmScopeAttr)
		if !ok {
			errs = append(errs, fmt.Errorf("Attribute %q not reported on NRT %q", tmScopeAttr, nodeName))
			continue
		}
		kconfTMScope := kconfig.TopologyManagerScope
		if kconfTMScope == "" {
			klog.InfoS("Topology Manager Scope not set in kubeletconfig, fixing", "scope", tmScopeDefault)
			kconfTMScope = tmScopeDefault
		}
		if nrtTMScope.Value != kconfTMScope {
			errs = append(errs, fmt.Errorf("Inconsistent topology manager scope for node %q: NRT=%q KConfig=%q", nodeName, nrtTMScope.Value, kconfTMScope))
			continue
		}
	}

	for _, nrt := range nrts {
		if _, ok := kconfigs[nrt.Name]; !ok {
			errs = append(errs, fmt.Errorf("Unable to find KubeletConfig for node %q", nrt.Name))
		}
	}

	return errors.Join(errs...)
}

func CheckNodesTopology(ctx context.Context) error {
	kconfigs, err := validator.GetKubeletConfigurationsFromTASEnabledNodes(ctx, e2eclient.Client)
	if err != nil {
		return err
	}

	nrtList, err := getNRTList(ctx, e2eclient.Client)
	if err != nil {
		return err
	}

	err = validateTopologyManagerConfiguration(kconfigs, nrtList.Items)
	if err != nil {
		return err
	}

	err = validateNRTAvailability(ctx, nrtList)
	if err != nil {
		return err
	}

	err = validateSMTAlignmentDisabled(kconfigs)
	if err != nil {
		return err
	}

	return nil
}

func getNRTList(ctx context.Context, cli client.Client) (nrtv1alpha2.NodeResourceTopologyList, error) {
	var nrtList nrtv1alpha2.NodeResourceTopologyList
	err := cli.List(ctx, &nrtList)
	if err != nil {
		return nrtList, fmt.Errorf("error while trying to get NodeResourceTopology objects. error: %w", err)
	}

	singleNUMANodeNRTs := e2enrt.FilterByTopologyManagerPolicy(nrtList.Items, intnrt.SingleNUMANode)
	if len(singleNUMANodeNRTs) < minNumberOfNodesWithSameTopology {
		return nrtList, fmt.Errorf("Not enough nodes with %q topology found %d needed %d", intnrt.SingleNUMANode, len(singleNUMANodeNRTs), minNumberOfNodesWithSameTopology)
	}

	return nrtList, nil
}

func validateNRTAvailability(ctx context.Context, nrtList nrtv1alpha2.NodeResourceTopologyList) error {
	workers, err := nodes.GetWorkers(&deployer.Environment{Cli: e2eclient.Client, Ctx: ctx})
	if err != nil {
		return err
	}
	for _, node := range workers {
		if _, err := e2enrt.FindFromList(nrtList.Items, node.Name); err != nil {
			klog.InfoS("NRT data missing", "workerNode", node.Name)
			return err
		}
	}
	return nil
}

func validateSMTAlignmentDisabled(kconfigs map[string]*kubeletconfigv1beta1.KubeletConfiguration) error {
	var errs []error
	for nodeName, kconfig := range kconfigs {
		v, ok := kconfig.CPUManagerPolicyOptions["full-pcpus-only"]
		if !ok {
			// SMT alignment disabled by default
			continue
		}
		// don't check for error, the value has to be valid, kubelet won't start otherwise
		enable, _ := strconv.ParseBool(v)
		if enable {
			errs = append(errs, fmt.Errorf("SMT alignment enabled for node %q", nodeName))
		}
	}
	return errors.Join(errs...)
}
