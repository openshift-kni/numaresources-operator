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

package sched

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	e2eimages "github.com/openshift-kni/numaresources-operator/test/internal/images"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
)

var _ = Describe("[Scheduler] CR configuration management", func() {
	var initialized bool
	initialSchedObj := &nropv1.NUMAResourcesScheduler{}
	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")
		}
		initialized = true
		initialSchedObj = nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, objectnames.DefaultNUMAResourcesSchedulerCrName)
		Expect(initialSchedObj).ToNot(BeNil())

		DeferCleanup(func() {
			nroSchedKey := objects.NROSchedObjectKey()
			nroSchedObj := &nropv1.NUMAResourcesScheduler{}
			Eventually(func() bool {
				err := e2eclient.Client.Get(context.TODO(), nroSchedKey, nroSchedObj)
				if err != nil {
					klog.Warningf("failed to get NUMAResourcesScheduler %s; err: %v", initialSchedObj.Name, err)
					return false
				}

				nroSchedObj.Spec = initialSchedObj.Spec
				err = e2eclient.Client.Update(context.TODO(), nroSchedObj)
				if err != nil {
					klog.Warningf("failed to update NUMAResourcesScheduler %s; err: %v", nroSchedObj.Name, err)
					return false
				}
				return true
			}).Should(BeTrue(), "failed to revert changes to %q during cleanup", nroSchedKey)

			restoredNRS := nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, initialSchedObj.Name)
			Expect(restoredNRS).ToNot(BeNil())
		})
	})
	Context("with a running cluster with all the components", func() {
		It("should be able to handle plugin image change without remove/rename", func() {
			nroSchedObj := &nropv1.NUMAResourcesScheduler{}

			var uid types.UID
			By(fmt.Sprintf("switching the NROS image to %s", e2eimages.SchedTestImageCI))
			Eventually(func() bool {
				if err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(initialSchedObj), nroSchedObj); err != nil {
					klog.Warningf("failed to get NUMAResourcesScheduler %s; err: %v", initialSchedObj.Name, err)
					return false
				}
				nroSchedObj.Spec.SchedulerImage = e2eimages.SchedTestImageCI
				if err := e2eclient.Client.Update(context.TODO(), nroSchedObj); err != nil {
					klog.Warningf("failed to update NUMAResourcesScheduler %s; err: %v", nroSchedObj.Name, err)
					return false
				}
				uid = nroSchedObj.GetUID()
				return true
			}).WithTimeout(time.Minute).WithPolling(time.Second * 10).Should(BeTrue())

			nroSchedObj = nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, initialSchedObj.Name)
			Expect(nroSchedObj).ToNot(BeNil())
			Expect(nroSchedObj.GetUID()).To(BeEquivalentTo(uid))

			Eventually(func() bool {
				// find deployment by the ownerReference
				deploy, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), uid)
				if err != nil {
					klog.Warningf("deployment pod listing failed: %v", err)
					return false
				}
				return deploy.Spec.Template.Spec.Containers[0].Image == e2eimages.SchedTestImageCI
			}).WithTimeout(time.Minute).WithPolling(time.Second * 10).Should(BeTrue())
		})

		It("should react to owned objects changes", func() {
			var err error

			var nroCM *corev1.ConfigMap
			var initialCM *corev1.ConfigMap

			Eventually(func() bool {
				cmList := &corev1.ConfigMapList{}
				if err := e2eclient.Client.List(context.TODO(), cmList); err != nil {
					klog.Warningf("failed to list ConfigMaps: %v", err)
					return false
				}

				for i := 0; i < len(cmList.Items); i++ {
					if objects.IsOwnedBy(cmList.Items[i].ObjectMeta, initialSchedObj.ObjectMeta) {
						nroCM = &cmList.Items[i]
					}
				}
				if nroCM == nil {
					klog.Warningf("cannot match ConfigMap")
					return false
				}

				initialCM = nroCM.DeepCopy()
				nroCM.Data["somekey"] = "somevalue"

				err = e2eclient.Client.Update(context.TODO(), nroCM)
				if err != nil {
					klog.Warningf("failed to update ConfigMap %s/%s; err: %v", nroCM.Namespace, nroCM.Name, err)
					return false
				}
				return true
			}).WithTimeout(60 * time.Second).WithPolling(10 * time.Second).Should(BeTrue())

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
			}).WithTimeout(time.Minute * 2).WithPolling(time.Second * 30).Should(BeTrue())

			dp, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), initialSchedObj.GetUID())
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
			}).WithTimeout(30 * time.Second).WithPolling(5 * time.Second).Should(BeTrue())
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
			}).WithTimeout(time.Minute * 2).WithPolling(time.Second * 30).Should(BeTrue())
		})

		It("should reflect changes in cacheResyncPeriod when configured", func() {
			deployment, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), initialSchedObj.UID)
			Expect(err).ToNot(HaveOccurred())
			podList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), *deployment)
			Expect(err).NotTo(HaveOccurred())
			// TODO support with multiple replicas
			Expect(podList).To(HaveLen(1), "cannot find correct amount of pods for DP %s/%s", deployment.Namespace, deployment.Name)
			uid := podList[0].UID

			var t time.Duration
			if initialSchedObj.Spec.CacheResyncPeriod != nil {
				// change to something different from the current spec
				t = initialSchedObj.Spec.CacheResyncPeriod.Duration * 2
			} else {
				t = 5 * time.Minute
			}

			nroSchedKey := objects.NROSchedObjectKey()
			nroSchedObj := &nropv1.NUMAResourcesScheduler{}
			Eventually(func() bool {
				err := e2eclient.Client.Get(context.TODO(), nroSchedKey, nroSchedObj)
				if err != nil {
					klog.Warningf("failed to get %q", nroSchedKey)
					return false
				}
				nroSchedObj.Spec.CacheResyncPeriod = &metav1.Duration{Duration: t}

				err = e2eclient.Client.Update(context.TODO(), nroSchedObj)
				if err != nil {
					klog.Warningf("failed to update %q", nroSchedKey)
					return false
				}
				return true
			}).Should(BeTrue(), "failed to update %s's CacheResyncPeriod value", nroSchedKey)

			By("checking cacheResyncPeriod under the CR's Status")
			nroSchedObj = nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, nroSchedObj.Name)
			Expect(nroSchedObj).ToNot(BeNil())
			Expect(nroSchedObj.Spec.CacheResyncPeriod.Duration).To(Equal(nroSchedObj.Status.CacheResyncPeriod.Duration), "cacheResyncPeriod not updated under the status")

			By("checking cacheResyncPeriod value reflected under the scheduler configMap")
			cmList := &corev1.ConfigMapList{}
			Expect(e2eclient.Client.List(context.TODO(), cmList)).ToNot(HaveOccurred())

			var nroschedCM *corev1.ConfigMap
			for i := 0; i < len(cmList.Items); i++ {
				if objects.IsOwnedBy(cmList.Items[i].ObjectMeta, nroSchedObj.ObjectMeta) {
					nroschedCM = &cmList.Items[i]
					break
				}
			}
			Expect(nroschedCM).ToNot(BeNil(), "failed to find ConfigMap owned by %q", nroSchedKey)
			data, ok := nroschedCM.Data[schedstate.SchedulerConfigFileName]
			Expect(data).ToNot(BeEmpty(), "no data found under %s/%s", nroschedCM.Namespace, nroschedCM.Name)
			Expect(ok).To(BeTrue(), "no data found under %s/%s", nroschedCM.Namespace, nroschedCM.Name)

			schedParams, err := manifests.DecodeSchedulerProfilesFromData([]byte(data))
			Expect(err).ToNot(HaveOccurred())

			schedCfg := manifests.FindSchedulerProfileByName(schedParams, nroSchedObj.Status.SchedulerName)
			Expect(schedCfg).ToNot(BeNil(), "cannot find profile config for profile %q", nroSchedObj.Status.SchedulerName)
			Expect(schedCfg.Cache).ToNot(BeNil(), "missing cache configuration")
			Expect(schedCfg.Cache.ResyncPeriodSeconds).ToNot(BeNil(), "missing cache resync configuration")
			Expect(*schedCfg.Cache.ResyncPeriodSeconds).To(Equal(int64(nroSchedObj.Spec.CacheResyncPeriod.Duration.Seconds())))

			By("checking new scheduler pod has been created")
			dp, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), nroSchedObj.UID)
			Expect(err).ToNot(HaveOccurred(), "unable to get deployment by owner reference")

			podList, err = podlist.With(e2eclient.Client).ByDeployment(context.TODO(), *dp)
			Expect(err).NotTo(HaveOccurred())
			Expect(podList).To(HaveLen(1), "cannot find correct amount of pods for DP %s/%s", dp.Namespace, dp.Name)
			Expect(podList[0].UID).ToNot(Equal(uid), "new scheduler pod has not been created")
		})
	})
})
