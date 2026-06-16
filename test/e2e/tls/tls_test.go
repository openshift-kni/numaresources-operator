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

package tls

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
	intls "github.com/openshift-kni/numaresources-operator/test/internal/tls"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const schedulerSecurePort = "10259"

var _ = Describe("TLS", func() {
	It("should reject TLS connections that are not compatible with the profile - negative test", func(ctx context.Context) {
		By("getting the current OCP TLS profile")
		minVersion, tlsProfileSpec, err := intls.GetClusterTLSProfile(ctx, e2eclient.Client)
		Expect(err).ToNot(HaveOccurred(), "unable to get TLS profile from APIServer")
		klog.InfoS("current TLS minimum version", "version", libgocrypto.TLSVersionToNameOrDie(minVersion))

		belowMinVersion, err := intls.TLSVersionBelow(minVersion)
		Expect(err).ToNot(HaveOccurred(), "failed to get TLS version below %s", libgocrypto.TLSVersionToNameOrDie(minVersion))

		By("getting the scheduler deployment and pods")
		nroSchedObj := &nropv1.NUMAResourcesScheduler{}
		nroSchedKey := objects.NROSchedObjectKey()
		Expect(e2eclient.Client.Get(ctx, nroSchedKey, nroSchedObj)).To(Succeed(), "failed to get %q in the cluster", nroSchedKey.String())

		deployment, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(ctx, nroSchedObj.GetUID())
		Expect(err).ToNot(HaveOccurred(), "failed to get the deployment")
		Expect(deployment).ToNot(BeNil(), "scheduler deployment not found")

		pods, err := podlist.With(e2eclient.Client).ByDeployment(ctx, *deployment)
		Expect(err).ToNot(HaveOccurred(), "failed to get the pods")
		Expect(pods).ToNot(BeEmpty(), "no pods found for the deployment")

		schedulerPod := &pods[0]

		By(fmt.Sprintf("verifying that TLS connections at version %s are rejected by the server", tls.VersionName(belowMinVersion)))
		err = intls.ProbeMaxTLSVersion(e2eclient.K8sClient, schedulerPod, schedulerSecurePort, belowMinVersion)
		Expect(err).To(HaveOccurred(), "scheduler server should reject TLS connections capped at %s", tls.VersionName(belowMinVersion))
		Expect(errors.Is(err, intls.ErrTLSHandshakeRejected)).To(BeTrue(),
			"expected TLS handshake rejection, got: %v", err)
		if minVersion == tls.VersionTLS13 {
			By("skipping disallowed-cipher probe: TLS 1.3 ciphers are not individually configurable")
			return
		}

		disallowedCipher := intls.FindDisallowedCipher(tlsProfileSpec.Ciphers)
		if disallowedCipher == "" {
			Skip("all known TLS 1.2 ciphers are in the allowed set, nothing to test")
		}
		By(fmt.Sprintf("verifying that TLS connections with disallowed cipher %s are rejected by pod %s", disallowedCipher, schedulerPod.Name))
		err = intls.ProbeTLSCipher(e2eclient.K8sClient, schedulerPod, schedulerSecurePort, disallowedCipher)
		Expect(err).To(HaveOccurred(), "scheduler server should reject connections with disallowed cipher %s", disallowedCipher)
		Expect(errors.Is(err, intls.ErrTLSHandshakeRejected)).To(BeTrue(), "expected TLS handshake rejection for cipher %s, got: %v", disallowedCipher, err)
	})

	It("should adhere to openshift TLS profile - positive test", func(ctx context.Context) {
		By("getting the current OCP TLS profile")
		minVersion, tlsProfileSpec, err := intls.GetClusterTLSProfile(ctx, e2eclient.Client)
		Expect(err).ToNot(HaveOccurred(), "unable to get TLS profile from APIServer")
		klog.InfoS("current TLS minimum version", "version", libgocrypto.TLSVersionToNameOrDie(minVersion))

		By("getting the scheduler deployment and pods")
		nroSchedObj := &nropv1.NUMAResourcesScheduler{}
		nroSchedKey := objects.NROSchedObjectKey()
		Expect(e2eclient.Client.Get(ctx, nroSchedKey, nroSchedObj)).To(Succeed(), "failed to get %q in the cluster", nroSchedKey.String())

		deployment, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(ctx, nroSchedObj.GetUID())
		Expect(err).ToNot(HaveOccurred(), "failed to get the deployment")
		Expect(deployment).ToNot(BeNil(), "scheduler deployment not found")

		pods, err := podlist.With(e2eclient.Client).ByDeployment(ctx, *deployment)
		Expect(err).ToNot(HaveOccurred(), "failed to get the pods")
		Expect(pods).ToNot(BeEmpty(), "no pods found for the deployment")

		schedulerPod := &pods[0]

		By("probing the scheduler HTTPS endpoint to verify TLS connection is accepted")
		gotVersion, gotCipherID, err := intls.ProbeTLSSettings(e2eclient.K8sClient, schedulerPod, schedulerSecurePort)
		Expect(err).ToNot(HaveOccurred(), "failed to probe TLS settings on pod %q", schedulerPod.Name)

		gotVersionName := tls.VersionName(gotVersion)
		gotCipherName := tls.CipherSuiteName(gotCipherID)
		klog.InfoS("negotiated TLS settings", "version", gotVersionName, "cipher", gotCipherName)

		Expect(gotVersion).To(BeNumerically(">=", minVersion), "negotiated TLS version %s is below the expected minimum %s", gotVersionName, libgocrypto.TLSVersionToNameOrDie(minVersion))

		// TLS 1.3 cipher suites are not configurable and won't appear in
		// the profile's list; only validate for TLS 1.2 and below.
		allowedCipherNames := libgocrypto.OpenSSLToIANACipherSuites(tlsProfileSpec.Ciphers)
		if gotVersion < tls.VersionTLS13 {
			Expect(gotCipherName).ToNot(BeEmpty(), "could not resolve negotiated cipher suite ID 0x%04x", gotCipherID)
			Expect(allowedCipherNames).To(ContainElement(gotCipherName), "negotiated cipher %s is not in the allowed set %v", gotCipherName, tlsProfileSpec.Ciphers)
		}
	})

	Context("[tlscompliance][rte] rte complies with TLS Profile modifications", Label(label.Tier0, "feature:tlscompliance"), func() {
		const (
			rteDaemonSetCheckTimeout  = 30 * time.Second
			rteDaemonSetCheckInterval = 5 * time.Second
		)
		It("[test_id:88380] should have RTE DaemonSet args aligned with the cluster TLS profile", func(ctx context.Context) {
			By("Verifying RTE DaemonSet TLS flags match the cluster TLS profile")
			Eventually(func() error {
				return intls.CheckRTEDaemonSetTLSFlagsMatchClusterProfile(ctx, e2eclient.Client)
			}).WithTimeout(rteDaemonSetCheckTimeout).WithPolling(rteDaemonSetCheckInterval).Should(Succeed())
		})

		Context("RTE Metrics end point TLS Compliance", Ordered, func() {
			const rteMetricsPort = "2112"
			var (
				minVersion       uint16
				profileSpec      configv1.TLSProfileSpec
				rtePods          []corev1.Pod
				disallowedCipher string
			)
			BeforeAll(func(ctx context.Context) {
				var err error
				minVersion, profileSpec, err = intls.GetClusterTLSProfile(ctx, e2eclient.Client)
				Expect(err).ToNot(HaveOccurred())
				rtePods = getRTEPodsFromNRO(ctx)
				disallowedCipher = intls.FindDisallowedCipher(profileSpec.Ciphers)
				if disallowedCipher != "" {
					e2efixture.By("selected disallowed cipher for negative tests: %s", disallowedCipher)
				}
			})

			It("[test_id:88381] should service metrics over TLS adhering to cluster profile", func(ctx context.Context) {
				for _, rtePod := range rtePods {
					By("Verifying TLS Handshake succeeds and negotiated version meets the cluster profile")
					// fetch TLS Version and the cipher supported by TLS version
					gotVersion, gotCipherId, err := intls.ProbeTLSSettings(e2eclient.K8sClient, &rtePod, rteMetricsPort)

					Expect(err).ToNot(HaveOccurred(), "TLS Handshake failed on pod %q", rtePod.Name)
					klog.InfoS("negotiated TLS settings",
						"version", tls.VersionName(gotVersion),
						"cipher", tls.CipherSuiteName(gotCipherId))

					Expect(gotVersion).To(BeNumerically(">=", minVersion),
						"negotiated version %s below minimum %s",
						tls.VersionName(gotVersion), tls.VersionName(minVersion))

					By("Fetching /metrics over TLS via port-fortward")
					body, err := intls.FetchMetrics(e2eclient.K8sClient, &rtePod, rteMetricsPort)
					Expect(err).ToNot(HaveOccurred(), "failed to fetch metrics from pod %q", rtePod.Name)

					Expect(body).ToNot(BeEmpty(), "metrics response body should not be empty")
					Expect(string(body)).To(ContainSubstring("rte_noderesourcetopology_writes_total"))
				}
			})

			It("[test_id:88382] should reject TLS connections incompatible with the cluster profile", func() {
				belowMinVersion, err := intls.TLSVersionBelow(minVersion)
				Expect(err).ToNot(HaveOccurred(), "cannot determine version below %s", tls.VersionName(minVersion))
				e2efixture.By("verifying TLS %s is rejected on each RTE pod", tls.VersionName(belowMinVersion))
				for _, rtePod := range rtePods {
					err = intls.ProbeMaxTLSVersion(e2eclient.K8sClient, &rtePod, rteMetricsPort, belowMinVersion)
					Expect(err).To(HaveOccurred(), "pod %q should reject TLS %s", rtePod.Name, tls.VersionName(belowMinVersion))
					Expect(errors.Is(err, intls.ErrTLSHandshakeRejected)).To(BeTrue(), "pod %q: got: %v", rtePod.Name, err)
				}
				if minVersion == tls.VersionTLS13 {
					Skip("TLS 1.3 ciphers are not individually configurable, skipping cipher rejection test")
				}

				if disallowedCipher == "" {
					Skip("all known TLS 1.2 ciphers are in the allowed set, nothing to test")
				}
				e2efixture.By("verifying disallowed cipher %s is rejected on each RTE pod", disallowedCipher)
				for _, rtePod := range rtePods {
					e2efixture.By("probing pod %s/%s with disallowed cipher %s", rtePod.Namespace, rtePod.Name, disallowedCipher)
					err = intls.ProbeTLSCipher(e2eclient.K8sClient, &rtePod, rteMetricsPort, disallowedCipher)
					Expect(err).To(HaveOccurred(), "pod %q should reject cipher %s", rtePod.Name, disallowedCipher)
					Expect(errors.Is(err, intls.ErrTLSHandshakeRejected)).To(BeTrue(),
						"pod %q cipher %s: expected TLS handshake rejection, got: %v", rtePod.Name, disallowedCipher, err)
					klog.InfoS("disallowed cipher rejected as expected", "pod", rtePod.Name, "cipher", disallowedCipher)
				}
			})
		})
	})
})

func getRTEPodsFromNRO(ctx context.Context) []corev1.Pod {
	GinkgoHelper()
	nropObj := &nropv1.NUMAResourcesOperator{}
	nropKey := client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}
	Expect(e2eclient.Client.Get(ctx, nropKey, nropObj)).To(Succeed(), "failed to get NRO %q in the cluster", nropKey.Name)
	Expect(nropObj.Status.NodeGroups).ToNot(BeEmpty(), "NRO %q must have at least one NodeGroup", nropKey.Name)
	var rtePods []corev1.Pod
	for _, nodeGroup := range nropObj.Status.NodeGroups {
		daemonSet := &appsv1.DaemonSet{}
		dsKey := client.ObjectKey{
			Namespace: nodeGroup.DaemonSet.Namespace,
			Name:      nodeGroup.DaemonSet.Name,
		}
		Expect(e2eclient.Client.Get(ctx, dsKey, daemonSet)).To(Succeed(), "failed to get RTE DaemonSet %s/%s", dsKey.Namespace, dsKey.Name)
		pods, err := podlist.With(e2eclient.Client).ByDaemonset(ctx, *daemonSet)
		Expect(err).ToNot(HaveOccurred(), "failed to list pods for RTE DaemonSet %s/%s", dsKey.Namespace, dsKey.Name)
		Expect(pods).ToNot(BeEmpty(), "expected at least one RTE pod from Daemonset %s/%s", dsKey.Namespace, dsKey.Name)
		rtePods = append(rtePods, pods...)
	}
	Expect(rtePods).ToNot(BeEmpty(), "expected at least one RTE Pod from NRO managed Daemonsets")
	return rtePods
}
