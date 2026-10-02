/*
 * Copyright 2026 Red Hat, Inc.
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

package numazone

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/numazoneresource"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func FindPluginPods(ctx context.Context, fxt *e2efixture.Fixture, nro *nropv1.NUMAResourcesOperator) map[string]*corev1.Pod {
	GinkgoHelper()
	var keys []client.ObjectKey
	for _, group := range nro.Status.NodeGroups {
		if group.NumazoneDaemonSet != nil {
			keys = append(keys, client.ObjectKey{Namespace: group.NumazoneDaemonSet.Namespace, Name: group.NumazoneDaemonSet.Name})
		}
	}
	Expect(keys).ToNot(BeEmpty(), "numazone must already be Enabled in the operator's NodeGroups")
	podsByNode := map[string]*corev1.Pod{}
	for _, key := range keys {
		ds, err := wait.With(fxt.Client).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, wait.ObjectKey{Namespace: key.Namespace, Name: key.Name})
		Expect(err).ToNot(HaveOccurred())
		if !enforcesAdmission(ds.Spec.Template.Spec.Containers) {
			continue
		}
		Expect(ds.Status.ObservedGeneration).To(BeNumerically(">=", ds.Generation), "numazone DaemonSet generation has not been observed")
		Expect(ds.Status.UpdatedNumberScheduled).To(Equal(ds.Status.DesiredNumberScheduled), "numazone DaemonSet rollout is incomplete")
		selector, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
		Expect(err).ToNot(HaveOccurred())
		var pods corev1.PodList
		Expect(fxt.Client.List(ctx, &pods, client.InNamespace(key.Namespace), client.MatchingLabelsSelector{Selector: selector})).To(Succeed())
		for idx := range pods.Items {
			pod := &pods.Items[idx]
			if !metav1.IsControlledBy(pod, ds) || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
				continue
			}
			if !enforcesAdmission(pod.Spec.Containers) {
				continue
			}
			Expect(podsByNode).ToNot(HaveKey(pod.Spec.NodeName), "multiple numazone pods on one node")
			podsByNode[pod.Spec.NodeName] = pod.DeepCopy()
		}
	}
	Expect(podsByNode).ToNot(BeEmpty(), "need an enforcing numazone DaemonSet with admission synchronization enabled")
	return podsByNode
}

func enforcesAdmission(containers []corev1.Container) bool {
	for _, container := range containers {
		if container.Name != numazoneresource.ContainerName {
			continue
		}
		return pluginFlagValue(container.Args, "mode", "enforcing") == "enforcing" &&
			pluginFlagValue(container.Args, "admission-sync", "true") == "true"
	}
	return false
}

func pluginFlagValue(args []string, name, fallback string) string {
	value := fallback
	for idx, arg := range args {
		flag := strings.TrimLeft(arg, "-")
		if flag == name && idx+1 < len(args) && !strings.HasPrefix(args[idx+1], "-") {
			value = args[idx+1]
		} else if strings.HasPrefix(flag, name+"=") {
			value = strings.TrimPrefix(flag, name+"=")
		}
	}
	return value
}

func WaitForAllocations(ctx context.Context, fxt *e2efixture.Fixture, pod *corev1.Pod, total int64, timeout time.Duration) Metrics {
	GinkgoHelper()
	var snapshot Metrics
	Eventually(func(g Gomega) {
		assertPluginUnchangedWith(g, ctx, fxt, pod)
		var err error
		snapshot, err = FetchMetricsFromPod(ctx, fxt.K8sClient, pod)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(snapshot.Total()).To(Equal(total), "NUMA allocations: %v", snapshot.Allocated)
	}).WithContext(ctx).WithTimeout(timeout).WithPolling(2 * time.Second).Should(Succeed())
	return snapshot
}

func AssertPluginUnchanged(ctx context.Context, fxt *e2efixture.Fixture, original *corev1.Pod) {
	GinkgoHelper()
	assertPluginUnchangedWith(Default, ctx, fxt, original)
}

func assertPluginUnchangedWith(g Gomega, ctx context.Context, fxt *e2efixture.Fixture, original *corev1.Pod) {
	var current corev1.Pod
	g.Expect(fxt.Client.Get(ctx, client.ObjectKeyFromObject(original), &current)).To(Succeed())
	g.Expect(current.UID).To(Equal(original.UID), "numazone pod replaced during test")
	g.Expect(current.DeletionTimestamp).To(BeNil(), "numazone pod is terminating")
	g.Expect(current.Status.Phase).To(Equal(corev1.PodRunning))
	for _, before := range original.Status.ContainerStatuses {
		if before.Name != numazoneresource.ContainerName {
			continue
		}
		for _, after := range current.Status.ContainerStatuses {
			if after.Name == before.Name {
				g.Expect(after.RestartCount).To(Equal(before.RestartCount), "numazone restarted during test")
				g.Expect(after.ContainerID).To(Equal(before.ContainerID), "numazone container replaced during test")
				g.Expect(after.State.Running).ToNot(BeNil())
				g.Expect(after.Ready).To(BeTrue())
				return
			}
		}
	}
	g.Expect(false).To(BeTrue(), "numazone container status missing")
}
