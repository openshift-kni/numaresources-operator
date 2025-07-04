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

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configv1 "github.com/openshift/api/config/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	depmanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	depobjupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	nrosched "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
	schedupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

const testSchedulerName = "testSchedulerName"

func NewFakeNUMAResourcesSchedulerReconciler(initObjects ...runtime.Object) (*NUMAResourcesSchedulerReconciler, error) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithStatusSubresource(&nropv1.NUMAResourcesScheduler{}).WithRuntimeObjects(initObjects...).Build()
	schedMf, err := schedmanifests.GetManifests(testNamespace)
	if err != nil {
		return nil, err
	}

	return &NUMAResourcesSchedulerReconciler{
		Client:             fakeClient,
		Scheme:             scheme.Scheme,
		SchedulerManifests: schedMf,
		Namespace:          testNamespace,
	}, nil
}

var _ = ginkgo.Describe("Test NUMAResourcesScheduler Reconcile", func() {
	verifyDegradedCondition := func(nrs *nropv1.NUMAResourcesScheduler, reason string) {
		reconciler, err := NewFakeNUMAResourcesSchedulerReconciler(nrs)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		key := client.ObjectKeyFromObject(nrs)
		result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(result).To(gomega.Equal(reconcile.Result{}))

		gomega.Expect(reconciler.Client.Get(context.TODO(), key, nrs)).ToNot(gomega.HaveOccurred())
		degradedCondition := getConditionByType(nrs.Status.Conditions, status.ConditionDegraded)
		gomega.Expect(degradedCondition.Status).To(gomega.Equal(metav1.ConditionTrue))
		gomega.Expect(degradedCondition.Reason).To(gomega.Equal(reason))
	}

	ginkgo.Context("with unexpected NRS CR name", func() {
		ginkgo.It("should updated the CR condition to degraded", func() {
			nrs := testobjs.NewNUMAResourcesScheduler("test", "some/url:latest", testSchedulerName, 9*time.Second)
			verifyDegradedCondition(nrs, status.ConditionTypeIncorrectNUMAResourcesSchedulerResourceName)
		})
	})

	ginkgo.Context("with correct NRS CR", func() {
		var nrs *nropv1.NUMAResourcesScheduler
		var reconciler *NUMAResourcesSchedulerReconciler
		numOfMasters := 3

		ginkgo.BeforeEach(func() {
			var err error
			nrs = testobjs.NewNUMAResourcesScheduler("numaresourcesscheduler", "some/url:latest", testSchedulerName, 11*time.Second)
			initObjects := []runtime.Object{nrs}
			initObjects = append(initObjects, fakeNodes(numOfMasters, 3)...)
			reconciler, err = NewFakeNUMAResourcesSchedulerReconciler(initObjects...)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("should create all components", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			key = client.ObjectKey{
				Name:      "secondary-scheduler",
				Namespace: testNamespace,
			}

			sa := &corev1.ServiceAccount{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, sa)).ToNot(gomega.HaveOccurred())

			key.Name = "topo-aware-scheduler-config"
			cm := &corev1.ConfigMap{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(gomega.HaveOccurred())

			key.Namespace = ""
			key.Name = "topology-aware-scheduler"
			cr := &rbacv1.ClusterRole{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, cr)).ToNot(gomega.HaveOccurred())

			crb := &rbacv1.ClusterRoleBinding{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, crb)).ToNot(gomega.HaveOccurred())

			key.Namespace = testNamespace
			key.Name = "secondary-scheduler"
			dp := &appsv1.Deployment{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("should have the correct schedulerName", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			key = client.ObjectKey{
				Namespace: testNamespace,
				Name:      "topo-aware-scheduler-config",
			}

			cm := &corev1.ConfigMap{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(gomega.HaveOccurred())

			name, found := sched.SchedulerNameFromObject(cm)
			gomega.Expect(found).To(gomega.BeTrue())
			gomega.Expect(name).To(gomega.BeEquivalentTo(testSchedulerName), "found scheduler %q expected %q", name, testSchedulerName)
		})

		ginkgo.It("should expose the resync period in status", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Expect(reconciler.Client.Get(context.TODO(), key, nrs)).ToNot(gomega.HaveOccurred())
			gomega.Expect(nrs.Status.CacheResyncPeriod).ToNot(gomega.BeNil())
			gomega.Expect(*nrs.Status.CacheResyncPeriod).To(gomega.Equal(*nrs.Spec.CacheResyncPeriod))
		})

		ginkgo.It("should expose relatedObjects in status", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Expect(reconciler.Client.Get(context.TODO(), key, nrs)).ToNot(gomega.HaveOccurred())

			expected := []configv1.ObjectReference{
				{
					Resource: "namespaces",
					Name:     reconciler.Namespace,
				},
				{
					Group:     "apps",
					Resource:  "deployments",
					Namespace: nrs.Status.Deployment.Namespace,
					Name:      nrs.Status.Deployment.Name,
				},
			}

			gomega.Expect(nrs.Status.RelatedObjects).ToNot(gomega.BeEmpty())
			gomega.Expect(nrs.Status.RelatedObjects).To(gomega.HaveLen(len(expected)))
			gomega.Expect(nrs.Status.RelatedObjects).To(gomega.ContainElements(expected))
		})

		ginkgo.It("should update the resync period in status", func() {
			resyncPeriod := 7 * time.Second
			nrs := nrs.DeepCopy()
			nrs.Spec.CacheResyncPeriod = &metav1.Duration{
				Duration: resyncPeriod,
			}

			gomega.Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Expect(reconciler.Client.Get(context.TODO(), key, nrs)).ToNot(gomega.HaveOccurred())
			gomega.Expect(nrs.Status.CacheResyncPeriod).ToNot(gomega.BeNil())
			gomega.Expect(nrs.Status.CacheResyncPeriod.Seconds()).To(gomega.Equal(resyncPeriod.Seconds()))
		})

		ginkgo.It("should have the correct priority class", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			key = client.ObjectKey{
				Name:      "secondary-scheduler",
				Namespace: testNamespace,
			}

			dp := &appsv1.Deployment{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(gomega.HaveOccurred())

			gomega.Expect(dp.Spec.Template.Spec.PriorityClassName).To(gomega.BeEquivalentTo(nrosched.SchedulerPriorityClassName))
		})

		ginkgo.It("should have a config hash annotation under deployment", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			key = client.ObjectKey{
				Namespace: testNamespace,
				Name:      "secondary-scheduler",
			}
			dp := &appsv1.Deployment{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(gomega.HaveOccurred())
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			val, ok := dp.Spec.Template.Annotations[hash.ConfigMapAnnotation]
			gomega.Expect(ok).To(gomega.BeTrue())
			gomega.Expect(val).ToNot(gomega.BeEmpty())
		})

		ginkgo.It("should react to owned objects changes", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			key = client.ObjectKey{
				Name:      "topo-aware-scheduler-config",
				Namespace: testNamespace,
			}

			cm := &corev1.ConfigMap{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(gomega.HaveOccurred())

			key.Name = "secondary-scheduler"
			dp := &appsv1.Deployment{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(gomega.HaveOccurred())

			initialCM := cm.DeepCopy()
			cm.Data["somekey"] = "somevalue"

			initialDP := dp.DeepCopy()
			dp.Spec.Template.Spec.Hostname = "newname"
			c := corev1.Container{Name: "newcontainer"}
			dp.Spec.Template.Spec.Containers = append(dp.Spec.Template.Spec.Containers, c)

			gomega.Eventually(func() bool {
				if err = reconciler.Client.Update(context.TODO(), cm); err != nil {
					klog.Warningf("failed to update MachineConfig %s; err: %v", cm.Name, err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

			gomega.Eventually(func() bool {
				if err = reconciler.Client.Update(context.TODO(), dp); err != nil {
					klog.Warningf("failed to update DaemonSet %s/%s; err: %v", dp.Namespace, dp.Name, err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

			key = client.ObjectKeyFromObject(nrs)
			_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			key = client.ObjectKeyFromObject(cm)
			err = reconciler.Client.Get(context.TODO(), key, cm)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			conf := pop(cm.Data, sched.SchedulerConfigFileName)
			initialConf := pop(initialCM.Data, sched.SchedulerConfigFileName)
			gomega.Expect(cm.Data).To(gomega.Equal(initialCM.Data))

			delta, err := diffYAML(initialConf, conf)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(delta).To(gomega.BeEmpty())

			key = client.ObjectKeyFromObject(dp)
			err = reconciler.Client.Get(context.TODO(), key, dp)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(dp.Spec.Template.Spec).To(gomega.Equal(initialDP.Spec.Template.Spec))
		})

		ginkgo.It("should allow to disable the resync period in the configmap", func() {
			resyncPeriod := 0 * time.Second
			nrs := nrs.DeepCopy()
			nrs.Spec.CacheResyncPeriod = &metav1.Duration{
				Duration: resyncPeriod,
			}

			gomega.Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			key = client.ObjectKey{
				Name:      "topo-aware-scheduler-config",
				Namespace: testNamespace,
			}

			cm := &corev1.ConfigMap{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(gomega.HaveOccurred())
			conf := pop(cm.Data, sched.SchedulerConfigFileName)

			gomega.Expect(conf).ToNot(gomega.ContainSubstring("cacheResyncPeriodSeconds"))
		})

		ginkgo.It("should expose the KNI customization environment variables in the deployment", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			key = client.ObjectKey{
				Name:      "secondary-scheduler",
				Namespace: testNamespace,
			}

			dp := &appsv1.Deployment{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(gomega.HaveOccurred())

			cnt := depobjupdate.FindContainerByName(dp.Spec.Template.Spec.Containers, schedupdate.MainContainerName)
			gomega.Expect(cnt).ToNot(gomega.BeNil(), "cannot find container %q in deployment", schedupdate.MainContainerName)

			// ordering doesn't matter
			for _, ev := range []corev1.EnvVar{
				{
					Name:  schedupdate.PFPStatusDumpEnvVar,
					Value: schedupdate.PFPStatusDir,
				},
			} {
				gotEv := schedupdate.FindEnvVarByName(cnt.Env, ev.Name)
				gomega.Expect(gotEv).ToNot(gomega.BeNil(), "missing environment variable %q in %q", ev.Name, cnt.Name)
				gomega.Expect(gotEv.Value).To(gomega.Equal(ev.Value), "unexpected value %q (wants %q) for variable %q in %q", gotEv.Value, ev.Value, ev.Name, cnt.Name)
			}
		})

		ginkgo.It("should allow to disable the KNI customization environment variables in the deployment", func() {
			debugDisabled := nropv1.CacheResyncDebugDisabled
			informerShared := nropv1.SchedulerInformerShared
			nrs := nrs.DeepCopy()
			nrs.Spec.CacheResyncDebug = &debugDisabled
			nrs.Spec.SchedulerInformer = &informerShared

			gomega.Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			key = client.ObjectKey{
				Name:      "secondary-scheduler",
				Namespace: testNamespace,
			}

			dp := &appsv1.Deployment{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(gomega.HaveOccurred())

			cnt := depobjupdate.FindContainerByName(dp.Spec.Template.Spec.Containers, schedupdate.MainContainerName)
			gomega.Expect(cnt).ToNot(gomega.BeNil(), "cannot find container %q in deployment", schedupdate.MainContainerName)

			// ordering doesn't matter
			for _, ev := range []corev1.EnvVar{
				{
					Name: schedupdate.PFPStatusDumpEnvVar,
				},
			} {
				gotEv := schedupdate.FindEnvVarByName(cnt.Env, ev.Name)
				gomega.Expect(gotEv).To(gomega.BeNil(), "unexpected environment variable %q in %q", ev.Name, cnt.Name)
			}
		})

		ginkgo.It("should configure by default the relaxed resync detection mode in configmap", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.ForeignPodsDetectOnlyExclusiveResources, depmanifests.CacheInformerDedicated)
		})

		ginkgo.It("should allow to set aggressive resync detection mode in configmap", func() {
			key := client.ObjectKeyFromObject(nrs)
			nrsUpdated := &nropv1.NUMAResourcesScheduler{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, nrsUpdated)).To(gomega.Succeed())

			resyncDetect := nropv1.CacheResyncDetectionAggressive
			nrsUpdated.Spec.CacheResyncDetection = &resyncDetect
			gomega.Expect(reconciler.Client.Update(context.TODO(), nrsUpdated)).To(gomega.Succeed())

			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.ForeignPodsDetectAll, depmanifests.CacheInformerDedicated)
		})

		ginkgo.It("should configure by default the informerMode to be Dedicated", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.CacheResyncOnlyExclusiveResources, depmanifests.CacheInformerDedicated)
		})

		ginkgo.It("should allow to change the informerMode to Shared", func() {
			nrs := nrs.DeepCopy()
			informerMode := nropv1.SchedulerInformerShared
			nrs.Spec.SchedulerInformer = &informerMode
			gomega.Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.CacheResyncOnlyExclusiveResources, depmanifests.CacheInformerShared)
		})

		ginkgo.It("should allow to change the informerMode to Dedicated", func() {
			nrs := nrs.DeepCopy()
			informerMode := nropv1.SchedulerInformerDedicated
			nrs.Spec.SchedulerInformer = &informerMode
			gomega.Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.CacheResyncOnlyExclusiveResources, depmanifests.CacheInformerDedicated)
		})

		ginkgo.It("should configure by default the ScoringStrategy to be LeastAllocated", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			var resources []depmanifests.ResourceSpecParams
			expectScoringStrategyParams(reconciler.Client, depmanifests.ScoringStrategyLeastAllocated, resources)

		})

		ginkgo.It("should allow to change the ScoringStrategy resources", func() {
			nrs := nrs.DeepCopy()
			nrs.Spec.ScoringStrategy = &nropv1.ScoringStrategyParams{}
			ResourceSpecParams := []nropv1.ResourceSpecParams{{Name: "cpu", Weight: 10}, {Name: "memory", Weight: 5}}
			nrs.Spec.ScoringStrategy.Resources = ResourceSpecParams
			gomega.Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			resources := []depmanifests.ResourceSpecParams{{Name: "cpu", Weight: 10}, {Name: "memory", Weight: 5}}
			expectScoringStrategyParams(reconciler.Client, depmanifests.ScoringStrategyLeastAllocated, resources)
		})

		ginkgo.It("should allow to change the ScoringStrategy to BalancedAllocation", func() {
			nrs := nrs.DeepCopy()
			nrs.Spec.ScoringStrategy = &nropv1.ScoringStrategyParams{}
			nrs.Spec.ScoringStrategy.Type = nropv1.BalancedAllocation
			gomega.Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			var resources []depmanifests.ResourceSpecParams
			expectScoringStrategyParams(reconciler.Client, depmanifests.ScoringStrategyBalancedAllocation, resources)
		})

		ginkgo.It("should allow to change the ScoringStrategy to MostAllocated", func() {
			nrs := nrs.DeepCopy()
			nrs.Spec.ScoringStrategy = &nropv1.ScoringStrategyParams{}
			nrs.Spec.ScoringStrategy.Type = nropv1.MostAllocated
			gomega.Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			var resources []depmanifests.ResourceSpecParams
			expectScoringStrategyParams(reconciler.Client, depmanifests.ScoringStrategyMostAllocated, resources)
		})

		ginkgo.It("should allow to change the ScoringStrategy to BalancedAllocation with resources", func() {
			nrs := nrs.DeepCopy()
			nrs.Spec.ScoringStrategy = &nropv1.ScoringStrategyParams{}
			nrs.Spec.ScoringStrategy.Type = nropv1.BalancedAllocation
			ResourceSpecParams := []nropv1.ResourceSpecParams{{Name: "cpu", Weight: 10}, {Name: "memory", Weight: 5}}
			nrs.Spec.ScoringStrategy.Resources = ResourceSpecParams
			gomega.Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			resources := []depmanifests.ResourceSpecParams{{Name: "cpu", Weight: 10}, {Name: "memory", Weight: 5}}
			expectScoringStrategyParams(reconciler.Client, depmanifests.ScoringStrategyBalancedAllocation, resources)
		})

		ginkgo.It("should allow to change the ScoringStrategy to MostAllocated with resources", func() {
			nrs := nrs.DeepCopy()
			nrs.Spec.ScoringStrategy = &nropv1.ScoringStrategyParams{}
			nrs.Spec.ScoringStrategy.Type = nropv1.MostAllocated
			ResourceSpecParams := []nropv1.ResourceSpecParams{{Name: "cpu", Weight: 10}, {Name: "memory", Weight: 5}}
			nrs.Spec.ScoringStrategy.Resources = ResourceSpecParams
			gomega.Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			resources := []depmanifests.ResourceSpecParams{{Name: "cpu", Weight: 10}, {Name: "memory", Weight: 5}}
			expectScoringStrategyParams(reconciler.Client, depmanifests.ScoringStrategyMostAllocated, resources)
		})

		ginkgo.It("should set the leader election resource parameters by default", func() {
			nrs := nrs.DeepCopy()
			nrs.Spec.Replicas = ptr.To(int32(1))
			gomega.Eventually(reconciler.Client.Update).WithArguments(context.TODO(), nrs).WithPolling(30 * time.Second).WithTimeout(5 * time.Minute).Should(gomega.Succeed())

			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(nrs)})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			expectLeaderElectParams(reconciler.Client, false, testNamespace, nrosched.LeaderElectionResourceName)
		})

		ginkgo.It("should set the leader election resource parameters to true default", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			expectLeaderElectParams(reconciler.Client, true, testNamespace, nrosched.LeaderElectionResourceName)
		})

		ginkgo.DescribeTable("should set the leader election resource parameters depending on replica count", func(replicas int32, expectedEnabled bool) {
			nrs := nrs.DeepCopy()
			nrs.Spec.Replicas = &replicas
			gomega.Eventually(reconciler.Client.Update).WithArguments(context.TODO(), nrs).WithPolling(30 * time.Second).WithTimeout(5 * time.Minute).Should(gomega.Succeed())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			expectLeaderElectParams(reconciler.Client, expectedEnabled, testNamespace, nrosched.LeaderElectionResourceName)
		},
			ginkgo.Entry("replicas=0", int32(0), false),
			ginkgo.Entry("replicas=1", int32(1), false),
			ginkgo.Entry("replicas=3", int32(3), true),
		)

		ginkgo.It("should detect replicas number by default when spec.Replicas is unset", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			dp := &appsv1.Deployment{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), client.ObjectKey{Namespace: testNamespace, Name: "secondary-scheduler"}, dp)).To(gomega.Succeed())
			gomega.Expect(*dp.Spec.Replicas).To(gomega.Equal(int32(numOfMasters)), "number of replicas is different than number of control-planes nodes; want=%d got=%d", numOfMasters, *dp.Spec.Replicas)
		})
	})

	ginkgo.Context("with kubelet PodResourcesAPI listing active pods by default", func() {
		var nrs *nropv1.NUMAResourcesScheduler
		var reconciler *NUMAResourcesSchedulerReconciler
		numOfMasters := 3

		ginkgo.When("kubelet fix is enabled", func() {
			fixedVersion, _ := platform.ParseVersion(activePodsResourcesSupportSince)

			ginkgo.DescribeTable("should configure by default the informerMode to the expected when field is not set", func(reconcilerPlatInfo PlatformInfo, expectedInformer string) {
				var err error
				nrs = testobjs.NewNUMAResourcesScheduler("numaresourcesscheduler", "some/url:latest", testSchedulerName, 11*time.Second)
				initObjects := []runtime.Object{nrs}
				initObjects = append(initObjects, fakeNodes(numOfMasters, 3)...)
				reconciler, err = NewFakeNUMAResourcesSchedulerReconciler(initObjects...)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				reconciler.PlatformInfo = reconcilerPlatInfo

				key := client.ObjectKeyFromObject(nrs)
				_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.CacheResyncOnlyExclusiveResources, expectedInformer)
			},
				ginkgo.Entry("with fixed Openshift the default informer is Shared", PlatformInfo{
					Platform: platform.OpenShift,
					Version:  fixedVersion,
				}, depmanifests.CacheInformerShared),
				ginkgo.Entry("with fixed Hypershift the default informer is Shared", PlatformInfo{
					Platform: platform.HyperShift,
					Version:  fixedVersion,
				}, depmanifests.CacheInformerShared),
				ginkgo.Entry("with unknown platform the default informer is Dedicated (unchanged)", PlatformInfo{}, depmanifests.CacheInformerDedicated))

			ginkgo.DescribeTable("should preserve informerMode value if set", func(reconcilerPlatInfo PlatformInfo) {
				var err error
				nrs = testobjs.NewNUMAResourcesScheduler("numaresourcesscheduler", "some/url:latest", testSchedulerName, 11*time.Second)
				infMode := nropv1.SchedulerInformerDedicated
				nrs.Spec.SchedulerInformer = &infMode
				initObjects := []runtime.Object{nrs}
				initObjects = append(initObjects, fakeNodes(numOfMasters, 3)...)
				reconciler, err = NewFakeNUMAResourcesSchedulerReconciler(initObjects...)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				reconciler.PlatformInfo = reconcilerPlatInfo

				key := client.ObjectKeyFromObject(nrs)
				_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.CacheResyncOnlyExclusiveResources, string(infMode))
			},
				ginkgo.Entry("with Openshift", PlatformInfo{
					Platform: platform.OpenShift,
					Version:  fixedVersion,
				}),
				ginkgo.Entry("with Hypershift", PlatformInfo{
					Platform: platform.HyperShift,
					Version:  fixedVersion,
				}),
				ginkgo.Entry("with unknown platform", PlatformInfo{}))

			ginkgo.DescribeTable("should allow to update the informerMode to be Dedicated after an overridden default", func(reconcilerPlatInfo PlatformInfo) {
				var err error
				nrs = testobjs.NewNUMAResourcesScheduler("numaresourcesscheduler", "some/url:latest", testSchedulerName, 11*time.Second)
				initObjects := []runtime.Object{nrs}
				initObjects = append(initObjects, fakeNodes(numOfMasters, 3)...)
				reconciler, err = NewFakeNUMAResourcesSchedulerReconciler(initObjects...)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				reconciler.PlatformInfo = reconcilerPlatInfo

				key := client.ObjectKeyFromObject(nrs)
				_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// intentionally skip checking default value

				// should query the object after reconcile because the defaults are overridden
				gomega.Expect(reconciler.Client.Get(context.TODO(), key, nrs)).ToNot(gomega.HaveOccurred())

				nrsUpdated := nrs.DeepCopy()
				informerMode := nropv1.SchedulerInformerDedicated
				nrsUpdated.Spec.SchedulerInformer = &informerMode
				gomega.Eventually(func() bool {
					if err := reconciler.Client.Update(context.TODO(), nrsUpdated); err != nil {
						klog.Warningf("failed to update the scheduler object; err: %v", err)
						return false
					}
					return true
				}, 30*time.Second, 5*time.Second).Should(gomega.BeTrue())

				_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.CacheResyncOnlyExclusiveResources, string(informerMode))
			},
				ginkgo.Entry("with Openshift", PlatformInfo{
					Platform: platform.OpenShift,
					Version:  fixedVersion,
				}),
				ginkgo.Entry("with Hypershift", PlatformInfo{
					Platform: platform.HyperShift,
					Version:  fixedVersion,
				}))
		})
	})
})

var _ = ginkgo.Describe("Test scheduler spec PreNormalize", func() {
	ginkgo.When("Spec.SchedulerInformer is not set by the user", func() {
		ginkgo.It("should override default informer to Shared if kubelet is fixed - first supported zstream version", func() {
			v, _ := platform.ParseVersion(activePodsResourcesSupportSince)
			spec := nropv1.NUMAResourcesSchedulerSpec{}
			platformNormalize(&spec, PlatformInfo{Platform: platform.OpenShift, Version: v})
			gomega.Expect(*spec.SchedulerInformer).To(gomega.Equal(nropv1.SchedulerInformerShared))
		})

		ginkgo.It("should override default informer to Shared if kubelet is fixed - version is greater than first supported (zstream)", func() {
			v, _ := platform.ParseVersion("4.20.1000")
			spec := nropv1.NUMAResourcesSchedulerSpec{}
			platformNormalize(&spec, PlatformInfo{Platform: platform.OpenShift, Version: v})
			gomega.Expect(*spec.SchedulerInformer).To(gomega.Equal(nropv1.SchedulerInformerShared))
		})

		ginkgo.It("should override default informer to Shared if kubelet is fixed - version is greater than first supported (ystream)", func() {
			v, _ := platform.ParseVersion("4.21.0")
			spec := nropv1.NUMAResourcesSchedulerSpec{}
			platformNormalize(&spec, PlatformInfo{Platform: platform.OpenShift, Version: v})
			gomega.Expect(*spec.SchedulerInformer).To(gomega.Equal(nropv1.SchedulerInformerShared))
		})

		ginkgo.It("should not override default informer if kubelet is not fixed - version is less than first supported (zstream)", func() {
			// this is only for testing purposes as there is plan to backport the fix to older minor versions
			// will need to remove this test if the fix is supported starting the first zstream of the release
			v, _ := platform.ParseVersion("4.20.0")
			spec := nropv1.NUMAResourcesSchedulerSpec{}
			platformNormalize(&spec, PlatformInfo{Platform: platform.OpenShift, Version: v})
			gomega.Expect(spec.SchedulerInformer).To(gomega.BeNil())
		})

		ginkgo.It("should not override default informer if kubelet is not fixed - version is less than first supported (ystream)", func() {
			v, _ := platform.ParseVersion("4.13.0")
			spec := nropv1.NUMAResourcesSchedulerSpec{}
			platformNormalize(&spec, PlatformInfo{Platform: platform.OpenShift, Version: v})
			gomega.Expect(spec.SchedulerInformer).To(gomega.BeNil())
		})
	})
	ginkgo.When("Spec.SchedulerInformer is set by the user", func() {
		ginkgo.It("should preserve informer value set by the user even if kubelet is fixed", func() {
			v, _ := platform.ParseVersion(activePodsResourcesSupportSince)
			spec := nropv1.NUMAResourcesSchedulerSpec{
				SchedulerInformer: ptr.To(nropv1.SchedulerInformerDedicated),
			}
			platformNormalize(&spec, PlatformInfo{Platform: platform.OpenShift, Version: v})
			gomega.Expect(*spec.SchedulerInformer).To(gomega.Equal(nropv1.SchedulerInformerDedicated))
		})
	})
})

func pop(m map[string]string, k string) string {
	v := m[k]
	delete(m, k)
	return v
}

func diffYAML(want, got string) (string, error) {
	cfgWant, err := depmanifests.DecodeSchedulerProfilesFromData([]byte(want))
	if err != nil {
		return "", err
	}
	cfgGot, err := depmanifests.DecodeSchedulerProfilesFromData([]byte(got))
	if err != nil {
		return "", err
	}
	return cmp.Diff(cfgWant, cfgGot), nil
}

func expectCacheParams(cli client.Client, resyncMethod, foreignPodsDetect string, informerMode string) {
	ginkgo.GinkgoHelper()

	key := client.ObjectKey{
		Name:      "topo-aware-scheduler-config",
		Namespace: testNamespace,
	}

	cm := corev1.ConfigMap{}
	gomega.Expect(cli.Get(context.TODO(), key, &cm)).To(gomega.Succeed())

	confRaw := cm.Data[sched.SchedulerConfigFileName]
	cfgs, err := depmanifests.DecodeSchedulerProfilesFromData([]byte(confRaw))
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfgs).To(gomega.HaveLen(1), "unexpected config params count: %d", len(cfgs))
	cfg := cfgs[0]

	klog.InfoS("config", dumpConfigCacheParams(cfg.Cache)...)

	gomega.Expect(cfg.Cache.ResyncMethod).ToNot(gomega.BeNil())
	gomega.Expect(*cfg.Cache.ResyncMethod).To(gomega.Equal(resyncMethod))
	gomega.Expect(cfg.Cache.ForeignPodsDetectMode).ToNot(gomega.BeNil())
	gomega.Expect(*cfg.Cache.ForeignPodsDetectMode).To(gomega.Equal(foreignPodsDetect))
	gomega.Expect(cfg.Cache.InformerMode).ToNot(gomega.BeNil())
	gomega.Expect(*cfg.Cache.InformerMode).To(gomega.Equal(informerMode))
}

func expectScoringStrategyParams(cli client.Client, scoringStrategyType string, resources []depmanifests.ResourceSpecParams) {
	ginkgo.GinkgoHelper()

	key := client.ObjectKey{
		Name:      "topo-aware-scheduler-config",
		Namespace: testNamespace,
	}

	cm := corev1.ConfigMap{}
	gomega.Expect(cli.Get(context.TODO(), key, &cm)).To(gomega.Succeed())

	confRaw := cm.Data[sched.SchedulerConfigFileName]
	cfgs, err := depmanifests.DecodeSchedulerProfilesFromData([]byte(confRaw))
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfgs).To(gomega.HaveLen(1), "unexpected config params count: %d", len(cfgs))
	cfg := cfgs[0]

	gomega.Expect(cfg.ScoringStrategy.Type).To(gomega.Equal(scoringStrategyType))
	gomega.Expect(cfg.ScoringStrategy.Resources).To(gomega.Equal(resources))
}

func expectLeaderElectParams(cli client.Client, enabled bool, resourceNamespace, resourceName string) {
	ginkgo.GinkgoHelper()

	key := client.ObjectKey{
		Name:      "topo-aware-scheduler-config",
		Namespace: testNamespace,
	}

	cm := corev1.ConfigMap{}
	gomega.Expect(cli.Get(context.TODO(), key, &cm)).To(gomega.Succeed())

	confRaw := cm.Data[sched.SchedulerConfigFileName]
	cfgs, err := depmanifests.DecodeSchedulerProfilesFromData([]byte(confRaw))
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfgs).To(gomega.HaveLen(1), "unexpected config params count: %d", len(cfgs))
	cfg := cfgs[0]

	gomega.Expect(cfg.LeaderElection.LeaderElect).To(gomega.Equal(enabled))
	gomega.Expect(cfg.LeaderElection.ResourceNamespace).To(gomega.Equal(resourceNamespace))
	gomega.Expect(cfg.LeaderElection.ResourceName).To(gomega.Equal(resourceName))
}

func fakeNodes(numOfMasters, numOfWorkers int) []runtime.Object {
	var nodes []runtime.Object
	for i := range numOfMasters {
		nodes = append(nodes, &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("master-node-%d", i+1),
				Labels: map[string]string{
					"node-role.kubernetes.io/control-plane": "",
				},
			},
		})
	}
	for i := range numOfWorkers {
		nodes = append(nodes, &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("worker-node-%d", i+1),
				Labels: map[string]string{
					"node-role.kubernetes.io/worker": "",
				},
			},
		})
	}
	return nodes
}
