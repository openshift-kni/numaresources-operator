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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	schedutils "github.com/openshift-kni/numaresources-operator/test/e2e/sched/utils"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	e2eimages "github.com/openshift-kni/numaresources-operator/test/utils/images"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2eobjects "github.com/openshift-kni/numaresources-operator/test/utils/objects"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects/wait"
)

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
			nroSchedObj.Spec.SchedulerImage = e2eimages.SchedTestImageCI

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

				return deploy.Spec.Template.Spec.Containers[0].Image == e2eimages.SchedTestImageCI
			}, time.Minute, time.Second*10).Should(BeTrue())

			By("reverting NROS changes")
			err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())

			nroSchedObj.Spec = objects.TestNROScheduler().Spec

			Eventually(func() bool {
				if err = e2eclient.Client.Update(context.TODO(), nroSchedObj); err != nil {
					klog.Warningf("failed to update NUMAResourcesScheduler %s; err: %v", nroSchedObj.Name, err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second)

			// find deployment by the ownerReference
			dp, err := schedutils.GetDeploymentByOwnerReference(nroSchedObj.GetUID())
			Expect(err).ToNot(HaveOccurred())

			Expect(wait.ForDeploymentComplete(e2eclient.Client, dp, time.Second*30, time.Minute*2)).ToNot(HaveOccurred())
		})

		It("should react to owned objects changes", func() {
			var err error
			nroSchedObj := objects.TestNROScheduler()

			err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())

			cmList := &corev1.ConfigMapList{}
			err = e2eclient.Client.List(context.TODO(), cmList)
			Expect(err).ToNot(HaveOccurred())

			var nroCM *corev1.ConfigMap
			for i := 0; i < len(cmList.Items); i++ {
				if e2eobjects.IsOwnedBy(cmList.Items[i].ObjectMeta, nroSchedObj.ObjectMeta) {
					nroCM = &cmList.Items[i]
				}
			}

			initialCM := nroCM.DeepCopy()
			nroCM.Data["somekey"] = "somevalue"

			Eventually(func() bool {
				err = e2eclient.Client.Update(context.TODO(), nroCM)
				if err != nil {
					klog.Warningf("failed to update ConfigMap %s/%s; err: %v", nroCM.Namespace, nroCM.Name, err)
					return false
				}
				return true
			}, 60*time.Second, 10*time.Second).Should(BeTrue())

			key := client.ObjectKeyFromObject(nroCM)
			Eventually(func() bool {
				err = e2eclient.Client.Get(context.TODO(), key, nroCM)
				if err != nil {
					klog.Warningf("failed to obtain ConfigMap; err: %v", err)
					return false
				}

				if diff := cmp.Diff(nroCM.Data, initialCM.Data); diff != "" {
					klog.Warningf("updated ConfigMap data is not equal to the expected: %v", diff)
					return false
				}
				return true
			}, time.Minute*2, time.Second*30).Should(BeTrue())

			dp, err := schedutils.GetDeploymentByOwnerReference(nroSchedObj.GetUID())
			Expect(err).ToNot(HaveOccurred())

			initialDP := dp.DeepCopy()

			dp.Spec.Template.Spec.Hostname = "newhostname"
			c := objects.NewTestPodPause("", "newcontainer").Spec.Containers[0]
			dp.Spec.Template.Spec.Containers = append(dp.Spec.Template.Spec.Containers, c)

			Eventually(func() bool {
				if err = e2eclient.Client.Update(context.TODO(), dp); err != nil {
					klog.Warningf("failed to update Deployment %s/%s; err: %v", dp.Namespace, dp.Name, err)
					return false
				}
				return true

			}, 30*time.Second, 5*time.Second)
			Expect(err).ToNot(HaveOccurred())

			key = client.ObjectKeyFromObject(dp)
			Eventually(func() bool {
				err = e2eclient.Client.Get(context.TODO(), key, dp)
				if err != nil {
					klog.Warningf("failed to obtain ConfigMap; err: %v", err)
					return false
				}

				if diff := cmp.Diff(dp.Spec.Template.Spec, initialDP.Spec.Template.Spec); diff != "" {
					klog.Warningf("updated Deployment is not equal to the expected: %v", diff)
					return false
				}
				return true
			}, time.Minute*2, time.Second*30).Should(BeTrue())
		})
	})
})
