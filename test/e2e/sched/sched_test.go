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
	"reflect"
	"time"

	"github.com/google/go-cmp/cmp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/api/v1/helper/namespacedname"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	e2eimages "github.com/openshift-kni/numaresources-operator/test/internal/images"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const schedulerConfigMapName = "topo-aware-scheduler-config"

var _ = Describe("[Scheduler] CR configuration management", func() {
	var initialized bool
	var initialSchedObj nropv1.NUMAResourcesScheduler
	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")
		}
		initialized = true
		initialSchedObj = nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, objectnames.DefaultNUMAResourcesSchedulerCrName)
		Expect(initialSchedObj).ToNot(Equal(nropv1.NUMAResourcesScheduler{}))

		DeferCleanup(func() {
			nroSchedKey := objects.NROSchedObjectKey()
			initialSpecCopy := *initialSchedObj.Spec.DeepCopy()
			var nroSchedObj nropv1.NUMAResourcesScheduler
			Eventually(func() bool {
				err := e2eclient.Client.Get(context.TODO(), nroSchedKey, &nroSchedObj)
				if err != nil {
					klog.ErrorS(err, "failed to get NUMAResourcesScheduler", "name", initialSchedObj.Name)
					return false
				}

				nroSchedObj.Spec = initialSpecCopy
				err = e2eclient.Client.Update(context.TODO(), &nroSchedObj)
				if err != nil {
					klog.ErrorS(err, "failed to update NUMAResourcesScheduler", "name", nroSchedObj.Name)
					return false
				}
				return true
			}).Should(BeTrue(), "failed to revert changes to %q during cleanup", nroSchedKey)

			restoredNRS := nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, initialSchedObj.Name)
			Expect(restoredNRS).ToNot(Equal(nropv1.NUMAResourcesScheduler{}))
		})
	})
	Context("with a running cluster with all the components", func() {
		It("should be able to handle plugin image change without remove/rename", func() {
			var nroSchedObj nropv1.NUMAResourcesScheduler

			var uid types.UID
			By(fmt.Sprintf("switching the NROS image to %s", e2eimages.SchedTestImageCI))
			Eventually(func() bool {
				if err := e2eclient.Client.Get(context.TODO(), objects.NROSchedObjectKey(), &nroSchedObj); err != nil {
					klog.ErrorS(err, "failed to get NUMAResourcesScheduler", "name", initialSchedObj.Name)
					return false
				}
				nroSchedObj.Spec.SchedulerImage = e2eimages.SchedTestImageCI
				if err := e2eclient.Client.Update(context.TODO(), &nroSchedObj); err != nil {
					klog.ErrorS(err, "failed to update NUMAResourcesScheduler", "name", nroSchedObj.Name)
					return false
				}
				uid = nroSchedObj.GetUID()
				return true
			}).WithTimeout(time.Minute).WithPolling(time.Second * 10).Should(BeTrue())

			nroSchedObj = nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, initialSchedObj.Name)
			Expect(nroSchedObj).ToNot(Equal(nropv1.NUMAResourcesScheduler{}))
			Expect(nroSchedObj.GetUID()).To(BeEquivalentTo(uid))

			Eventually(func() bool {
				// find deployment by the ownerReference
				deploy, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), uid)
				if err != nil {
					klog.ErrorS(err, "deployment pod listing failed")
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
					klog.ErrorS(err, "failed to list ConfigMaps")
					return false
				}

				for i := 0; i < len(cmList.Items); i++ {
					if objects.IsOwnedBy(cmList.Items[i].ObjectMeta, initialSchedObj.ObjectMeta) {
						nroCM = &cmList.Items[i]
					}
				}
				if nroCM == nil {
					klog.InfoS("cannot match ConfigMap affecting scheduler", "schedulerName", initialSchedObj.Spec.SchedulerName, "schedulerImage", initialSchedObj.Spec.SchedulerImage)
					return false
				}

				initialCM = nroCM.DeepCopy()
				nroCM.Data["somekey"] = "somevalue"

				err = e2eclient.Client.Update(context.TODO(), nroCM)
				if err != nil {
					klog.ErrorS(err, "failed to update ConfigMap", "namespace", nroCM.Namespace, "name", nroCM.Name)
					return false
				}
				return true
			}).WithTimeout(60 * time.Second).WithPolling(10 * time.Second).Should(BeTrue())

			key := client.ObjectKeyFromObject(nroCM)
			Eventually(func() bool {
				err = e2eclient.Client.Get(context.TODO(), key, nroCM)
				if err != nil {
					klog.ErrorS(err, "failed to obtain ConfigMap")
					return false
				}

				if diff := cmp.Diff(nroCM.Data, initialCM.Data); diff != "" {
					// TODO: multi-line value in structured log
					klog.InfoS("updated ConfigMap data is not equal to the expected", "diff", diff)
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
					klog.ErrorS(err, "failed to update Deployment", "namespace", dp.Namespace, "name", dp.Name)
					return false
				}
				return true
			}).WithTimeout(30 * time.Second).WithPolling(5 * time.Second).Should(BeTrue())
			Expect(err).ToNot(HaveOccurred())

			key = client.ObjectKeyFromObject(dp)
			Eventually(func() bool {
				err = e2eclient.Client.Get(context.TODO(), key, dp)
				if err != nil {
					klog.ErrorS(err, "failed to obtain ConfigMap")
					return false
				}

				if diff := cmp.Diff(dp.Spec.Template.Spec, initialDP.Spec.Template.Spec); diff != "" {
					// TODO: multi-line value in structured log
					klog.InfoS("updated Deployment is not equal to the expected", "diff", diff)
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
			Expect(podList).ToNot(BeEmpty(), "cannot find any pods for DP %s/%s", deployment.Namespace, deployment.Name)
			uid := podList[0].UID

			var t time.Duration
			if initialSchedObj.Spec.CacheResyncPeriod != nil {
				// change to something different from the current spec
				t = initialSchedObj.Spec.CacheResyncPeriod.Duration * 2
			} else {
				t = 5 * time.Minute
			}

			nroSchedKey := objects.NROSchedObjectKey()
			var nroSchedObj nropv1.NUMAResourcesScheduler
			Eventually(func() bool {
				err := e2eclient.Client.Get(context.TODO(), nroSchedKey, &nroSchedObj)
				if err != nil {
					klog.ErrorS(err, "failed to get", "key", nroSchedKey)
					return false
				}
				nroSchedObj.Spec.CacheResyncPeriod = &metav1.Duration{Duration: t}

				err = e2eclient.Client.Update(context.TODO(), &nroSchedObj)
				if err != nil {
					klog.ErrorS(err, "failed to update", "key", nroSchedKey)
					return false
				}
				return true
			}).Should(BeTrue(), "failed to update %s's CacheResyncPeriod value", nroSchedKey)

			By("checking cacheResyncPeriod under the CR's Status")
			nroSchedObj = nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, nroSchedObj.Name)
			Expect(nroSchedObj).ToNot(Equal(nropv1.NUMAResourcesScheduler{}))
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

			klog.Info("checking the old pod is removed")
			err = wait.With(e2eclient.Client).Timeout(3*time.Minute).ForPodDeleted(context.TODO(), podList[0].Namespace, podList[0].Name)
			Expect(err).ToNot(HaveOccurred())

			podList, err = podlist.With(e2eclient.Client).ByDeployment(context.TODO(), *dp)
			Expect(err).NotTo(HaveOccurred())
			Expect(podList).ToNot(BeEmpty(), "cannot find any pods for DP %s/%s", dp.Namespace, dp.Name)
			for _, pod := range podList {
				Expect(pod.UID).ToNot(Equal(uid), "new scheduler pod has not been created")
			}
		})

		It("should be able to modify scheduler loglevel", func() {
			var deployment appsv1.Deployment
			Expect(e2eclient.Client.Get(context.TODO(), namespacedname.AsObjectKey(initialSchedObj.Status.Deployment), &deployment)).To(Succeed())
			initialPodList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(initialPodList).ToNot(BeEmpty(), "cannot find pods for DP %s", initialSchedObj.Status.Deployment)

			newLogLevel := operatorv1.LogLevel("Debug")
			logLevelArg := "-v=4"
			if initialSchedObj.Spec.LogLevel == newLogLevel {
				newLogLevel = "Trace"
				logLevelArg = "-v=6"

			}

			By(fmt.Sprintf("modifying the NUMAResourcesScheduler spec.loglevel field to %q", newLogLevel))
			var nroSchedObj nropv1.NUMAResourcesScheduler
			Eventually(func(g Gomega) {
				//updates must be done on object.Spec and active values should be fetched from object.Status
				g.Expect(e2eclient.Client.Get(context.TODO(), objects.NROSchedObjectKey(), &nroSchedObj)).ToNot(HaveOccurred())

				nroSchedObj.Spec.LogLevel = newLogLevel
				g.Expect(e2eclient.Client.Update(context.TODO(), &nroSchedObj)).ToNot(HaveOccurred())
			}).WithTimeout(5 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

			By("verify scheduler is available")
			nroSchedObj = nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, nroSchedObj.Name)
			Expect(nroSchedObj).ToNot(Equal(nropv1.NUMAResourcesScheduler{}))

			By("verify scheduler log level is updated")
			Expect(e2eclient.Client.Get(context.TODO(), namespacedname.AsObjectKey(initialSchedObj.Status.Deployment), &deployment)).To(Succeed())
			Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement(logLevelArg))

			By("verify the pod was restarted")
			klog.Info("checking the old pods are removed")
			Expect(wait.With(e2eclient.Client).Timeout(3*time.Minute).ForPodListAllDeleted(context.TODO(), initialPodList)).ToNot(HaveOccurred())

			newPodList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(newPodList).ToNot(BeEmpty(), "cannot find pods for DP %s", initialSchedObj.Status.Deployment)
		})

		It("should be able to modify scheduler CacheResyncDetection", func() {
			deployment, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), initialSchedObj.UID)
			Expect(err).ToNot(HaveOccurred())
			initialPodList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), *deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(initialPodList).ToNot(BeEmpty(), "cannot find pods for DP %s", initialSchedObj.Status.Deployment)

			newValue := nropv1.CacheResyncDetectionAggressive
			expectedCMValue := "All"
			if initialSchedObj.Spec.CacheResyncDetection != nil && *initialSchedObj.Spec.CacheResyncDetection == newValue {
				newValue = nropv1.CacheResyncDetectionRelaxed
				expectedCMValue = "OnlyExclusiveResources"
			}

			By(fmt.Sprintf("modifying the NUMAResourcesScheduler spec.CacheResyncDetection field to %q", newValue))
			var nroSchedObj nropv1.NUMAResourcesScheduler
			Eventually(func(g Gomega) {
				//updates must be done on object.Spec and active values should be fetched from object.Status
				err := e2eclient.Client.Get(context.TODO(), objects.NROSchedObjectKey(), &nroSchedObj)
				g.Expect(err).ToNot(HaveOccurred())

				nroSchedObj.Spec.CacheResyncDetection = &newValue
				err = e2eclient.Client.Update(context.TODO(), &nroSchedObj)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithTimeout(5 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

			By("verify scheduler is available")
			nroSchedObj = nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, nroSchedObj.Name)
			Expect(nroSchedObj).ToNot(Equal(nropv1.NUMAResourcesScheduler{}))

			By("verify scheduler CacheResyncDetection mode is updated")
			var cm corev1.ConfigMap
			cmKey := client.ObjectKey{
				Name:      schedulerConfigMapName,
				Namespace: deployment.Namespace,
			}
			Expect(e2eclient.Client.Get(context.TODO(), cmKey, &cm)).Should(Succeed())
			data, ok := cm.Data[schedstate.SchedulerConfigFileName]
			Expect(data).ToNot(BeEmpty(), "no data found under %s/%s", cm.Namespace, cm.Name)
			Expect(ok).To(BeTrue(), "no data found under %s/%s", cm.Namespace, cm.Name)

			schedParams, err := manifests.DecodeSchedulerProfilesFromData([]byte(data))
			Expect(err).ToNot(HaveOccurred())
			schedCfg := manifests.FindSchedulerProfileByName(schedParams, nroSchedObj.Status.SchedulerName)
			Expect(schedCfg).ToNot(BeNil(), "cannot find profile config for profile %q", nroSchedObj.Status.SchedulerName)
			Expect(schedCfg.Cache).ToNot(BeNil(), "missing cache configuration")
			Expect(schedCfg.Cache.ForeignPodsDetectMode).ToNot(BeNil(), "missing cache resync configuration")
			Expect(*schedCfg.Cache.ForeignPodsDetectMode).To(Equal(expectedCMValue))

			By("verify the pod was restarted")
			klog.Info("checking the old pods are removed")
			Expect(wait.With(e2eclient.Client).Timeout(3*time.Minute).ForPodListAllDeleted(context.TODO(), initialPodList)).ToNot(HaveOccurred())

			newPodList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), *deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(newPodList).ToNot(BeEmpty(), "cannot find pods for DP %s", initialSchedObj.Status.Deployment)
		})

		It("should be able to modify scheduler ScoringStrategy", func() {
			var deployment appsv1.Deployment
			Expect(e2eclient.Client.Get(context.TODO(), namespacedname.AsObjectKey(initialSchedObj.Status.Deployment), &deployment)).To(Succeed())
			initialPodList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(initialPodList).ToNot(BeEmpty(), "cannot find pods for DP %s", initialSchedObj.Status.Deployment)

			newValue := nropv1.ScoringStrategyParams{
				Type: nropv1.BalancedAllocation,
				Resources: []nropv1.ResourceSpecParams{
					{
						Name:   "example.com/balanced-allocation",
						Weight: 10,
					},
				},
			}
			if initialSchedObj.Spec.ScoringStrategy != nil && reflect.DeepEqual(*initialSchedObj.Spec.ScoringStrategy, newValue) {
				newValue = nropv1.ScoringStrategyParams{
					Type: nropv1.MostAllocated,
					Resources: []nropv1.ResourceSpecParams{
						{
							Name:   "example.com/most-allocated",
							Weight: initialSchedObj.Spec.ScoringStrategy.Resources[0].Weight * 2,
						},
					},
				}
			}

			By(fmt.Sprintf("modifying the NUMAResourcesScheduler spec.ScoringStartegy field to %q", newValue))
			var nroSchedObj nropv1.NUMAResourcesScheduler
			Eventually(func(g Gomega) {
				//updates must be done on object.Spec and active values should be fetched from object.Status
				err := e2eclient.Client.Get(context.TODO(), objects.NROSchedObjectKey(), &nroSchedObj)
				g.Expect(err).ToNot(HaveOccurred())

				nroSchedObj.Spec.ScoringStrategy = &newValue
				err = e2eclient.Client.Update(context.TODO(), &nroSchedObj)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithTimeout(5 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

			By("verify scheduler is available")
			nroSchedObj = nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, nroSchedObj.Name)
			Expect(nroSchedObj).ToNot(Equal(nropv1.NUMAResourcesScheduler{}))

			By("verify scheduler ScoringStrategy is updated")
			var cm corev1.ConfigMap
			cmKey := client.ObjectKey{
				Name:      schedulerConfigMapName,
				Namespace: deployment.Namespace,
			}
			Expect(e2eclient.Client.Get(context.TODO(), cmKey, &cm)).Should(Succeed())
			data, ok := cm.Data[schedstate.SchedulerConfigFileName]
			Expect(data).ToNot(BeEmpty(), "no data found under %s/%s", cm.Namespace, cm.Name)
			Expect(ok).To(BeTrue(), "no data found under %s/%s", cm.Namespace, cm.Name)

			schedParams, err := manifests.DecodeSchedulerProfilesFromData([]byte(data))
			Expect(err).ToNot(HaveOccurred())
			schedCfg := manifests.FindSchedulerProfileByName(schedParams, nroSchedObj.Status.SchedulerName)
			Expect(schedCfg).ToNot(BeNil(), "cannot find profile config for profile %q", nroSchedObj.Status.SchedulerName)
			Expect(schedCfg.ScoringStrategy).ToNot(BeNil(), "missing ScoringStrategy configuration")
			// avoid converting to manifests object, compare fields instead
			Expect(schedCfg.ScoringStrategy.Type).To(Equal(string(newValue.Type)))
			Expect(schedCfg.ScoringStrategy.Resources).To(HaveLen(1))
			Expect(schedCfg.ScoringStrategy.Resources[0].Weight).To(Equal(newValue.Resources[0].Weight))

			By("verify the pod was restarted")
			klog.Info("checking the old pods are removed")
			Expect(wait.With(e2eclient.Client).Timeout(3*time.Minute).ForPodListAllDeleted(context.TODO(), initialPodList)).ToNot(HaveOccurred())

			newPodList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(newPodList).ToNot(BeEmpty(), "cannot find pods for DP %s", initialSchedObj.Status.Deployment)
		})

		It("should be able to modify scheduler CacheResyncDebug", func() {
			var deployment appsv1.Deployment
			Expect(e2eclient.Client.Get(context.TODO(), namespacedname.AsObjectKey(initialSchedObj.Status.Deployment), &deployment)).To(Succeed())
			initialPodList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(initialPodList).ToNot(BeEmpty(), "cannot find pods for DP %s", initialSchedObj.Status.Deployment)

			newValue := nropv1.CacheResyncDebugDisabled
			var expectedEnvVars []corev1.EnvVar
			if initialSchedObj.Spec.CacheResyncDebug != nil && reflect.DeepEqual(*initialSchedObj.Spec.CacheResyncDebug, newValue) {
				newValue = nropv1.CacheResyncDebugDumpJSONFile
				expectedEnvVars = []corev1.EnvVar{
					{
						Name:  "PFP_STATUS_DUMP",
						Value: "/run/pfpstatus",
					},
				}
			}

			By(fmt.Sprintf("modifying the NUMAResourcesScheduler spec.CacheResyncDebug field to %q", newValue))
			var nroSchedObj nropv1.NUMAResourcesScheduler
			Eventually(func(g Gomega) {
				//updates must be done on object.Spec and active values should be fetched from object.Status
				err := e2eclient.Client.Get(context.TODO(), objects.NROSchedObjectKey(), &nroSchedObj)
				g.Expect(err).ToNot(HaveOccurred())

				nroSchedObj.Spec.CacheResyncDebug = &newValue
				err = e2eclient.Client.Update(context.TODO(), &nroSchedObj)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

			By("verify scheduler is available")
			nroSchedObj = nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, nroSchedObj.Name)
			Expect(nroSchedObj).ToNot(Equal(nropv1.NUMAResourcesScheduler{}))

			By("verify the pod was restarted")
			klog.Info("checking the old pods are removed")
			Expect(wait.With(e2eclient.Client).Timeout(3*time.Minute).ForPodListAllDeleted(context.TODO(), initialPodList)).ToNot(HaveOccurred())

			newPodList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(newPodList).ToNot(BeEmpty(), "cannot find pods for DP %s", initialSchedObj.Status.Deployment)

			By("verify the pod's container was updated with the resync debug arg")
			Expect(e2eclient.Client.Get(context.TODO(), namespacedname.AsObjectKey(initialSchedObj.Status.Deployment), &deployment)).To(Succeed())
			Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(Equal(expectedEnvVars))
		})
	})

	It("should be able to modify scheduler replicas", func() {
		var deployment appsv1.Deployment
		Expect(e2eclient.Client.Get(context.TODO(), namespacedname.AsObjectKey(initialSchedObj.Status.Deployment), &deployment)).To(Succeed())
		initialPodList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(initialPodList).ToNot(BeEmpty(), "cannot find pods for DP %s", initialSchedObj.Status.Deployment)

		var newValue = deployment.Status.Replicas + 1

		By(fmt.Sprintf("modifying the NUMAResourcesScheduler spec.CacheResyncDebug field to %q", newValue))
		var nroSchedObj nropv1.NUMAResourcesScheduler
		Eventually(func(g Gomega) {
			//updates must be done on object.Spec and active values should be fetched from object.Status
			err := e2eclient.Client.Get(context.TODO(), objects.NROSchedObjectKey(), &nroSchedObj)
			g.Expect(err).ToNot(HaveOccurred())

			nroSchedObj.Spec.Replicas = &newValue
			err = e2eclient.Client.Update(context.TODO(), &nroSchedObj)
			g.Expect(err).ToNot(HaveOccurred())
		}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

		By("verify scheduler is available")
		nroSchedObj = nrosched.CheckNROSchedulerAvailable(context.TODO(), e2eclient.Client, nroSchedObj.Name)
		Expect(nroSchedObj).ToNot(Equal(nropv1.NUMAResourcesScheduler{}))

		By(fmt.Sprintf("verify scheduler pods are now %d", newValue))
		klog.Info("checking the old pod is removed")
		err = wait.With(e2eclient.Client).Timeout(3*time.Minute).ForPodDeleted(context.TODO(), initialPodList[0].Namespace, initialPodList[0].Name)
		Expect(err).ToNot(HaveOccurred())

		Expect(e2eclient.Client.Get(context.TODO(), namespacedname.AsObjectKey(initialSchedObj.Status.Deployment), &deployment)).To(Succeed())
		Expect(deployment.Status.Replicas).To(Equal(newValue), "cannot find correct replicas for scheduler")

		newPodList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(newPodList).To(HaveLen(int(newValue)), "cannot find correct replicas for scheduler")
	})
})
