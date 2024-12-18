package tests

import (
	"context"
	"fmt"
	"net"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/deploy"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2etestenv "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/testenv"

)

// This test verifies that the HTTPS metrics endpoints, are accessible and serving metrics.
var _ = Describe("metrics exposed securely", Serial, func() {
	metricsAddress := "127.0.0.1"
	var namespace string
	var err error
	var nropObj *nropv1.NUMAResourcesOperator

	BeforeEach(func() {
		nropObj = objects.TestNRO()
		nname := client.ObjectKeyFromObject(nropObj)

		err = e2eclient.Client.Get(context.TODO(), nname, nropObj)
		Expect(err).NotTo(HaveOccurred(), "failed to get the NRO resource: %v", err)

		Expect(len(nropObj.Status.NodeGroups)).To(BeNumerically(">=", 1), "node groups not reported, nothing to do")
		// nrop places all daemonsets in the same namespace on which it resides, so any group is fine
		namespace = nropObj.Status.NodeGroups[0].DaemonSet.Namespace // shortcut
	})

	Context("testing metrics e2e from manager pod", func() {
		metricsPort := "8080"

		It("should be able to fetch metrics from the manager container", func() {
			managerPod, err := deploy.FindNUMAResourcesOperatorPod(context.TODO(), e2eclient.Client, nropObj)
			Expect(err).ToNot(HaveOccurred())
			stdout := fetchMetricsFromPod(managerPod, metricsAddress, metricsPort)
			Expect(strings.Contains(stdout, "workqueue_adds_total{controller=\"numaresourcesoperator\",name=\"numaresourcesoperator\"}")).To(BeTrue(), "workqueue_adds_total operator metric not found")
			Expect(strings.Contains(stdout, "workqueue_adds_total{controller=\"numaresourcesscheduler\",name=\"numaresourcesscheduler\"}")).To(BeTrue(), "workqueue_adds_total scheduler metric not found")
		})
	})

	Context("testing metrics e2e from rte worker pod", func() {
		metricsPort := "2112"

		It("should be able to fetch metrics from the worker pod container using kube-rbac-proxy", func() {
			labelSelector := fmt.Sprintf("name=%s",e2etestenv.RTELabelName)
			pods, err := e2eclient.K8sClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			Expect(err).To(BeNil(), "Error listing worker pods: %v", err)
			Expect(len(pods.Items)).To(BeNumerically(">=", 1), "There should be at least one worker pod")
			workerPod := &pods.Items[0]
			stdout := fetchMetricsFromPod(workerPod, metricsAddress, metricsPort)
			Expect(strings.Contains(stdout, "rte_noderesourcetopology_writes_total")).To(BeTrue(), "rte_noderesourcetopology_writes_total metric not found")
			Expect(strings.Contains(stdout, "rte_wakeup_delay_milliseconds")).To(BeTrue(), "rte_wakeup_delay_milliseconds metric not found")
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
