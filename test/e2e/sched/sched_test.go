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

package sched

import (
	"context"
	"time"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	schedutils "github.com/openshift-kni/numaresources-operator/test/e2e/sched/utils"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

const newTestImage = "quay.io/openshift-kni/scheduler-plugins:test-ci"

var _ = Describe("[Scheduler] imageReplacement", func() {
	var initialized bool
	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")
		}
		initialized = true
	})
	Context("with a running cluster with all the components", func() {
		It("should be able to handle plugin image change without remove/rename", func() {
			var err error
			nroSchedObj := objects.TestNROScheduler()

			err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())

			uid := nroSchedObj.GetUID()
			nroSchedObj.Spec.SchedulerImage = newTestImage

			err = e2eclient.Client.Update(context.TODO(), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())

			err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())
			Expect(nroSchedObj.GetUID()).To(BeEquivalentTo(uid))

			Eventually(func() bool {
				// find deployment by the ownerReference
				deploy, err := schedutils.GetDeploymentByOwnerReference(uid)
				if err != nil {
					klog.Warningf("%w", err)
					return false
				}

				return deploy.Spec.Template.Spec.Containers[0].Image == newTestImage
			}, time.Minute, time.Second*10).Should(BeTrue())
		})
	})
})
