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

	"github.com/google/go-cmp/cmp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	klog "k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	e2eimages "github.com/openshift-kni/numaresources-operator/test/internal/images"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[Scheduler] scheduler object updates", func() {
	var initialized bool
	nroSchedObj := &nropv1.NUMAResourcesScheduler{}
	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")
		}
		initialized = true
		nroSchedKey := objects.NROSchedObjectKey()
		Expect(e2eclient.Client.Get(context.TODO(), nroSchedKey, nroSchedObj)).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroSchedKey.String())
		initialNroSchedObj := nroSchedObj.DeepCopy()

		DeferCleanup(func() {
			Eventually(func() error {
				err := e2eclient.Client.Get(context.TODO(), nroSchedKey, nroSchedObj)
				if err != nil {
					return err
				}

				nroSchedObj.Spec = initialNroSchedObj.Spec
				return e2eclient.Client.Update(context.TODO(), nroSchedObj)
			}).Should(Succeed(), "failed to revert changes to %q during cleanup", nroSchedKey)

			dp, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), nroSchedObj.UID)
			Expect(err).ToNot(HaveOccurred(), "unable to get deployment by owner reference")

			_, err = wait.With(e2eclient.Client).Timeout(5*time.Minute).Interval(10*time.Second).ForDeploymentComplete(context.TODO(), dp)
			Expect(err).ToNot(HaveOccurred())
		})
	})
	Context("with a running cluster with all the components", func() {
		It("should be able to handle plugin image change without remove/rename", func() {
			var err error
			nroSchedObj := objects.TestNROScheduler()

			var uid types.UID
			By(fmt.Sprintf("switching the NROS image to %s", e2eimages.SchedTestImageCI))
			Eventually(func() error {
				if err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj); err != nil {
					return err
				}
				nroSchedObj.Spec.SchedulerImage = e2eimages.SchedTestImageCI
				if err := e2eclient.Client.Update(context.TODO(), nroSchedObj); err != nil {
					return err
				}
				uid = nroSchedObj.GetUID()
				return nil
			}).WithTimeout(time.Minute).WithPolling(time.Second * 10).Should(Succeed())

			err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())
			Expect(nroSchedObj.GetUID()).To(BeEquivalentTo(uid))

			Eventually(func() error {
				// find deployment by the ownerReference
				dp, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), uid)
				if err != nil {
					return fmt.Errorf("deployment pod listing failed: %w", err)
				}
				if len(dp.Spec.Template.Spec.Containers) < 1 {
					return fmt.Errorf("missing containers in deployment")
				}
				if dp.Spec.Template.Spec.Containers[0].Image != e2eimages.SchedTestImageCI {
					return fmt.Errorf("image mismatch: got %q, want %q", dp.Spec.Template.Spec.Containers[0].Image, e2eimages.SchedTestImageCI)
				}
				return nil
			}).WithTimeout(time.Minute).WithPolling(time.Second * 10).Should(Succeed())

			By("reverting NROS changes")
			Eventually(func() error {
				if err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj); err != nil {
					return err
				}
				nroSchedObj.Spec = objects.TestNROScheduler().Spec
				return e2eclient.Client.Update(context.TODO(), nroSchedObj)
			}).WithTimeout(30 * time.Second).WithPolling(5 * time.Second).Should(Succeed())

			// find deployment by the ownerReference
			dp, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), nroSchedObj.GetUID())
			Expect(err).ToNot(HaveOccurred())

			_, err = wait.With(e2eclient.Client).Interval(30*time.Second).Timeout(2*time.Minute).ForDeploymentComplete(context.TODO(), dp)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should react to owned objects changes", func() {
			var err error
			nroSchedObj := objects.TestNROScheduler()

			err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())

			var nroCM *corev1.ConfigMap
			var initialCM *corev1.ConfigMap

			Eventually(func() error {
				cmList := &corev1.ConfigMapList{}
				if err := e2eclient.Client.List(context.TODO(), cmList); err != nil {
					return fmt.Errorf("failed to list ConfigMaps: %w", err)
				}

				for i := 0; i < len(cmList.Items); i++ {
					if objects.IsOwnedBy(cmList.Items[i].ObjectMeta, nroSchedObj.ObjectMeta) {
						nroCM = &cmList.Items[i]
					}
				}
				if nroCM == nil {
					return fmt.Errorf("cannot match ConfigMap affecting scheduler %q image %q", nroSchedObj.Spec.SchedulerName, nroSchedObj.Spec.SchedulerImage)
				}

				initialCM = nroCM.DeepCopy()
				nroCM.Data["somekey"] = "somevalue"

				return e2eclient.Client.Update(context.TODO(), nroCM)
			}).WithTimeout(60 * time.Second).WithPolling(10 * time.Second).Should(Succeed())

			key := client.ObjectKeyFromObject(nroCM)
			Eventually(func() error {
				err = e2eclient.Client.Get(context.TODO(), key, nroCM)
				if err != nil {
					return fmt.Errorf("failed to get ConfigMap: %w", err)
				}

				if diff := cmp.Diff(nroCM.Data, initialCM.Data); diff != "" {
					return fmt.Errorf("updated ConfigMap data is not equal to the expected: %s", diff)
				}
				return nil
			}).WithTimeout(time.Minute * 2).WithPolling(time.Second * 30).Should(Succeed())

			var initialDP *appsv1.Deployment
			var dpKey client.ObjectKey
			Eventually(func() error {
				dp, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), nroSchedObj.GetUID())
				if err != nil {
					return fmt.Errorf("failed to get Deployment: %w", err)
				}
				initialDP = dp.DeepCopy()
				dpKey = client.ObjectKeyFromObject(dp)
				dp.Spec.Template.Spec.Hostname = "newhostname"
				c := objects.NewTestPodPause("", "newcontainer").Spec.Containers[0]
				dp.Spec.Template.Spec.Containers = append(dp.Spec.Template.Spec.Containers, c)
				return e2eclient.Client.Update(context.TODO(), dp)
			}).WithTimeout(30 * time.Second).WithPolling(5 * time.Second).Should(Succeed())

			dp := &appsv1.Deployment{}
			Eventually(func() error {
				err = e2eclient.Client.Get(context.TODO(), dpKey, dp)
				if err != nil {
					return fmt.Errorf("failed to get Deployment: %w", err)
				}

				if diff := cmp.Diff(dp.Spec.Template.Spec, initialDP.Spec.Template.Spec); diff != "" {
					return fmt.Errorf("updated Deployment is not equal to the expected: %s", diff)
				}
				return nil
			}).WithTimeout(time.Minute * 2).WithPolling(time.Second * 30).Should(Succeed())
		})
		It("should reflect changes in cacheResyncPeriod when configured", func() {
			deployment, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), nroSchedObj.UID)
			Expect(err).ToNot(HaveOccurred())
			podList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), *deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(podList).ToNot(BeEmpty(), "cannot find any pods for DP %s/%s", deployment.Namespace, deployment.Name)
			uid := podList[0].UID

			var t time.Duration
			if nroSchedObj.Spec.CacheResyncPeriod != nil {
				// change to something different from the current spec
				t = nroSchedObj.Spec.CacheResyncPeriod.Duration * 2
			} else {
				t = 5 * time.Minute
			}

			nroSchedKey := objects.NROSchedObjectKey()
			Eventually(func() error {
				err := e2eclient.Client.Get(context.TODO(), nroSchedKey, nroSchedObj)
				if err != nil {
					return err
				}
				nroSchedObj.Spec.CacheResyncPeriod = &metav1.Duration{Duration: t}
				return e2eclient.Client.Update(context.TODO(), nroSchedObj)
			}).Should(Succeed(), "failed to update %s's CacheResyncPeriod value", nroSchedKey)

			By("checking cacheResyncPeriod under the CR's Status")
			Eventually(func(g Gomega) bool {
				g.Expect(e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)).To(Succeed())
				return nroSchedObj.Spec.CacheResyncPeriod.Duration == nroSchedObj.Status.CacheResyncPeriod.Duration
			}).WithTimeout(time.Minute*2).WithPolling(time.Second*10).Should(BeTrue(), "cacheResyncPeriod not updated under the status; want: %d, got %d",
				nroSchedObj.Spec.CacheResyncPeriod.Duration, nroSchedObj.Status.CacheResyncPeriod.Duration)

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

			dp, err = wait.With(e2eclient.Client).Timeout(5*time.Minute).Interval(10*time.Second).ForDeploymentComplete(context.TODO(), dp)
			Expect(err).ToNot(HaveOccurred())

			podList, err = podlist.With(e2eclient.Client).ByDeployment(context.TODO(), *dp)
			Expect(err).NotTo(HaveOccurred())
			Expect(podList).ToNot(BeEmpty(), "cannot find any pods for DP %s/%s", dp.Namespace, dp.Name)
			for _, pod := range podList {
				Expect(pod.UID).ToNot(Equal(uid), "new scheduler pod has not been created")
			}
		})
	})

	When("testing deployment TopologySpreadConstraints", func() {
		var (
			nroSchedKey               client.ObjectKey
			autoDetectedReplicasCount *int32
			schedDp                   *appsv1.Deployment
		)

		BeforeEach(func(ctx context.Context) {
			nroSchedKey = client.ObjectKeyFromObject(nroSchedObj)
			Expect(e2eclient.Client.Get(ctx, nroSchedKey, nroSchedObj)).To(Succeed())
			currentReplicas := nroSchedObj.Spec.Replicas
			var err error
			schedDp, err = podlist.With(e2eclient.Client).DeploymentByOwnerReference(ctx, nroSchedObj.UID)
			Expect(err).ToNot(HaveOccurred())

			if currentReplicas != nil {
				e2efixture.By("configure NRS replicas for autodetection: current=%d desired=nil", *currentReplicas)
				Eventually(func(g Gomega) {
					g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, nroSchedObj)).To(Succeed())
					nroSchedObj.Spec.Replicas = nil
					g.Expect(e2eclient.Client.Update(ctx, nroSchedObj)).To(Succeed())
				}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to update NRS with autodetection mode for replicas")

				schedDp, err = wait.With(e2eclient.Client).Timeout(5*time.Minute).Interval(10*time.Second).ForDeploymentComplete(ctx, schedDp)
				if err != nil {
					infoUponDeploymentCompleteFailure(ctx, schedDp)
				}
				Expect(err).ToNot(HaveOccurred())

				DeferCleanup(func(ctx context.Context) {
					e2efixture.By("revert NRS replicas to %d", *currentReplicas)
					Eventually(func(g Gomega) {
						g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, nroSchedObj)).To(Succeed())
						nroSchedObj.Spec.Replicas = currentReplicas
						g.Expect(e2eclient.Client.Update(ctx, nroSchedObj)).To(Succeed())
					}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to update NRS with autodetection mode for replicas")
					schedDp, err := wait.With(e2eclient.Client).Timeout(5*time.Minute).Interval(10*time.Second).ForDeploymentComplete(ctx, schedDp)
					if err != nil {
						infoUponDeploymentCompleteFailure(ctx, schedDp)
					}
					Expect(err).ToNot(HaveOccurred())
				})
			}

			expectedTopologySpreadConstraints := []corev1.TopologySpreadConstraint{
				{
					MaxSkew:           1,
					TopologyKey:       "kubernetes.io/hostname",
					WhenUnsatisfiable: corev1.DoNotSchedule,
					MatchLabelKeys:    []string{"pod-template-hash"},
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: schedDp.Spec.Template.Labels,
					},
				},
			}

			By("verify spread constraints are set by default")
			Expect(schedDp.Spec.Template.Spec.TopologySpreadConstraints).To(Equal(expectedTopologySpreadConstraints), "topology spread constraints mismatch")

			autoDetectedReplicasCount = schedDp.Spec.Replicas
			Expect(autoDetectedReplicasCount).ToNot(BeNil()) // must never happen
		})

		It("should have each replica running on a different node", func(ctx context.Context) {
			pods, err := podlist.With(e2eclient.Client).ByDeployment(ctx, *schedDp)
			Expect(err).NotTo(HaveOccurred())
			Expect(pods).To(HaveLen(int(*autoDetectedReplicasCount)))

			nodeNames := sets.New[string]()
			for _, pod := range pods {
				Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
				nodeNames.Insert(pod.Spec.NodeName)
			}
			Expect(nodeNames).To(HaveLen(len(pods)), "expected each replica on a different node")
		})

		It("should allow updating replicas to 0 and properly switch back to autodetection mode", func(ctx context.Context) {
			By("update NRS to use explicit 0 replicas")
			Eventually(func(g Gomega) {
				g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, nroSchedObj)).To(Succeed())
				nroSchedObj.Spec.Replicas = ptr.To[int32](0)
				g.Expect(e2eclient.Client.Update(ctx, nroSchedObj)).To(Succeed())
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to update NRS with explicit replicas count")

			schedDp, err := wait.With(e2eclient.Client).Timeout(10*time.Minute).Interval(10*time.Second).ForDeploymentComplete(ctx, schedDp)
			if err != nil {
				infoUponDeploymentCompleteFailure(ctx, schedDp)
			}
			Expect(err).ToNot(HaveOccurred())

			By("verify the running pods are scaled down to 0")
			Eventually(func(g Gomega) {
				updatedPodListExplicit, err := podlist.With(e2eclient.Client).ByDeployment(ctx, *schedDp)
				g.Expect(err).NotTo(HaveOccurred())
				runningPods := make([]*corev1.Pod, 0, len(updatedPodListExplicit))
				for _, pod := range updatedPodListExplicit {
					if pod.Status.Phase == corev1.PodRunning && pod.DeletionTimestamp == nil {
						runningPods = append(runningPods, &pod)
					}
				}
				g.Expect(runningPods).To(BeEmpty(), "expected 0 running pods, got %d", len(runningPods))
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to have the expected number of pods running")

			By("switch back to autodetection mode and verify pods are running")
			Eventually(func(g Gomega) {
				g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, nroSchedObj)).To(Succeed())
				nroSchedObj.Spec.Replicas = nil
				g.Expect(e2eclient.Client.Update(ctx, nroSchedObj)).To(Succeed())
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to update NRS with autodetection mode for replicas")

			schedDp, err = wait.With(e2eclient.Client).Timeout(10*time.Minute).Interval(10*time.Second).ForDeploymentComplete(ctx, schedDp)
			if err != nil {
				infoUponDeploymentCompleteFailure(ctx, schedDp)
			}
			Expect(err).ToNot(HaveOccurred())

			By("verifying new pods are created and running")
			Eventually(func(g Gomega) {
				updatedPodListAutodetection, err := podlist.With(e2eclient.Client).ByDeployment(ctx, *schedDp)
				g.Expect(err).NotTo(HaveOccurred())
				for _, pod := range updatedPodListAutodetection {
					g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
				}
				g.Expect(updatedPodListAutodetection).To(HaveLen(int(*autoDetectedReplicasCount)), "expected %d new pods, got %d", *autoDetectedReplicasCount, len(updatedPodListAutodetection))
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to have the expected number of pods running")
		})

		It("should allow updating replicas and keep deployment the same when explicit replicas count set similar to autodetected count", func(ctx context.Context) {
			newCount := *autoDetectedReplicasCount
			By(fmt.Sprintf("update NRS to use explicit %d replicas", newCount))
			Eventually(func(g Gomega) {
				g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, nroSchedObj)).To(Succeed())
				nroSchedObj.Spec.Replicas = &newCount
				g.Expect(e2eclient.Client.Update(ctx, nroSchedObj)).To(Succeed())
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to update NRS with explicit replicas count")

			By("wait enough to ensure the deployment is kept the same")
			Consistently(func(g Gomega) {
				schedDpPostUpdate, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(ctx, nroSchedObj.UID)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(schedDpPostUpdate.UID).To(Equal(schedDp.UID), "deployment UID mismatch")
				g.Expect(schedDpPostUpdate.Generation).To(Equal(schedDp.Generation), "deployment generation mismatch")
			}).WithTimeout(2*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to verify the deployment is kept the same")

			By("switch back to autodetection mode")
			Eventually(func(g Gomega) {
				g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, nroSchedObj)).To(Succeed())
				nroSchedObj.Spec.Replicas = nil
				g.Expect(e2eclient.Client.Update(ctx, nroSchedObj)).To(Succeed())
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to update NRS with autodetection mode for replicas")

			By("wait enough to ensure the deployment is kept the same")
			Consistently(func(g Gomega) {
				schedDpPostUpdate, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(ctx, nroSchedObj.UID)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(schedDpPostUpdate.UID).To(Equal(schedDp.UID), "deployment UID mismatch")
				g.Expect(schedDpPostUpdate.Generation).To(Equal(schedDp.Generation), "deployment generation mismatch")
			}).WithTimeout(2*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to verify the deployment is kept the same")
		})

		It("should allow updating replicas to 1 and have pods running properly", func(ctx context.Context) {
			if *autoDetectedReplicasCount == 1 {
				Skip("skipping test for single replica deployment")
			}

			By("update NRS to use explicit 1 replicas")
			Eventually(func(g Gomega) {
				g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, nroSchedObj)).To(Succeed())
				nroSchedObj.Spec.Replicas = ptr.To[int32](1)
				g.Expect(e2eclient.Client.Update(ctx, nroSchedObj)).To(Succeed())
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to update NRS with explicit replicas count")

			schedDp, err := wait.With(e2eclient.Client).Timeout(10*time.Minute).Interval(10*time.Second).ForDeploymentComplete(ctx, schedDp)
			if err != nil {
				infoUponDeploymentCompleteFailure(ctx, schedDp)
			}
			Expect(err).ToNot(HaveOccurred())

			By("verify the running pods are scaled down to 1")
			Eventually(func(g Gomega) {
				updatedPodListExplicit, err := podlist.With(e2eclient.Client).ByDeployment(ctx, *schedDp)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(updatedPodListExplicit).To(HaveLen(1), "expected 1 pod, got %d", len(updatedPodListExplicit))
				By("verifying the pod is running")
				g.Expect(updatedPodListExplicit[0].Status.Phase).To(Equal(corev1.PodRunning))
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to have the expected number of pods running")

			By("switch back to autodetection mode")
			Eventually(func(g Gomega) {
				g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, nroSchedObj)).To(Succeed())
				nroSchedObj.Spec.Replicas = nil
				g.Expect(e2eclient.Client.Update(ctx, nroSchedObj)).To(Succeed())
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to update NRS with autodetection mode for replicas")

			schedDp, err = wait.With(e2eclient.Client).Timeout(10*time.Minute).Interval(10*time.Second).ForDeploymentComplete(ctx, schedDp)
			if err != nil {
				infoUponDeploymentCompleteFailure(ctx, schedDp)
			}
			Expect(err).ToNot(HaveOccurred())

			By("verifying new pods are created and running")
			Eventually(func(g Gomega) {
				newPods, err := podlist.With(e2eclient.Client).ByDeployment(ctx, *schedDp)
				g.Expect(err).NotTo(HaveOccurred())
				for _, pod := range newPods {
					g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
				}
				g.Expect(newPods).To(HaveLen(int(*autoDetectedReplicasCount)), "expected %d new pods, got %d", *autoDetectedReplicasCount, len(newPods))
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to have the expected number of pods running")
		})

		When("one host of a scheduler pod is down", func() {
			It("should keep the associated pod pending when deleted for balanced spread", func(ctx context.Context) {
				var targetNodeName string
				var targetPod *corev1.Pod
				By("verify pods are spread across nodes")
				// the deployment has a soft node affinity to pin the pods to control-plane nodes, thus if a pod landed
				//  on a worker node that is also valid. Hence we only check here that the hosts are different.
				pods, err := podlist.With(e2eclient.Client).ByDeployment(ctx, *schedDp)
				Expect(err).NotTo(HaveOccurred())
				Expect(pods).To(HaveLen(int(*autoDetectedReplicasCount)), "expected %d pods, got %d", *autoDetectedReplicasCount, len(pods))
				nodeNames := sets.New[string]()
				for idx, pod := range pods {
					Expect(pod.Status.Phase).To(Equal(corev1.PodRunning), "pod %s/%s is not running", pod.Namespace, pod.Name)
					nodeNames.Insert(pod.Spec.NodeName)
					klog.InfoS("running pod", "namespace", pod.Namespace, "name", pod.Name, "nodeName", pod.Spec.NodeName)
					// any sample pod is fine as target
					if idx == 0 {
						targetNodeName = pod.Spec.NodeName
						targetPod = &pod
					}
				}
				Expect(nodeNames).To(HaveLen(int(*autoDetectedReplicasCount)), "expected unique host per pod")

				// remove the target node to know what nodes to keep untouched
				nodeNames.Delete(targetNodeName)

				// for compact cluster, it's enough to mark only one of the nodes as unschedulable to get a pending pod.
				// however, for multi-node cluster, we need to mark all nodes except for the ones who have a scheduler pod -1.
				// the reason for that is because the NodeAffinity to pin the pod to control-plane is best effort, if no
				// control-plane is found fit, the scheduler will try to schedule the pod on other workernodes.
				e2efixture.By("list all cluster nodes and mark them unschedulable except for %d nodes who have a scheduler pod", len(nodeNames))
				allNodes := &corev1.NodeList{}
				Expect(e2eclient.Client.List(ctx, allNodes)).To(Succeed())
				nodesToUpdate := sets.New[string]()
				for _, node := range allNodes.Items {
					if nodeNames.Has(node.Name) {
						continue
					}

					if node.Spec.Unschedulable {
						continue
					}

					e2efixture.By("cordon node %s", node.Name)
					Eventually(func(g Gomega) {
						var nd corev1.Node
						g.Expect(e2eclient.Client.Get(ctx, client.ObjectKey{Name: node.Name}, &nd)).To(Succeed())
						nd.Spec.Unschedulable = true
						g.Expect(e2eclient.Client.Update(ctx, &nd)).To(Succeed())
					}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to cordon node %s", node.Name)

					nodesToUpdate.Insert(node.Name)
				}

				DeferCleanup(func(ctx context.Context) {
					if nodesToUpdate.Len() == 0 {
						return
					}

					By("REVERT: uncordon nodes")
					for nodeName := range nodesToUpdate {
						klog.InfoS("uncordoning node", "nodeName", nodeName)
						var node corev1.Node
						Eventually(func(g Gomega) {
							g.Expect(e2eclient.Client.Get(ctx, client.ObjectKey{Name: nodeName}, &node)).To(Succeed())
							node.Spec.Unschedulable = false
							g.Expect(e2eclient.Client.Update(ctx, &node)).To(Succeed())
						}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to uncordon node %s", nodeName)
						nodesToUpdate.Delete(nodeName)
					}
				})

				e2efixture.By("delete pod %s/%s to trigger its recreation", targetPod.Namespace, targetPod.Name)
				Expect(e2eclient.Client.Delete(ctx, targetPod)).To(Succeed())
				Expect(wait.With(e2eclient.Client).Timeout(5*time.Minute).ForPodDeleted(ctx, targetPod.Namespace, targetPod.Name)).To(Succeed())

				By("fetch the recreated pod")
				var recreatedPod *corev1.Pod
				Eventually(func(g Gomega) {
					recreatedPod = nil
					newPods, err := podlist.With(e2eclient.Client).ByDeployment(ctx, *schedDp)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(newPods).To(HaveLen(int(*autoDetectedReplicasCount)), "expected %d pods, got %d", *autoDetectedReplicasCount, len(newPods))
					for _, pod := range newPods {
						if pod.Status.Phase == corev1.PodPending {
							// this is for sure the pod that was recreated because we know for sure there are exactly
							// len(control-plane) pods that were running and each is on a different node, and only one was deleted
							// after node cordoning
							recreatedPod = &pod
							break
						}
					}
					g.Expect(recreatedPod).ToNot(BeNil(), "failed to find recreated pod")
					klog.InfoS("recreated pod", "namespace", recreatedPod.Namespace, "name", recreatedPod.Name)
				}).WithTimeout(20*time.Second).WithPolling(2*time.Second).Should(Succeed(), "failed to have the expected number of pods running")

				By("wait enough to ensure the pod keeps pending as long as there is no schedulable node")
				Consistently(func(g Gomega) {
					g.Expect(e2eclient.Client.Get(ctx, client.ObjectKey{Namespace: recreatedPod.Namespace, Name: recreatedPod.Name}, recreatedPod)).To(Succeed())
					g.Expect(recreatedPod.Status.Phase).To(Equal(corev1.PodPending), "pod %s/%s is not pending", recreatedPod.Namespace, recreatedPod.Name)
				}).WithTimeout(2*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to have the pod pending")

				Expect(recreatedPod.Status.Phase).To(Equal(corev1.PodPending), "pod %s/%s is not pending", recreatedPod.Namespace, recreatedPod.Name)
				Expect(recreatedPod.Status.Conditions).To(HaveLen(1), "pod %s/%s should have only one condition", recreatedPod.Namespace, recreatedPod.Name)
				Expect(recreatedPod.Status.Conditions[0].Type).To(Equal(corev1.PodScheduled))
				Expect(recreatedPod.Status.Conditions[0].Status).To(Equal(corev1.ConditionFalse))
				Expect(recreatedPod.Status.Conditions[0].Reason).To(Equal("Unschedulable"))
				occupiedNodes := *autoDetectedReplicasCount - 1
				Expect(recreatedPod.Status.Conditions[0].Message).To(ContainSubstring(fmt.Sprintf("%d node(s) didn't match pod topology spread constraints", occupiedNodes)))

				By("uncordon the nodes")
				for nodeName := range nodesToUpdate {
					klog.InfoS("uncordoning node", "nodeName", nodeName)
					var node corev1.Node
					Eventually(func(g Gomega) {
						g.Expect(e2eclient.Client.Get(ctx, client.ObjectKey{Name: nodeName}, &node)).To(Succeed())
						node.Spec.Unschedulable = false
						g.Expect(e2eclient.Client.Update(ctx, &node)).To(Succeed())
					}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to uncordon node %s", nodeName)
					nodesToUpdate.Delete(nodeName)
				}

				By("verify the pod goes back to running")
				Eventually(func(g Gomega) {
					g.Expect(e2eclient.Client.Get(ctx, client.ObjectKey{Namespace: recreatedPod.Namespace, Name: recreatedPod.Name}, recreatedPod)).To(Succeed())
					g.Expect(recreatedPod.Status.Phase).To(Equal(corev1.PodRunning), "pod %s/%s is not running", recreatedPod.Namespace, recreatedPod.Name)
				}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to have the pod running")
			})
		})
	})
})

func infoUponDeploymentCompleteFailure(ctx context.Context, schedDp *appsv1.Deployment) {
	klog.InfoS("deployment status", "status", schedDp.Status)
	pods, err := podlist.With(e2eclient.Client).ByDeployment(ctx, *schedDp)
	if err != nil {
		klog.ErrorS(err, "failed to list deployment pods", "namespace", schedDp.Namespace, "name", schedDp.Name)
		return
	}

	for _, pod := range pods {
		klog.InfoS("pod status", "podName", pod.Name, "podStatus", pod.Status.String())
	}
}
