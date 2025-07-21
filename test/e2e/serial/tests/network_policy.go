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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	e2etestenv "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/testenv"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/deploy"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This test suite verifies that the correct network policies are applied and enforced.
// Specifically, it checks the following:
// - All ingress and egress traffic is denied by default for the NUMAResources operator.
// - Egress traffic from the operator, RTE, and scheduler pods to the Kubernetes API server is allowed.
// - Ingress and egress traffic to/from other pods in the cluster is restricted.
// - Ingress traffic to the metrics endpoints is allowed.
//
// Full coverage for inter-pod communication is challenging, so we include basic tests to validate the expected behavior.

var _ = Describe("network policies are applied", Ordered, Label("feature:network_policies"), func() {
	var namespace string
	var ctx context.Context
	var nropObj *nropv1.NUMAResourcesOperator
	var nroSchedObj *nropv1.NUMAResourcesScheduler
	var operatorPod, schedulerPod, rteWorkerPod, prometheusPod *corev1.Pod

	BeforeAll(func() {
		ctx = context.Background()
		nropObj = objects.TestNRO()
		Expect(e2eclient.Client.Get(ctx, client.ObjectKeyFromObject(nropObj), nropObj)).To(Succeed())

		nroSchedObj = objects.TestNROScheduler()
		Expect(e2eclient.Client.Get(ctx, client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)).To(Succeed())

		Expect(nropObj.Status.NodeGroups).ToNot(BeEmpty())
		namespace = nropObj.Status.NodeGroups[0].DaemonSet.Namespace

		var err error
		operatorPod, err = deploy.FindNUMAResourcesOperatorPod(ctx, e2eclient.Client, nropObj)
		Expect(err).ToNot(HaveOccurred())

		schedulerPod, err = deploy.FindNUMAResourcesSchedulerPod(ctx, e2eclient.Client, nroSchedObj)
		Expect(err).ToNot(HaveOccurred())

		pods, err := e2eclient.K8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("name=%s", e2etestenv.RTELabelName),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(pods.Items).ToNot(BeEmpty())
		rteWorkerPod = &pods.Items[0]

		prometheusPods, err := e2eclient.K8sClient.CoreV1().Pods("openshift-monitoring").List(ctx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(map[string]string{
				"app.kubernetes.io/name": "prometheus",
			}).String(),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(prometheusPods.Items).ToNot(BeEmpty())
		prometheusPod = &prometheusPods.Items[0]
	})

	type trafficCase struct {
		FromPod     func() *corev1.Pod
		ToHost      func() string
		ToPort      string
		ShouldAllow bool
		Description string
	}

	DescribeTable("traffic behavior",
		func(tc trafficCase) {
			Expect(tc.FromPod).ToNot(BeNil(), "source pod should not be nil")
			klog.InfoS("Running traffic test", "description", tc.Description)
			reachable := trafficTest(e2eclient.K8sClient, ctx, tc.FromPod(), tc.ToHost(), tc.ToPort)
			klog.InfoS("Traffic test result", "reachable", reachable)
			Expect(reachable).To(Equal(tc.ShouldAllow), tc.Description)
		},
		// Testing operator and operands egress traffic to API server
		Entry("operator -> API server", trafficCase{
			FromPod:     func() *corev1.Pod { return operatorPod },
			ToHost:      func() string { return "$KUBERNETES_SERVICE_HOST" },
			ToPort:      "$KUBERNETES_SERVICE_PORT",
			ShouldAllow: true,
			Description: "operator should access API server",
		}),
		Entry("scheduler -> API server", trafficCase{
			FromPod:     func() *corev1.Pod { return schedulerPod },
			ToHost:      func() string { return "$KUBERNETES_SERVICE_HOST" },
			ToPort:      "$KUBERNETES_SERVICE_PORT",
			ShouldAllow: true,
			Description: "scheduler should access API server",
		}),
		Entry("rte worker -> API server", trafficCase{
			FromPod:     func() *corev1.Pod { return rteWorkerPod },
			ToHost:      func() string { return "$KUBERNETES_SERVICE_HOST" },
			ToPort:      "$KUBERNETES_SERVICE_PORT",
			ShouldAllow: true,
			Description: "rte worker should access API server",
		}),

		// Testing operator and RTE metrics endpoints
		Entry("prometheus operator -> numaresouces operator metrics endpoint", trafficCase{
			FromPod:     func() *corev1.Pod { return prometheusPod },
			ToHost:      func() string { return operatorPod.Status.PodIP },
			ToPort:      "8080",
			ShouldAllow: true,
			Description: "prometheus operator pod should access numaresources operator metrics endpoint",
		}),

		Entry("prometheus operator -> numaresouces rte worker endpoint", trafficCase{
			FromPod:     func() *corev1.Pod { return prometheusPod },
			ToHost:      func() string { return rteWorkerPod.Status.PodIP },
			ToPort:      "2112",
			ShouldAllow: true,
			Description: "prometheus operator pod should access rte worker metrics endpoint",
		}),

		// Testing traffic restrictions between pods in the numaresources namespace
		Entry("scheduler -> operator", trafficCase{
			FromPod:     func() *corev1.Pod { return schedulerPod },
			ToHost:      func() string { return operatorPod.Status.PodIP },
			ToPort:      "8081",
			ShouldAllow: false,
			Description: "scheduler should NOT access operator",
		}),

		// Testing network traffic restrictions between pods cross namespaces (numaresouces and openshift-monitoring)
		Entry("numaresouces operator -> prometheus operator", trafficCase{
			FromPod:     func() *corev1.Pod { return operatorPod },
			ToHost:      func() string { return prometheusPod.Status.PodIP },
			ToPort:      "8081",
			ShouldAllow: false,
			Description: "numaresources operator should NOT access prometheus operator pod",
		}),

		Entry("prometheus operator -> numaresouces operator", trafficCase{
			FromPod:     func() *corev1.Pod { return prometheusPod },
			ToHost:      func() string { return operatorPod.Status.PodIP },
			ToPort:      "8081", // readinessProbe!
			ShouldAllow: false,
			Description: "prometheus operator pod should NOT access numaresources operator pod's readiness probe endpoint)",
		}),
	)
})

// trafficTest returns true if the sourcePod can connect to the given destination IP and port over HTTP.
// It is used to validate network connectivity (e.g., for testing networkPolicy behavior).
func trafficTest(cli *kubernetes.Clientset, ctx context.Context, sourcePod *corev1.Pod, destinationIP, destinationPort string) bool {
	GinkgoHelper()
	endpoint := net.JoinHostPort(destinationIP, destinationPort)

	key := client.ObjectKeyFromObject(sourcePod)
	By(fmt.Sprintf("verifying HTTP egress connectivity from pod %q to endpoint %s", key.String(), endpoint))

	cmd := []string{"sh", "-c", fmt.Sprintf("curl --connect-timeout 5 http://%s", endpoint)}

	stdout, stderr, err := remoteexec.CommandOnPod(ctx, cli, sourcePod, cmd...)

	if err != nil {
		GinkgoWriter.Printf("curl failed: stdout=%q, stderr=%q, err=%v\n", stdout, stderr, err)
	}
	return err == nil
}
