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

package controllers

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/google/go-cmp/cmp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	depmanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
	schedupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/status"

	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
)

const testSchedulerName = "testSchedulerName"

func NewFakeNUMAResourcesSchedulerReconciler(initObjects ...runtime.Object) (*NUMAResourcesSchedulerReconciler, error) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(initObjects...).Build()
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
			verifyDegradedCondition(nrs, conditionTypeIncorrectNUMAResourcesSchedulerResourceName)
		})
	})

	ginkgo.Context("with correct NRS CR", func() {
		var nrs *nropv1.NUMAResourcesScheduler
		var reconciler *NUMAResourcesSchedulerReconciler

		ginkgo.BeforeEach(func() {
			var err error
			nrs = testobjs.NewNUMAResourcesScheduler("numaresourcesscheduler", "some/url:latest", testSchedulerName, 11*time.Second)
			reconciler, err = NewFakeNUMAResourcesSchedulerReconciler(nrs)
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

			cnt, err := schedupdate.FindContainerByName(&dp.Spec.Template.Spec, schedupdate.MainContainerName)
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), "cannot find container %q in deployment", schedupdate.MainContainerName)

			// ordering doesn't matter
			for _, ev := range []corev1.EnvVar{
				{
					Name:  schedupdate.PFPStatusDumpEnvVar,
					Value: schedupdate.PFPStatusDir,
				},
				{
					Name:  schedupdate.NRTInformerEnvVar,
					Value: schedupdate.NRTInformerVal,
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

			cnt, err := schedupdate.FindContainerByName(&dp.Spec.Template.Spec, schedupdate.MainContainerName)
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), "cannot find container %q in deployment", schedupdate.MainContainerName)

			// ordering doesn't matter
			for _, ev := range []corev1.EnvVar{
				{
					Name: schedupdate.PFPStatusDumpEnvVar,
				},
				{
					Name: schedupdate.NRTInformerEnvVar,
				},
			} {
				gotEv := schedupdate.FindEnvVarByName(cnt.Env, ev.Name)
				gomega.Expect(gotEv).To(gomega.BeNil(), "unexpected environment variable %q in %q", ev.Name, cnt.Name)
			}
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
