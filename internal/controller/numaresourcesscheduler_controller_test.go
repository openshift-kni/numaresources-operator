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
	"reflect"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	nrosched "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
	schedupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/status"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("Test NUMAResourcesScheduler Reconcile", func() {
	verifyDegradedCondition := func(nrs *nropv1.NUMAResourcesScheduler, reason string) {
		reconciler, err := NewFakeNUMAResourcesSchedulerReconciler(nrs)
		Expect(err).ToNot(HaveOccurred())

		key := client.ObjectKeyFromObject(nrs)
		result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		Expect(reconciler.Client.Get(context.TODO(), key, nrs)).ToNot(HaveOccurred())
		degradedCondition := getConditionByType(nrs.Status.Conditions, status.ConditionDegraded)
		Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(degradedCondition.Reason).To(Equal(reason))
	}

	Context("with unexpected NRS CR name", func() {
		It("should updated the CR condition to degraded", func() {
			nrs := testobjs.NewNUMAResourcesScheduler("test", "some/url:latest", testSchedulerName, 9*time.Second)
			verifyDegradedCondition(nrs, status.ConditionTypeIncorrectNUMAResourcesSchedulerResourceName)
		})
	})

	Context("with correct NRS CR", func() {
		var nrs *nropv1.NUMAResourcesScheduler
		var reconciler *NUMAResourcesSchedulerReconciler
		numOfMasters := 3

		BeforeEach(func() {
			var err error
			nrs = testobjs.NewNUMAResourcesScheduler("numaresourcesscheduler", "some/url:latest", testSchedulerName, 11*time.Second)
			initObjects := []runtime.Object{nrs}
			initObjects = append(initObjects, fakeNodes(numOfMasters, 3)...)
			reconciler, err = NewFakeNUMAResourcesSchedulerReconciler(initObjects...)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should create all components", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			key = client.ObjectKey{
				Name:      "secondary-scheduler",
				Namespace: testNamespace,
			}

			sa := &corev1.ServiceAccount{}
			Expect(reconciler.Client.Get(context.TODO(), key, sa)).ToNot(HaveOccurred())

			key.Name = "topo-aware-scheduler-config"
			cm := &corev1.ConfigMap{}
			Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(HaveOccurred())

			key.Namespace = ""
			key.Name = "topology-aware-scheduler"
			cr := &rbacv1.ClusterRole{}
			Expect(reconciler.Client.Get(context.TODO(), key, cr)).ToNot(HaveOccurred())

			crb := &rbacv1.ClusterRoleBinding{}
			Expect(reconciler.Client.Get(context.TODO(), key, crb)).ToNot(HaveOccurred())

			key.Namespace = testNamespace
			key.Name = "secondary-scheduler"
			dp := &appsv1.Deployment{}
			Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(HaveOccurred())
		})

		It("should have the correct schedulerName", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			key = client.ObjectKey{
				Namespace: testNamespace,
				Name:      "topo-aware-scheduler-config",
			}

			cm := &corev1.ConfigMap{}
			Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(HaveOccurred())

			name, found := sched.SchedulerNameFromObject(cm)
			Expect(found).To(BeTrue())
			Expect(name).To(BeEquivalentTo(testSchedulerName), "found scheduler %q expected %q", name, testSchedulerName)
		})

		It("should expose the resync period in status", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			Expect(reconciler.Client.Get(context.TODO(), key, nrs)).ToNot(HaveOccurred())
			Expect(nrs.Status.CacheResyncPeriod).ToNot(BeNil())
			Expect(*nrs.Status.CacheResyncPeriod).To(Equal(*nrs.Spec.CacheResyncPeriod))
		})

		It("should expose relatedObjects in status", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			Expect(reconciler.Client.Get(context.TODO(), key, nrs)).ToNot(HaveOccurred())

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

			Expect(nrs.Status.RelatedObjects).ToNot(BeEmpty())
			Expect(nrs.Status.RelatedObjects).To(HaveLen(len(expected)))
			Expect(nrs.Status.RelatedObjects).To(ContainElements(expected))
		})

		It("should update the resync period in status", func() {
			resyncPeriod := 7 * time.Second
			nrs := nrs.DeepCopy()
			nrs.Spec.CacheResyncPeriod = &metav1.Duration{
				Duration: resyncPeriod,
			}

			Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			Expect(reconciler.Client.Get(context.TODO(), key, nrs)).ToNot(HaveOccurred())
			Expect(nrs.Status.CacheResyncPeriod).ToNot(BeNil())
			Expect(nrs.Status.CacheResyncPeriod.Seconds()).To(Equal(resyncPeriod.Seconds()))
		})

		It("should have the correct priority class", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			key = client.ObjectKey{
				Name:      "secondary-scheduler",
				Namespace: testNamespace,
			}

			dp := &appsv1.Deployment{}
			Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(HaveOccurred())

			Expect(dp.Spec.Template.Spec.PriorityClassName).To(BeEquivalentTo(nrosched.SchedulerPriorityClassName))
		})

		It("should have a config hash annotation under deployment", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			key = client.ObjectKey{
				Namespace: testNamespace,
				Name:      "secondary-scheduler",
			}
			dp := &appsv1.Deployment{}
			Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(HaveOccurred())
			Expect(err).ToNot(HaveOccurred())

			val, ok := dp.Spec.Template.Annotations[hash.ConfigMapAnnotation]
			Expect(ok).To(BeTrue())
			Expect(val).ToNot(BeEmpty())
		})

		It("should react to owned objects changes", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			key = client.ObjectKey{
				Name:      "topo-aware-scheduler-config",
				Namespace: testNamespace,
			}

			cm := &corev1.ConfigMap{}
			Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(HaveOccurred())

			key.Name = "secondary-scheduler"
			dp := &appsv1.Deployment{}
			Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(HaveOccurred())

			initialCM := cm.DeepCopy()
			cm.Data["somekey"] = "somevalue"

			initialDP := dp.DeepCopy()
			dp.Spec.Template.Spec.Hostname = "newname"
			c := corev1.Container{Name: "newcontainer"}
			dp.Spec.Template.Spec.Containers = append(dp.Spec.Template.Spec.Containers, c)

			Eventually(func() bool {
				if err = reconciler.Client.Update(context.TODO(), cm); err != nil {
					klog.Warningf("failed to update MachineConfig %s; err: %v", cm.Name, err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			Eventually(func() bool {
				if err = reconciler.Client.Update(context.TODO(), dp); err != nil {
					klog.Warningf("failed to update DaemonSet %s/%s; err: %v", dp.Namespace, dp.Name, err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			key = client.ObjectKeyFromObject(nrs)
			_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			key = client.ObjectKeyFromObject(cm)
			err = reconciler.Client.Get(context.TODO(), key, cm)
			Expect(err).ToNot(HaveOccurred())

			conf := pop(cm.Data, sched.SchedulerConfigFileName)
			initialConf := pop(initialCM.Data, sched.SchedulerConfigFileName)
			Expect(cm.Data).To(Equal(initialCM.Data))

			delta, err := diffYAML(initialConf, conf)
			Expect(err).ToNot(HaveOccurred())
			Expect(delta).To(BeEmpty())

			key = client.ObjectKeyFromObject(dp)
			err = reconciler.Client.Get(context.TODO(), key, dp)
			Expect(err).ToNot(HaveOccurred())
			Expect(dp.Spec.Template.Spec).To(Equal(initialDP.Spec.Template.Spec))
		})

		It("should allow to disable the resync period in the configmap", func() {
			resyncPeriod := 0 * time.Second
			nrs := nrs.DeepCopy()
			nrs.Spec.CacheResyncPeriod = &metav1.Duration{
				Duration: resyncPeriod,
			}

			Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			key = client.ObjectKey{
				Name:      "topo-aware-scheduler-config",
				Namespace: testNamespace,
			}

			cm := &corev1.ConfigMap{}
			Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(HaveOccurred())
			conf := pop(cm.Data, sched.SchedulerConfigFileName)

			Expect(conf).ToNot(ContainSubstring("cacheResyncPeriodSeconds"))
		})

		It("should expose the KNI customization environment variables in the deployment", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			key = client.ObjectKey{
				Name:      "secondary-scheduler",
				Namespace: testNamespace,
			}

			dp := &appsv1.Deployment{}
			Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(HaveOccurred())

			cnt := depobjupdate.FindContainerByName(dp.Spec.Template.Spec.Containers, schedupdate.MainContainerName)
			Expect(cnt).ToNot(BeNil(), "cannot find container %q in deployment", schedupdate.MainContainerName)

			// ordering doesn't matter
			for _, ev := range []corev1.EnvVar{
				{
					Name:  schedupdate.PFPStatusDumpEnvVar,
					Value: schedupdate.PFPStatusDir,
				},
			} {
				gotEv := schedupdate.FindEnvVarByName(cnt.Env, ev.Name)
				Expect(gotEv).ToNot(BeNil(), "missing environment variable %q in %q", ev.Name, cnt.Name)
				Expect(gotEv.Value).To(Equal(ev.Value), "unexpected value %q (wants %q) for variable %q in %q", gotEv.Value, ev.Value, ev.Name, cnt.Name)
			}
		})

		It("should allow to disable the KNI customization environment variables in the deployment", func() {
			debugDisabled := nropv1.CacheResyncDebugDisabled
			informerShared := nropv1.SchedulerInformerShared
			nrs := nrs.DeepCopy()
			nrs.Spec.CacheResyncDebug = &debugDisabled
			nrs.Spec.SchedulerInformer = &informerShared

			Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			key = client.ObjectKey{
				Name:      "secondary-scheduler",
				Namespace: testNamespace,
			}

			dp := &appsv1.Deployment{}
			Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(HaveOccurred())

			cnt := depobjupdate.FindContainerByName(dp.Spec.Template.Spec.Containers, schedupdate.MainContainerName)
			Expect(cnt).ToNot(BeNil(), "cannot find container %q in deployment", schedupdate.MainContainerName)

			// ordering doesn't matter
			for _, ev := range []corev1.EnvVar{
				{
					Name: schedupdate.PFPStatusDumpEnvVar,
				},
			} {
				gotEv := schedupdate.FindEnvVarByName(cnt.Env, ev.Name)
				Expect(gotEv).To(BeNil(), "unexpected environment variable %q in %q", ev.Name, cnt.Name)
			}
		})

		It("should configure by default the relaxed resync detection mode in configmap", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.ForeignPodsDetectOnlyExclusiveResources, depmanifests.CacheInformerDedicated)
		})

		It("should allow to set aggressive resync detection mode in configmap", func() {
			key := client.ObjectKeyFromObject(nrs)
			nrsUpdated := &nropv1.NUMAResourcesScheduler{}
			Expect(reconciler.Client.Get(context.TODO(), key, nrsUpdated)).To(Succeed())

			resyncDetect := nropv1.CacheResyncDetectionAggressive
			nrsUpdated.Spec.CacheResyncDetection = &resyncDetect
			Expect(reconciler.Client.Update(context.TODO(), nrsUpdated)).To(Succeed())

			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.ForeignPodsDetectAll, depmanifests.CacheInformerDedicated)
		})

		It("should configure by default the informerMode to be Dedicated", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())
			expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.CacheResyncOnlyExclusiveResources, depmanifests.CacheInformerDedicated)
		})

		It("should allow to change the informerMode to Shared", func() {
			nrs := nrs.DeepCopy()
			informerMode := nropv1.SchedulerInformerShared
			nrs.Spec.SchedulerInformer = &informerMode
			Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.CacheResyncOnlyExclusiveResources, depmanifests.CacheInformerShared)
		})

		It("should allow to change the informerMode to Dedicated", func() {
			nrs := nrs.DeepCopy()
			informerMode := nropv1.SchedulerInformerDedicated
			nrs.Spec.SchedulerInformer = &informerMode
			Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.CacheResyncOnlyExclusiveResources, depmanifests.CacheInformerDedicated)
		})

		It("should configure by default the ScoringStrategy to be LeastAllocated", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())
			var resources []depmanifests.ResourceSpecParams
			expectScoringStrategyParams(reconciler.Client, depmanifests.ScoringStrategyLeastAllocated, resources)

		})

		It("should allow to change the ScoringStrategy resources", func() {
			nrs := nrs.DeepCopy()
			nrs.Spec.ScoringStrategy = &nropv1.ScoringStrategyParams{}
			ResourceSpecParams := []nropv1.ResourceSpecParams{{Name: "cpu", Weight: 10}, {Name: "memory", Weight: 5}}
			nrs.Spec.ScoringStrategy.Resources = ResourceSpecParams
			Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())
			resources := []depmanifests.ResourceSpecParams{{Name: "cpu", Weight: 10}, {Name: "memory", Weight: 5}}
			expectScoringStrategyParams(reconciler.Client, depmanifests.ScoringStrategyLeastAllocated, resources)
		})

		It("should allow to change the ScoringStrategy to BalancedAllocation", func() {
			nrs := nrs.DeepCopy()
			nrs.Spec.ScoringStrategy = &nropv1.ScoringStrategyParams{}
			nrs.Spec.ScoringStrategy.Type = nropv1.BalancedAllocation
			Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())
			var resources []depmanifests.ResourceSpecParams
			expectScoringStrategyParams(reconciler.Client, depmanifests.ScoringStrategyBalancedAllocation, resources)
		})

		It("should allow to change the ScoringStrategy to MostAllocated", func() {
			nrs := nrs.DeepCopy()
			nrs.Spec.ScoringStrategy = &nropv1.ScoringStrategyParams{}
			nrs.Spec.ScoringStrategy.Type = nropv1.MostAllocated
			Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())
			var resources []depmanifests.ResourceSpecParams
			expectScoringStrategyParams(reconciler.Client, depmanifests.ScoringStrategyMostAllocated, resources)
		})

		It("should allow to change the ScoringStrategy to BalancedAllocation with resources", func() {
			nrs := nrs.DeepCopy()
			nrs.Spec.ScoringStrategy = &nropv1.ScoringStrategyParams{}
			nrs.Spec.ScoringStrategy.Type = nropv1.BalancedAllocation
			ResourceSpecParams := []nropv1.ResourceSpecParams{{Name: "cpu", Weight: 10}, {Name: "memory", Weight: 5}}
			nrs.Spec.ScoringStrategy.Resources = ResourceSpecParams
			Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())
			resources := []depmanifests.ResourceSpecParams{{Name: "cpu", Weight: 10}, {Name: "memory", Weight: 5}}
			expectScoringStrategyParams(reconciler.Client, depmanifests.ScoringStrategyBalancedAllocation, resources)
		})

		It("should allow to change the ScoringStrategy to MostAllocated with resources", func() {
			nrs := nrs.DeepCopy()
			nrs.Spec.ScoringStrategy = &nropv1.ScoringStrategyParams{}
			nrs.Spec.ScoringStrategy.Type = nropv1.MostAllocated
			ResourceSpecParams := []nropv1.ResourceSpecParams{{Name: "cpu", Weight: 10}, {Name: "memory", Weight: 5}}
			nrs.Spec.ScoringStrategy.Resources = ResourceSpecParams
			Eventually(func() bool {
				if err := reconciler.Client.Update(context.TODO(), nrs); err != nil {
					klog.Warningf("failed to update the scheduler object; err: %v", err)
					return false
				}
				return true
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())
			resources := []depmanifests.ResourceSpecParams{{Name: "cpu", Weight: 10}, {Name: "memory", Weight: 5}}
			expectScoringStrategyParams(reconciler.Client, depmanifests.ScoringStrategyMostAllocated, resources)
		})

		It("should set the leader election resource parameters by default", func() {
			nrs := nrs.DeepCopy()
			nrs.Spec.Replicas = ptr.To(int32(1))
			Eventually(reconciler.Client.Update).WithArguments(context.TODO(), nrs).WithPolling(30 * time.Second).WithTimeout(5 * time.Minute).Should(Succeed())

			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(nrs)})
			Expect(err).ToNot(HaveOccurred())
			expectLeaderElectParams(reconciler.Client, false, testNamespace, nrosched.LeaderElectionResourceName)
		})

		It("should set the leader election resource parameters to true default", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())
			expectLeaderElectParams(reconciler.Client, true, testNamespace, nrosched.LeaderElectionResourceName)
		})

		DescribeTable("should set the leader election resource parameters depending on replica count", func(replicas int32, expectedEnabled bool) {
			nrs := nrs.DeepCopy()
			nrs.Spec.Replicas = &replicas
			Eventually(reconciler.Client.Update).WithArguments(context.TODO(), nrs).WithPolling(30 * time.Second).WithTimeout(5 * time.Minute).Should(Succeed())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())
			expectLeaderElectParams(reconciler.Client, expectedEnabled, testNamespace, nrosched.LeaderElectionResourceName)
		},
			Entry("replicas=0", int32(0), false),
			Entry("replicas=1", int32(1), false),
			Entry("replicas=3", int32(3), true),
		)

		It("should detect replicas number by default when spec.Replicas is unset", func() {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			dp := &appsv1.Deployment{}
			Expect(reconciler.Client.Get(context.TODO(), client.ObjectKey{Namespace: testNamespace, Name: "secondary-scheduler"}, dp)).To(Succeed())
			Expect(*dp.Spec.Replicas).To(Equal(int32(numOfMasters)), "number of replicas is different than number of control-planes nodes; want=%d got=%d", numOfMasters, *dp.Spec.Replicas)
		})
	})

	When("setting the scheduler pod resource requests", func() {
		var nrs *nropv1.NUMAResourcesScheduler
		var reconciler *NUMAResourcesSchedulerReconciler
		numOfMasters := 3

		BeforeEach(func() {
			var err error
			nrs = testobjs.NewNUMAResourcesScheduler("numaresourcesscheduler", "some/url:latest", testSchedulerName, 11*time.Second)
			initObjects := []runtime.Object{nrs}
			initObjects = append(initObjects, fakeNodes(numOfMasters, 3)...)
			reconciler, err = NewFakeNUMAResourcesSchedulerReconciler(initObjects...)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should keep setting the legacy guaranteed values by default", func(ctx context.Context) {
			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			expectedRR := corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    mustParseResource("600m"),
					corev1.ResourceMemory: mustParseResource("1200Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    mustParseResource("600m"),
					corev1.ResourceMemory: mustParseResource("1200Mi"),
				},
			}

			dp := &appsv1.Deployment{}
			Expect(reconciler.Client.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "secondary-scheduler"}, dp)).To(Succeed())
			Expect(dp).To(HaveTheSameResourceRequirements(expectedRR))
		})

		It("should keep setting the legacy guaranteed values explicitly", func(ctx context.Context) {
			nrs := nrs.DeepCopy()
			nrs.Annotations = map[string]string{
				annotations.SchedulerQOSRequestAnnotation: "guaranteed",
			}
			Eventually(reconciler.Client.Update).WithArguments(ctx, nrs).WithPolling(30 * time.Second).WithTimeout(5 * time.Minute).Should(Succeed())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			expectedRR := corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    mustParseResource("600m"),
					corev1.ResourceMemory: mustParseResource("1200Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    mustParseResource("600m"),
					corev1.ResourceMemory: mustParseResource("1200Mi"),
				},
			}

			dp := &appsv1.Deployment{}
			Expect(reconciler.Client.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "secondary-scheduler"}, dp)).To(Succeed())
			Expect(dp).To(HaveTheSameResourceRequirements(expectedRR))
		})

		It("should setting the burstable values only if requested", func(ctx context.Context) {
			nrs := nrs.DeepCopy()
			nrs.Annotations = map[string]string{
				annotations.SchedulerQOSRequestAnnotation: annotations.SchedulerQOSRequestBurstable,
			}
			Eventually(reconciler.Client.Update).WithArguments(ctx, nrs).WithPolling(30 * time.Second).WithTimeout(5 * time.Minute).Should(Succeed())

			key := client.ObjectKeyFromObject(nrs)
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())

			expectedRR := corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    mustParseResource("150m"),
					corev1.ResourceMemory: mustParseResource("500Mi"),
				},
			}

			dp := &appsv1.Deployment{}
			Expect(reconciler.Client.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "secondary-scheduler"}, dp)).To(Succeed())
			Expect(dp).To(HaveTheSameResourceRequirements(expectedRR))
		})
	})

	Context("with kubelet PodResourcesAPI listing active pods by default", func() {
		var nrs *nropv1.NUMAResourcesScheduler
		var reconciler *NUMAResourcesSchedulerReconciler
		numOfMasters := 3

		When("kubelet fix is enabled", func() {
			fixedVersion, _ := platform.ParseVersion(activePodsResourcesSupportSince)

			DescribeTable("should configure by default the informerMode to the expected when field is not set", func(reconcilerPlatInfo PlatformInfo, expectedInformer string) {
				var err error
				nrs = testobjs.NewNUMAResourcesScheduler("numaresourcesscheduler", "some/url:latest", testSchedulerName, 11*time.Second)
				initObjects := []runtime.Object{nrs}
				initObjects = append(initObjects, fakeNodes(numOfMasters, 3)...)
				reconciler, err = NewFakeNUMAResourcesSchedulerReconciler(initObjects...)
				Expect(err).ToNot(HaveOccurred())

				reconciler.PlatformInfo = reconcilerPlatInfo

				key := client.ObjectKeyFromObject(nrs)
				_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())

				expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.CacheResyncOnlyExclusiveResources, expectedInformer)
			},
				Entry("with fixed Openshift the default informer is Shared", PlatformInfo{
					Platform: platform.OpenShift,
					Version:  fixedVersion,
				}, depmanifests.CacheInformerShared),
				Entry("with fixed Hypershift the default informer is Shared", PlatformInfo{
					Platform: platform.HyperShift,
					Version:  fixedVersion,
				}, depmanifests.CacheInformerShared),
				Entry("with unknown platform the default informer is Dedicated (unchanged)", PlatformInfo{}, depmanifests.CacheInformerDedicated))

			DescribeTable("should preserve informerMode value if set", func(reconcilerPlatInfo PlatformInfo) {
				var err error
				nrs = testobjs.NewNUMAResourcesScheduler("numaresourcesscheduler", "some/url:latest", testSchedulerName, 11*time.Second)
				infMode := nropv1.SchedulerInformerDedicated
				nrs.Spec.SchedulerInformer = &infMode
				initObjects := []runtime.Object{nrs}
				initObjects = append(initObjects, fakeNodes(numOfMasters, 3)...)
				reconciler, err = NewFakeNUMAResourcesSchedulerReconciler(initObjects...)
				Expect(err).ToNot(HaveOccurred())

				reconciler.PlatformInfo = reconcilerPlatInfo

				key := client.ObjectKeyFromObject(nrs)
				_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.CacheResyncOnlyExclusiveResources, string(infMode))
			},
				Entry("with Openshift", PlatformInfo{
					Platform: platform.OpenShift,
					Version:  fixedVersion,
				}),
				Entry("with Hypershift", PlatformInfo{
					Platform: platform.HyperShift,
					Version:  fixedVersion,
				}),
				Entry("with unknown platform", PlatformInfo{}))

			DescribeTable("should allow to update the informerMode to be Dedicated after an overridden default", func(reconcilerPlatInfo PlatformInfo) {
				var err error
				nrs = testobjs.NewNUMAResourcesScheduler("numaresourcesscheduler", "some/url:latest", testSchedulerName, 11*time.Second)
				initObjects := []runtime.Object{nrs}
				initObjects = append(initObjects, fakeNodes(numOfMasters, 3)...)
				reconciler, err = NewFakeNUMAResourcesSchedulerReconciler(initObjects...)
				Expect(err).ToNot(HaveOccurred())

				reconciler.PlatformInfo = reconcilerPlatInfo

				key := client.ObjectKeyFromObject(nrs)
				_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())

				// intentionally skip checking default value

				// should query the object after reconcile because the defaults are overridden
				Expect(reconciler.Client.Get(context.TODO(), key, nrs)).ToNot(HaveOccurred())

				nrsUpdated := nrs.DeepCopy()
				informerMode := nropv1.SchedulerInformerDedicated
				nrsUpdated.Spec.SchedulerInformer = &informerMode
				Eventually(func() bool {
					if err := reconciler.Client.Update(context.TODO(), nrsUpdated); err != nil {
						klog.Warningf("failed to update the scheduler object; err: %v", err)
						return false
					}
					return true
				}, 30*time.Second, 5*time.Second).Should(BeTrue())

				_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())

				expectCacheParams(reconciler.Client, depmanifests.CacheResyncAutodetect, depmanifests.CacheResyncOnlyExclusiveResources, string(informerMode))
			},
				Entry("with Openshift", PlatformInfo{
					Platform: platform.OpenShift,
					Version:  fixedVersion,
				}),
				Entry("with Hypershift", PlatformInfo{
					Platform: platform.HyperShift,
					Version:  fixedVersion,
				}))
		})
	})
})

var _ = Describe("Test computeSchedulerReplicas", func() {
	var reconciler *NUMAResourcesSchedulerReconciler

	BeforeEach(func() {
		var err error
		reconciler, err = NewFakeNUMAResourcesSchedulerReconciler()
		Expect(err).ToNot(HaveOccurred())
	})

	DescribeTable("should compute replicas correctly for different platforms and node counts",
		func(platform platform.Platform, numControlPlane, numWorker int, expectedReplicas *int32, expectError bool) {
			// setup reconciler with platform info
			reconciler.PlatformInfo = PlatformInfo{
				Platform: platform,
				Version:  "v4.14.0",
			}

			// create nodes based on test scenario
			var nodes []runtime.Object
			if numControlPlane > 0 {
				nodes = append(nodes, fakeNodes(numControlPlane, 0)...)
			}
			if numWorker > 0 {
				nodes = append(nodes, fakeNodes(0, numWorker)...)
			}

			// recreate reconciler with nodes
			reconciler, err := NewFakeNUMAResourcesSchedulerReconciler(nodes...)
			Expect(err).ToNot(HaveOccurred())
			reconciler.PlatformInfo = PlatformInfo{
				Platform: platform,
				Version:  "v4.14.0",
			}

			// create test scheduler spec with no replicas set (auto-detect)
			schedSpec := nropv1.NUMAResourcesSchedulerSpec{
				Replicas: nil, // force auto-detection
			}

			// call the function under test
			result, err := reconciler.computeSchedulerReplicas(context.TODO(), schedSpec)

			if expectError {
				Expect(err).To(HaveOccurred())
				Expect(result).To(BeNil())
			} else {
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(*result).To(Equal(*expectedReplicas))
			}
		},
		// OpenShift platform tests (uses control-plane nodes)
		Entry("OpenShift: no control-plane nodes should return error", platform.OpenShift, 0, 5, nil, true),
		Entry("OpenShift: 1 control-plane node should return 1 replica", platform.OpenShift, 1, 0, ptr.To(int32(1)), false),
		Entry("OpenShift: 2 control-plane nodes should return 1 replica", platform.OpenShift, 2, 0, ptr.To(int32(1)), false),
		Entry("OpenShift: 3 control-plane nodes should return 3 replicas", platform.OpenShift, 3, 0, ptr.To(int32(3)), false),
		Entry("OpenShift: 4 control-plane nodes should be capped at 3 replicas", platform.OpenShift, 4, 0, ptr.To(int32(3)), false),
		Entry("OpenShift: 5 control-plane nodes should be capped at 3 replicas", platform.OpenShift, 5, 0, ptr.To(int32(3)), false),

		// HyperShift platform tests (uses worker nodes)
		Entry("HyperShift: no worker nodes should return error", platform.HyperShift, 3, 0, nil, true),
		Entry("HyperShift: 1 worker node should return 1 replica", platform.HyperShift, 0, 1, ptr.To(int32(1)), false),
		Entry("HyperShift: 2 worker nodes should return 1 replica", platform.HyperShift, 0, 2, ptr.To(int32(1)), false),
		Entry("HyperShift: 3 worker nodes should return 3 replicas", platform.HyperShift, 0, 3, ptr.To(int32(3)), false),
		Entry("HyperShift: 4 worker nodes should be capped at 3 replicas", platform.HyperShift, 0, 4, ptr.To(int32(3)), false),
		Entry("HyperShift: 10 worker nodes should be capped at 3 replicas", platform.HyperShift, 0, 10, ptr.To(int32(3)), false),

		// mixed scenarios to ensure platform-specific node selection works
		Entry("OpenShift: 3 control-plane + 5 worker should return 3 replicas (uses control-plane)", platform.OpenShift, 3, 5, ptr.To(int32(3)), false),
		Entry("HyperShift: 3 control-plane + 5 worker should return 3 replicas (uses worker)", platform.HyperShift, 3, 5, ptr.To(int32(3)), false),
	)

	Context("when replicas are explicitly set", func() {
		It("should return the explicitly set replicas without checking nodes", func() {
			// setup reconciler with HyperShift platform (uses worker nodes)
			reconciler.PlatformInfo = PlatformInfo{
				Platform: platform.HyperShift,
				Version:  "v4.14.0",
			}

			// create scenario with no worker nodes but explicit replicas
			nodes := fakeNodes(3, 0) // only control-plane nodes
			reconciler, err := NewFakeNUMAResourcesSchedulerReconciler(nodes...)
			Expect(err).ToNot(HaveOccurred())
			reconciler.PlatformInfo = PlatformInfo{
				Platform: platform.HyperShift,
				Version:  "v4.14.0",
			}

			// create test scheduler spec with explicit replicas
			explicitReplicas := int32(5)
			schedSpec := nropv1.NUMAResourcesSchedulerSpec{
				Replicas: &explicitReplicas,
			}

			// call the function under test
			result, err := reconciler.computeSchedulerReplicas(context.TODO(), schedSpec)

			// should return the explicit value without error
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
			Expect(*result).To(Equal(explicitReplicas))
		})
	})
})

var _ = Describe("Test scheduler spec PreNormalize", func() {
	When("Spec.SchedulerInformer is not set by the user", func() {
		It("should override default informer to Shared if kubelet is fixed - first supported zstream version", func() {
			v, _ := platform.ParseVersion(activePodsResourcesSupportSince)
			spec := nropv1.NUMAResourcesSchedulerSpec{}
			platformNormalize(&spec, PlatformInfo{Platform: platform.OpenShift, Version: v})
			Expect(*spec.SchedulerInformer).To(Equal(nropv1.SchedulerInformerShared))
		})

		It("should override default informer to Shared if kubelet is fixed - version is greater than first supported (zstream)", func() {
			v, _ := platform.ParseVersion("4.20.1000")
			spec := nropv1.NUMAResourcesSchedulerSpec{}
			platformNormalize(&spec, PlatformInfo{Platform: platform.OpenShift, Version: v})
			Expect(*spec.SchedulerInformer).To(Equal(nropv1.SchedulerInformerShared))
		})

		It("should override default informer to Shared if kubelet is fixed - version is greater than first supported (ystream)", func() {
			v, _ := platform.ParseVersion("4.21.0")
			spec := nropv1.NUMAResourcesSchedulerSpec{}
			platformNormalize(&spec, PlatformInfo{Platform: platform.OpenShift, Version: v})
			Expect(*spec.SchedulerInformer).To(Equal(nropv1.SchedulerInformerShared))
		})

		It("should not override default informer if kubelet is not fixed - version is less than first supported (zstream)", func() {
			// this is only for testing purposes as there is plan to backport the fix to older minor versions
			// will need to remove this test if the fix is supported starting the first zstream of the release
			v, _ := platform.ParseVersion("4.20.0")
			spec := nropv1.NUMAResourcesSchedulerSpec{}
			platformNormalize(&spec, PlatformInfo{Platform: platform.OpenShift, Version: v})
			Expect(spec.SchedulerInformer).To(BeNil())
		})

		It("should not override default informer if kubelet is not fixed - version is less than first supported (ystream)", func() {
			v, _ := platform.ParseVersion("4.13.0")
			spec := nropv1.NUMAResourcesSchedulerSpec{}
			platformNormalize(&spec, PlatformInfo{Platform: platform.OpenShift, Version: v})
			Expect(spec.SchedulerInformer).To(BeNil())
		})
	})
	When("Spec.SchedulerInformer is set by the user", func() {
		It("should preserve informer value set by the user even if kubelet is fixed", func() {
			v, _ := platform.ParseVersion(activePodsResourcesSupportSince)
			spec := nropv1.NUMAResourcesSchedulerSpec{
				SchedulerInformer: ptr.To(nropv1.SchedulerInformerDedicated),
			}
			platformNormalize(&spec, PlatformInfo{Platform: platform.OpenShift, Version: v})
			Expect(*spec.SchedulerInformer).To(Equal(nropv1.SchedulerInformerDedicated))
		})
	})
})

func HaveTheSameResourceRequirements(expectedRR corev1.ResourceRequirements) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual *appsv1.Deployment) (bool, error) {
		cntName := schedupdate.MainContainerName // shortcut
		cnt := depobjupdate.FindContainerByName(actual.Spec.Template.Spec.Containers, cntName)
		if cnt == nil {
			return false, fmt.Errorf("cannot find container %q", cntName)
		}
		return reflect.DeepEqual(cnt.Resources, expectedRR), nil
	}).WithTemplate("Deployment {{.Actual.Namespace}}/{{.Actual.Name}} resources request mismatch")
}

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
	GinkgoHelper()

	key := client.ObjectKey{
		Name:      "topo-aware-scheduler-config",
		Namespace: testNamespace,
	}

	cm := corev1.ConfigMap{}
	Expect(cli.Get(context.TODO(), key, &cm)).To(Succeed())

	confRaw := cm.Data[sched.SchedulerConfigFileName]
	cfgs, err := depmanifests.DecodeSchedulerProfilesFromData([]byte(confRaw))
	Expect(err).ToNot(HaveOccurred())
	Expect(cfgs).To(HaveLen(1), "unexpected config params count: %d", len(cfgs))
	cfg := cfgs[0]

	klog.InfoS("config", dumpConfigCacheParams(cfg.Cache)...)

	Expect(cfg.Cache.ResyncMethod).ToNot(BeNil())
	Expect(*cfg.Cache.ResyncMethod).To(Equal(resyncMethod))
	Expect(cfg.Cache.ForeignPodsDetectMode).ToNot(BeNil())
	Expect(*cfg.Cache.ForeignPodsDetectMode).To(Equal(foreignPodsDetect))
	Expect(cfg.Cache.InformerMode).ToNot(BeNil())
	Expect(*cfg.Cache.InformerMode).To(Equal(informerMode))
}

func expectScoringStrategyParams(cli client.Client, scoringStrategyType string, resources []depmanifests.ResourceSpecParams) {
	GinkgoHelper()

	key := client.ObjectKey{
		Name:      "topo-aware-scheduler-config",
		Namespace: testNamespace,
	}

	cm := corev1.ConfigMap{}
	Expect(cli.Get(context.TODO(), key, &cm)).To(Succeed())

	confRaw := cm.Data[sched.SchedulerConfigFileName]
	cfgs, err := depmanifests.DecodeSchedulerProfilesFromData([]byte(confRaw))
	Expect(err).ToNot(HaveOccurred())
	Expect(cfgs).To(HaveLen(1), "unexpected config params count: %d", len(cfgs))
	cfg := cfgs[0]

	Expect(cfg.ScoringStrategy.Type).To(Equal(scoringStrategyType))
	Expect(cfg.ScoringStrategy.Resources).To(Equal(resources))
}

func expectLeaderElectParams(cli client.Client, enabled bool, resourceNamespace, resourceName string) {
	GinkgoHelper()

	key := client.ObjectKey{
		Name:      "topo-aware-scheduler-config",
		Namespace: testNamespace,
	}

	cm := corev1.ConfigMap{}
	Expect(cli.Get(context.TODO(), key, &cm)).To(Succeed())

	confRaw := cm.Data[sched.SchedulerConfigFileName]
	cfgs, err := depmanifests.DecodeSchedulerProfilesFromData([]byte(confRaw))
	Expect(err).ToNot(HaveOccurred())
	Expect(cfgs).To(HaveLen(1), "unexpected config params count: %d", len(cfgs))
	cfg := cfgs[0]

	Expect(cfg.LeaderElection.LeaderElect).To(Equal(enabled))
	Expect(cfg.LeaderElection.ResourceNamespace).To(Equal(resourceNamespace))
	Expect(cfg.LeaderElection.ResourceName).To(Equal(resourceName))
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

func mustParseResource(v string) resource.Quantity {
	GinkgoHelper()
	qty, err := resource.ParseQuantity(v)
	if err != nil {
		Fail(fmt.Sprintf("cannot parse %q: %v", v, err))
	}
	return qty
}
