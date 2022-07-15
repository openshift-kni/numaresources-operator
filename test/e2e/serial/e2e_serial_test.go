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

package serial

import (
	"math/rand"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"

	"k8s.io/klog/v2"

	ginkgo_reporters "kubevirt.io/qe-tools/pkg/ginkgo-reporters"

	nropmcp "github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	_ "github.com/openshift-kni/numaresources-operator/test/e2e/serial/tests"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/hugepages"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
)

func TestSerial(t *testing.T) {
	RegisterFailHandler(Fail)

	rr := []Reporter{}
	if ginkgo_reporters.Polarion.Run {
		rr = append(rr, &ginkgo_reporters.Polarion)
	}
	rr = append(rr, reporters.NewJUnitReporter("numaresources"))
	//RunSpecsWithDefaultAndCustomReporters(t, "NUMAResources serial e2e tests", rr)
}

var _ = BeforeSuite(func() {
	// this must be the very first thing
	rand.Seed(time.Now().UnixNano())
	serialconfig.Setup()
	err := setupHugepages()
	if err != nil {
		klog.Errorf("failed to configure hugpages: %v", err)
	}
})

//TODO do we need to teardown hp settings? can that be by deleting the mc?
var _ = AfterSuite(serialconfig.Teardown)

func setupHugepages() error {
	profile := hugepages.GetProfileWithHugepages()
	mc, err := hugepages.GetHugepagesMachineConfig(profile)
	if err != nil {
		klog.Errorf("could not generate machineconfig with hugpages settings: %v", err)
		return err
	}

	nroSchedObj := &nropv1alpha1.NUMAResourcesScheduler{}
	err := fxt.Client.Get(context.TODO(), client.ObjectKey{Name: nrosched.NROSchedObjectName}, nroSchedObj)
	Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nrosched.NROSchedObjectName)

	mcps, err := nropmcp.GetListByNodeGroups(context.TODO(), e2eclient.Client, nroOperObj.Spec.NodeGroups)
	Expect(err).ToNot(HaveOccurred())

	err = hugepages.ApplyMachineConfig(mc, mcp)
	if err != nil {
		klog.Errorf("could not apply the machineconfig with hugpages settings: %v", err)
		return err
	}
}
