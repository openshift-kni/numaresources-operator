package tests

import (
	"context"
	"fmt"
	"net"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
)

// This test verifies that the HTTPS metrics endpoints, exposed by the kube-rbac-proxy sidecars, are accessible and serving metrics.
var _ = Describe("metrics exposed securely", Serial, func() {
	metricsPort := "8443"
	metricsAddress := "127.0.0.1"
	namespace := "numaresources"

	Context("testing metrics e2e from manager pod", func() {
		labelSelector := "control-plane=controller-manager"
		pods, err := e2eclient.K8sClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		Expect(err).To(BeNil(), "Error listing manager pods: %v", err)
		Expect(len(pods.Items)).To(Equal(1), "There should be one manager pod")
		managerPod := &pods.Items[0]

		It("should be able to fetch metrics from the manager container using kube-rbac-proxy", func() {
			stdout := fetchMetricsFromPod(managerPod, metricsAddress, metricsPort)
			Expect(strings.Contains(stdout, "workqueue_work_duration_seconds_count{name=\"numaresourcesoperator\"}")).To(BeTrue(), "Operator metric not found")
			Expect(strings.Contains(stdout, "workqueue_work_duration_seconds_count{name=\"numaresourcesscheduler\"}")).To(BeTrue(), "Scheduler metric not found")
		})
	})

	Context("testing metrics e2e from rte worker pod", func() {
		labelSelector := "name=resource-topology"
		pods, err := e2eclient.K8sClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		Expect(err).To(BeNil(), "Error listing worker pods: %v", err)
		Expect(len(pods.Items)).To(BeNumerically(">=", 1), "There should be at least one worker pod")
		workerPod := &pods.Items[0]

		It("should be able to fetch metrics from the worker pod container using kube-rbac-proxy", func() {
			stdout := fetchMetricsFromPod(workerPod, metricsAddress, metricsPort)
			Expect(strings.Contains(stdout, "go_gc_duration_seconds_sum")).To(BeTrue(), "go_gc_duration_seconds_sum metric not found")
			Expect(strings.Contains(stdout, "go_gc_duration_seconds_count")).To(BeTrue(), "go_gc_duration_seconds_count metric not found")
		})
	})
})

func fetchMetricsFromPod(pod *corev1.Pod, metricsAddress, metricsPort string) string {
	GinkgoHelper()
	endpoint := net.JoinHostPort(metricsAddress, metricsPort)

	By("fetching the authorization token from the pod")
	cmd := []string{"cat", "/var/run/secrets/kubernetes.io/serviceaccount/token"}
	key := client.ObjectKeyFromObject(pod)
	klog.V(2).Infof("executing cmd: %s on pod %q", cmd, key.String())
	token, stderr, err := remoteexec.CommandOnPod(context.Background(), e2eclient.K8sClient, pod, cmd...)
	Expect(err).ToNot(HaveOccurred(), "failed exec command on pod. pod=%q; cmd=%q; err=%v; stderr=%q", key.String(), cmd, err, stderr)

	By("running curl command to fetch metrics")
	cmd = []string{
		"curl",
		"-k",
		"-H", fmt.Sprintf("Authorization: Bearer %s", token),
		fmt.Sprintf("https://%s/metrics", endpoint),
	}
	klog.V(2).Infof("executing cmd: %s on pod %q", cmd, key.String())
	stdout, stderr, err := remoteexec.CommandOnPod(context.Background(), e2eclient.K8sClient, pod, cmd...)
	Expect(err).ToNot(HaveOccurred(), "failed exec command on pod. pod=%q; cmd=%q; err=%v; stderr=%q", key.String(), cmd, err, stderr)
	Expect(stdout).NotTo(BeEmpty(), stdout)
	return string(stdout)
}
