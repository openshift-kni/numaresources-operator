/*
 * Copyright 2025 Red Hat, Inc.
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

package tests

import (
	"context"
	"fmt"
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	e2etestenv "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/testenv"
	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/deploy"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
)

const metricsAddress = "127.0.0.1"

// This test verifies that the HTTPS metrics endpoints, are accessible and serving metrics.
var _ = Describe("metrics exposed securely", Serial, func() {
	ctx := context.Background()
	var namespace string
	var nropObj *nropv1.NUMAResourcesOperator

	BeforeEach(func() {
		nropObj = objects.TestNRO()
		nname := client.ObjectKeyFromObject(nropObj)

		Expect(e2eclient.Client.Get(ctx, nname, nropObj)).To(Succeed(), "failed to get the NRO resource")

		Expect(nropObj.Status.NodeGroups).ToNot(BeEmpty(), "node groups not reported, nothing to do")
		// nrop places all daemonsets in the same namespace on which it resides, so any group is fine
		namespace = nropObj.Status.NodeGroups[0].DaemonSet.Namespace // shortcut
	})

	When("testing operator metrics endpoint", func() {
		metricsPort := "8080"

		It("should be able to fetch metrics from the manager container", func() {
			managerPod, err := deploy.FindNUMAResourcesOperatorPod(ctx, e2eclient.Client, nropObj)
			Expect(err).ToNot(HaveOccurred())
			stdout := fetchMetricsFromPod(ctx, managerPod, metricsAddress, metricsPort)
			Expect(strings.Contains(stdout, "workqueue_adds_total{controller=\"numaresourcesoperator\",name=\"numaresourcesoperator\"}")).To(BeTrue(), "workqueue_adds_total operator metric not found")
			Expect(strings.Contains(stdout, "workqueue_adds_total{controller=\"numaresourcesscheduler\",name=\"numaresourcesscheduler\"}")).To(BeTrue(), "workqueue_adds_total scheduler metric not found")
		})
	})

	When("testing RTE metrics endpoint", func() {
		metricsPort := "2112"

		It("should be able to fetch metrics from the RTE pod container", func() {
			labelSelector := fmt.Sprintf("name=%s", e2etestenv.RTELabelName)
			pods, err := e2eclient.K8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			Expect(err).ToNot(HaveOccurred(), "Error listing worker pods: %v", err)
			Expect(pods.Items).ToNot(BeEmpty(), "There should be at least one worker pod")
			workerPod := &pods.Items[0]
			stdout := fetchMetricsFromPod(ctx, workerPod, metricsAddress, metricsPort)
			Expect(strings.Contains(stdout, "rte_noderesourcetopology_writes_total")).To(BeTrue(), "rte_noderesourcetopology_writes_total metric not found")
			Expect(strings.Contains(stdout, "rte_wakeup_delay_milliseconds")).To(BeTrue(), "rte_wakeup_delay_milliseconds metric not found")
		})
	})
})

func fetchMetricsFromPod(ctx context.Context, pod *corev1.Pod, metricsAddress, metricsPort string) string {
	GinkgoHelper()
	endpoint := net.JoinHostPort(metricsAddress, metricsPort)

	key := client.ObjectKeyFromObject(pod)
	By("running curl command to fetch metrics")
	cmd := []string{
		"/bin/curl",
		"-k",
		fmt.Sprintf("https://%s/metrics", endpoint),
	}
	klog.V(2).Infof("executing cmd: %s on pod %q", cmd, key.String())
	stdout, stderr, err := remoteexec.CommandOnPod(ctx, e2eclient.K8sClient, pod, cmd...)
	Expect(err).ToNot(HaveOccurred(), "failed exec command on pod. pod=%q; cmd=%q; err=%v; stderr=%q", key.String(), cmd, err, stderr)
	Expect(stdout).NotTo(BeEmpty(), stdout)
	return string(stdout)
}
