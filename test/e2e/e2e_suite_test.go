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

package e2e

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	e2eclient "github.com/openshift-kni/numaresources-operator/test/e2e/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/e2e/utils/objects"

	_ "github.com/openshift-kni/numaresources-operator/test/e2e/basic_install"
	_ "github.com/openshift-kni/numaresources-operator/test/e2e/rte"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E numareasoures-operator")
}

var _ = BeforeSuite(func() {
	By("Creating all test resources")
	Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")

	mcpObj := objects.TestMCP()
	nroObj := objects.TestNRO()

	By(fmt.Sprintf("creating the machine config pool object: %s", mcpObj.Name))
	err := e2eclient.Client.Create(context.TODO(), mcpObj)
	Expect(err).NotTo(HaveOccurred())

	By(fmt.Sprintf("creating the NRO object: %s", nroObj.Name))
	err = e2eclient.Client.Create(context.TODO(), nroObj)
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("Deleting all test resources")

	nroObj := objects.TestNRO()
	mcpObj := objects.TestMCP()

	if err := e2eclient.Client.Delete(context.TODO(), mcpObj); err != nil && !errors.IsNotFound(err) {
		klog.Warningf("failed to delete the machine config pool %q", mcpObj.Name)
	}

	if err := e2eclient.Client.Delete(context.TODO(), nroObj); err != nil && !errors.IsNotFound(err) {
		klog.Warningf("failed to delete the numaresourcesoperators %q", nroObj.Name)
	}
})
