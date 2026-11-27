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
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"
	ctrltls "github.com/openshift/controller-runtime-common/pkg/tls"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	objtls "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/tls"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/deploy"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
	intls "github.com/openshift-kni/numaresources-operator/test/internal/tls"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	schedulerSecurePort = "10259"
	rteMetricsPort      = "2112"
	operatorMetricsPort = "8080"

	podDiscoveryTimeout = 5 * time.Minute
	podDiscoveryPolling = 5 * time.Second
	tlsArgsCheckTimeout = 30 * time.Second
	tlsArgsCheckPolling = 5 * time.Second
)

var errNotDeployed = errors.New("operand is not deployed on this cluster")

var _ = Describe("TLS", func() {
	var (
		minVersion       uint16
		tlsProfileSpec   configv1.TLSProfileSpec
		allowedCiphers   []string
		allowedCurves    []tls.CurveID
		unrecognizedGrps []configv1.TLSGroup
	)

	BeforeEach(func(ctx context.Context) {
		var err error
		minVersion, tlsProfileSpec, err = intls.GetClusterTLSProfile(ctx, e2eclient.Client)
		Expect(err).ToNot(HaveOccurred(), "failed to read the cluster TLS profile")
		allowedCiphers = libgocrypto.OpenSSLToIANACipherSuites(tlsProfileSpec.Ciphers)
		allowedCurves, unrecognizedGrps = intls.CurveIDsFromProfile(tlsProfileSpec)
		// A group this build cannot map is dropped from the operand flags as well,
		// so the mapped subset stays a sound expectation; only log it here.
		if len(unrecognizedGrps) > 0 {
			klog.InfoS("the cluster profile lists TLS groups this build cannot map; curve assertions use the mapped subset",
				"groups", tlsProfileSpec.Groups, "unrecognized", unrecognizedGrps)
		}
	})

	Context("[tlscompliance][scheduler] scheduler complies with TLS Profile modifications",
		Label(label.Tier0, "feature:tlscompliance"), func() {
			var schedulerPods []corev1.Pod

			BeforeEach(func(ctx context.Context) {
				schedulerPods = waitForReadyPods(ctx, resolveSchedulerPods)
			})

			It("[test_id:90744] should accept TLS connections negotiated within the cluster profile", func() {
				for i := range schedulerPods {
					schedulerPod := &schedulerPods[i]
					e2efixture.By("probing the scheduler HTTPS endpoint on port %s", schedulerSecurePort)
					state, err := intls.ProbeTLSState(e2eclient.K8sClient, schedulerPod, schedulerSecurePort)
					Expect(err).ToNot(HaveOccurred(), "scheduler pod %q: TLS handshake failed", schedulerPod.Name)
					klog.InfoS("negotiated TLS settings", "pod", schedulerPod.Name,
						"version", tls.VersionName(state.Version), "cipher", tls.CipherSuiteName(state.CipherSuite),
						"curve", state.CurveID.String())

					Expect(state.Version).To(BeNumerically(">=", minVersion),
						"scheduler negotiated %s, below the cluster minimum %s",
						tls.VersionName(state.Version), tls.VersionName(minVersion))
					if state.Version < tls.VersionTLS13 {
						cipherName := tls.CipherSuiteName(state.CipherSuite)
						Expect(allowedCiphers).To(ContainElement(cipherName),
							"scheduler negotiated cipher %s, outside the profile ciphers %v", cipherName, tlsProfileSpec.Ciphers)
					}
				}
			})

			It("[test_id:90743] should reject TLS connections below the minimum profile version", func() {
				if minVersion == tls.VersionTLS10 {
					Skip("cluster minimum is TLS 1.0: there is no lower TLS version to probe with, so the version probe does not apply")
				}
				belowMinVersion, err := intls.TLSVersionBelow(minVersion)
				Expect(err).ToNot(HaveOccurred(), "cannot determine a TLS version below %s", tls.VersionName(minVersion))
				for i := range schedulerPods {
					schedulerPod := &schedulerPods[i]
					e2efixture.By("verifying the scheduler rejects TLS %s", tls.VersionName(belowMinVersion))
					err = intls.ProbeMaxTLSVersion(e2eclient.K8sClient, schedulerPod, schedulerSecurePort, belowMinVersion)
					Expect(err).To(HaveOccurred(), "scheduler accepted a connection capped at %s", tls.VersionName(belowMinVersion))
					Expect(errors.Is(err, intls.ErrTLSHandshakeRejected)).To(BeTrue(), "expected a TLS handshake rejection, got: %v", err)
				}
			})

			It("[test_id:91174] should reject a cipher the profile does not allow", func() {
				if minVersion == tls.VersionTLS13 {
					Skip(fmt.Sprintf("cluster minimum is %s: TLS 1.3 cipher suites are not individually configurable, so the cipher probe does not apply", tls.VersionName(minVersion)))
				}
				for i := range schedulerPods {
					schedulerPod := &schedulerPods[i]
					e2efixture.By("probing the scheduler to learn its certificate key type")
					state, err := intls.ProbeTLSState(e2eclient.K8sClient, schedulerPod, schedulerSecurePort)
					Expect(err).ToNot(HaveOccurred(), "scheduler pod %q: TLS handshake failed", schedulerPod.Name)
					keyType := intls.ServerCertificateKeyType(state)
					disallowedCipher := intls.FindDisallowedCipherForKey(tlsProfileSpec.Ciphers, keyType)
					if disallowedCipher == "" {
						Skip(fmt.Sprintf("no cipher excluded by profile %v can be selected by a %q certificate", tlsProfileSpec.Ciphers, keyType))
					}

					e2efixture.By("verifying the scheduler rejects cipher %s for a %q certificate", disallowedCipher, keyType)
					err = intls.ProbeTLSCipher(e2eclient.K8sClient, schedulerPod, schedulerSecurePort, disallowedCipher)
					Expect(err).To(HaveOccurred(), "%s pod %q accepted %s, which the profile does not allow", "scheduler", schedulerPod.Name, disallowedCipher)
					Expect(errors.Is(err, intls.ErrTLSHandshakeRejected)).To(BeTrue(),
						"%s pod %q: %s: expected a TLS handshake rejection, got: %v", "scheduler", schedulerPod.Name, disallowedCipher, err)
					logRejectionSide("scheduler", schedulerPod.Name, err)
				}
			})

			It("[test_id:91138] should negotiate a curve allowed by the cluster TLS profile", func() {
				if len(allowedCurves) == 0 {
					Skip(fmt.Sprintf("no recognized Go curves in %v (unrecognized: %v)", tlsProfileSpec.Groups, unrecognizedGrps))
				}
				for i := range schedulerPods {
					schedulerPod := &schedulerPods[i]
					e2efixture.By("reading the negotiated curve from the scheduler")
					state, err := intls.ProbeTLSState(e2eclient.K8sClient, schedulerPod, schedulerSecurePort)
					Expect(err).ToNot(HaveOccurred(), "scheduler pod %q: TLS handshake failed", schedulerPod.Name)
					klog.InfoS("negotiated TLS settings", "pod", schedulerPod.Name,
						"version", tls.VersionName(state.Version), "cipher", tls.CipherSuiteName(state.CipherSuite),
						"curve", state.CurveID.String())

					Expect(state.CurveID).ToNot(BeZero(),
						"scheduler negotiated no ECDHE curve; TLS 1.3 and the TLS 1.2 profiles in use always use (EC)DHE, so this is a probe or server defect")
					Expect(allowedCurves).To(ContainElement(state.CurveID),
						"scheduler negotiated curve %s (id %d), outside the profile groups %v; check --tls-curve-preferences on the scheduler Deployment",
						state.CurveID.String(), uint16(state.CurveID), tlsProfileSpec.Groups)
				}
			})

			It("[test_id:91139] should reject a handshake that offers only a curve disallowed by the cluster TLS profile", func() {
				if len(allowedCurves) == 0 {
					Skip(fmt.Sprintf("no recognized Go curves in %v (unrecognized: %v)", tlsProfileSpec.Groups, unrecognizedGrps))
				}
				disallowed, err := intls.FindDisallowedCurve(allowedCurves)
				if err != nil {
					Skip(fmt.Sprintf("no curve outside the profile groups is available to probe with; reason: %v, allowed: %v", err, allowedCurves))
				}
				for i := range schedulerPods {
					schedulerPod := &schedulerPods[i]
					e2efixture.By("probing the scheduler with curve %s", disallowed)
					_, err = intls.ProbeTLSCurve(e2eclient.K8sClient, schedulerPod, schedulerSecurePort, disallowed)
					Expect(err).To(HaveOccurred(), "scheduler accepted curve %s (id %d), which the profile groups %v do not allow",
						disallowed, uint16(disallowed), allowedCurves)
					Expect(errors.Is(err, intls.ErrTLSServerRejected)).To(BeTrue(),
						"curve %s: expected a server-side TLS alert, got: %v", disallowed, err)
					logRejectionSide("scheduler", schedulerPod.Name, err)
				}
			})

			It("[test_id:91140] should have the scheduler Deployment TLS args aligned with the cluster profile", func(ctx context.Context) {
				apply, unsupported := ctrltls.NewTLSConfigFromProfile(tlsProfileSpec)
				if len(unsupported) > 0 {
					klog.InfoS("the profile contains ciphers or groups this build does not support; they are ignored", "unsupported", unsupported)
				}
				cfg := &tls.Config{}
				apply(cfg)
				expected := objtls.NewSettings(cfg)
				e2efixture.By("verifying the scheduler Deployment TLS args: %s", expected.String())
				Eventually(func(g Gomega) {
					g.Expect(intls.CheckSchedulerDeploymentTLSFlags(ctx, e2eclient.Client, expected)).To(Succeed())
				}).WithTimeout(tlsArgsCheckTimeout).WithPolling(tlsArgsCheckPolling).Should(Succeed())
			})
		})

	Context("[tlscompliance][rte] rte complies with TLS Profile modifications",
		Label(label.Tier0, "feature:tlscompliance"), func() {
			It("[test_id:88380] should have RTE DaemonSet args aligned with the cluster TLS profile", func(ctx context.Context) {
				e2efixture.By("verifying the RTE DaemonSet TLS flags match the cluster TLS profile")
				Eventually(func() error {
					return intls.CheckRTEDaemonSetTLSFlagsMatchClusterProfile(ctx, e2eclient.Client)
				}).WithTimeout(tlsArgsCheckTimeout).WithPolling(tlsArgsCheckPolling).Should(Succeed())
			})

			Context("RTE metrics endpoint TLS compliance", func() {
				var rtePods []corev1.Pod

				BeforeEach(func(ctx context.Context) {
					rtePods = waitForReadyPodsByOwner(ctx, resolveRTEPods)
				})

				It("[test_id:88381] should serve metrics over TLS negotiated within the cluster profile", func() {
					for i := range rtePods {
						rtePod := &rtePods[i]
						e2efixture.By("probing RTE pod %s/%s on port %s", rtePod.Namespace, rtePod.Name, rteMetricsPort)
						state, err := intls.ProbeTLSState(e2eclient.K8sClient, rtePod, rteMetricsPort)
						Expect(err).ToNot(HaveOccurred(), "RTE pod %q: TLS handshake failed", rtePod.Name)
						klog.InfoS("negotiated TLS settings", "pod", rtePod.Name,
							"version", tls.VersionName(state.Version), "cipher", tls.CipherSuiteName(state.CipherSuite),
							"curve", state.CurveID.String())

						Expect(state.Version).To(BeNumerically(">=", minVersion),
							"RTE pod %q negotiated %s, below the cluster minimum %s",
							rtePod.Name, tls.VersionName(state.Version), tls.VersionName(minVersion))
						if state.Version < tls.VersionTLS13 {
							cipherName := tls.CipherSuiteName(state.CipherSuite)
							Expect(allowedCiphers).To(ContainElement(cipherName),
								"RTE pod %q negotiated cipher %s, outside the profile ciphers %v", rtePod.Name, cipherName, tlsProfileSpec.Ciphers)
						}

						e2efixture.By("fetching /metrics over TLS from RTE pod %s/%s", rtePod.Namespace, rtePod.Name)
						body, err := intls.FetchMetrics(e2eclient.K8sClient, rtePod, rteMetricsPort)
						Expect(err).ToNot(HaveOccurred(), "RTE pod %q: failed to fetch metrics", rtePod.Name)
						Expect(body).ToNot(BeEmpty(), "RTE pod %q: empty metrics response", rtePod.Name)
						Expect(string(body)).To(ContainSubstring("rte_noderesourcetopology_writes_total"),
							"RTE pod %q: metrics body does not look like RTE metrics", rtePod.Name)
					}
				})

				It("[test_id:88382] should reject TLS connections below the minimum profile version", func() {
					if minVersion == tls.VersionTLS10 {
						Skip("cluster minimum is TLS 1.0: there is no lower TLS version to probe with, so the version probe does not apply")
					}
					belowMinVersion, err := intls.TLSVersionBelow(minVersion)
					Expect(err).ToNot(HaveOccurred(), "cannot determine a TLS version below %s", tls.VersionName(minVersion))
					for i := range rtePods {
						rtePod := &rtePods[i]
						e2efixture.By("verifying RTE pod %s/%s rejects TLS %s", rtePod.Namespace, rtePod.Name, tls.VersionName(belowMinVersion))
						err := intls.ProbeMaxTLSVersion(e2eclient.K8sClient, rtePod, rteMetricsPort, belowMinVersion)
						Expect(err).To(HaveOccurred(), "RTE pod %q accepted a connection capped at %s", rtePod.Name, tls.VersionName(belowMinVersion))
						Expect(errors.Is(err, intls.ErrTLSHandshakeRejected)).To(BeTrue(),
							"RTE pod %q: expected a TLS handshake rejection, got: %v", rtePod.Name, err)
					}
				})

				It("[test_id:91175] should reject a cipher the profile does not allow", func() {
					if minVersion == tls.VersionTLS13 {
						Skip(fmt.Sprintf("cluster minimum is %s: TLS 1.3 cipher suites are not individually configurable, so the cipher probe does not apply", tls.VersionName(minVersion)))
					}
					probed := 0
					for i := range rtePods {
						rtePod := &rtePods[i]
						e2efixture.By("probing RTE pod %s/%s to learn its certificate key type", rtePod.Namespace, rtePod.Name)
						state, err := intls.ProbeTLSState(e2eclient.K8sClient, rtePod, rteMetricsPort)
						Expect(err).ToNot(HaveOccurred(), "RTE pod %q: TLS handshake failed", rtePod.Name)
						keyType := intls.ServerCertificateKeyType(state)
						disallowedCipher := intls.FindDisallowedCipherForKey(tlsProfileSpec.Ciphers, keyType)
						if disallowedCipher == "" {
							klog.InfoS("no cipher excluded by the profile can be selected by this pod's certificate",
								"pod", rtePod.Name, "keyType", keyType, "profileCiphers", tlsProfileSpec.Ciphers)
							continue
						}
						probed++

						e2efixture.By("verifying RTE pod %s/%s rejects cipher %s for a %q certificate", rtePod.Namespace, rtePod.Name, disallowedCipher, keyType)
						err = intls.ProbeTLSCipher(e2eclient.K8sClient, rtePod, rteMetricsPort, disallowedCipher)
						Expect(err).To(HaveOccurred(), "RTE pod %q accepted cipher %s, which the profile does not allow", rtePod.Name, disallowedCipher)
						Expect(errors.Is(err, intls.ErrTLSHandshakeRejected)).To(BeTrue(),
							"RTE pod %q, cipher %s: expected a TLS handshake rejection, got: %v", rtePod.Name, disallowedCipher, err)
						logRejectionSide("rte", rtePod.Name, err)
					}
					if probed == 0 {
						Skip(fmt.Sprintf("no cipher excluded by profile %v can be selected by any RTE certificate", tlsProfileSpec.Ciphers))
					}
				})

				It("[test_id:91141] should negotiate a curve allowed by the cluster profile on every RTE pod", func() {
					if len(allowedCurves) == 0 {
						Skip(fmt.Sprintf("no recognized Go curves in %v (unrecognized: %v)", tlsProfileSpec.Groups, unrecognizedGrps))
					}

					for i := range rtePods {
						rtePod := &rtePods[i]
						e2efixture.By("reading the negotiated curve from RTE pod %s/%s", rtePod.Namespace, rtePod.Name)
						state, err := intls.ProbeTLSState(e2eclient.K8sClient, rtePod, rteMetricsPort)
						Expect(err).ToNot(HaveOccurred(), "RTE pod %q: TLS handshake failed", rtePod.Name)
						klog.InfoS("negotiated TLS settings", "pod", rtePod.Name,
							"version", tls.VersionName(state.Version), "cipher", tls.CipherSuiteName(state.CipherSuite),
							"curve", state.CurveID.String())

						Expect(state.CurveID).ToNot(BeZero(),
							"RTE pod %q negotiated no ECDHE curve; TLS 1.3 and the TLS 1.2 profiles in use always use (EC)DHE, so this is a probe or server defect", rtePod.Name)
						Expect(allowedCurves).To(ContainElement(state.CurveID),
							"RTE pod %q negotiated curve %s (id %d), outside the profile groups %v; check --metrics-tls-curve-preferences on the RTE DaemonSet",
							rtePod.Name, state.CurveID.String(), uint16(state.CurveID), tlsProfileSpec.Groups)
					}
				})

				It("[test_id:91142] should reject a curve disallowed by the cluster profile on every RTE pod", func(ctx context.Context) {
					if len(allowedCurves) == 0 {
						Skip(fmt.Sprintf("no recognized Go curves in %v (unrecognized: %v)", tlsProfileSpec.Groups, unrecognizedGrps))
					}
					disallowed, err := intls.FindDisallowedCurve(allowedCurves)
					if err != nil {
						Skip(fmt.Sprintf("no curve outside the profile groups is available to probe with; reason: %v, allowed: %v", err, allowedCurves))
					}

					for i := range rtePods {
						rtePod := &rtePods[i]
						e2efixture.By("probing RTE pod %s/%s with curve %s", rtePod.Namespace, rtePod.Name, disallowed)
						_, err := intls.ProbeTLSCurve(e2eclient.K8sClient, rtePod, rteMetricsPort, disallowed)
						Expect(err).To(HaveOccurred(), "RTE pod %q accepted curve %s (id %d), which the profile groups %v do not allow",
							rtePod.Name, disallowed, uint16(disallowed), allowedCurves)
						Expect(errors.Is(err, intls.ErrTLSServerRejected)).To(BeTrue(),
							"RTE pod %q curve %s: expected a server-side TLS alert, got: %v", rtePod.Name, disallowed, err)
						logRejectionSide("RTE", rtePod.Name, err)
					}
				})
			})
		})

	Context("[tlscompliance][operator] operator metrics complies with TLS Profile modifications",
		Label(label.Tier0, "feature:tlscompliance"), func() {
			var operatorPod corev1.Pod

			BeforeEach(func(ctx context.Context) {
				operatorPod = waitForReadyPods(ctx, resolveOperatorPods)[0]
			})

			It("[test_id:91143] should accept TLS connections negotiated within the cluster profile", func() {
				e2efixture.By("probing the operator metrics endpoint on port %s", operatorMetricsPort)
				state, err := intls.ProbeTLSState(e2eclient.K8sClient, &operatorPod, operatorMetricsPort)
				Expect(err).ToNot(HaveOccurred(), "operator pod %q: TLS handshake failed", operatorPod.Name)
				klog.InfoS("negotiated TLS settings", "pod", operatorPod.Name,
					"version", tls.VersionName(state.Version), "cipher", tls.CipherSuiteName(state.CipherSuite),
					"curve", state.CurveID.String())

				Expect(state.Version).To(BeNumerically(">=", minVersion),
					"operator negotiated %s, below the cluster minimum %s",
					tls.VersionName(state.Version), tls.VersionName(minVersion))
				if state.Version < tls.VersionTLS13 {
					cipherName := tls.CipherSuiteName(state.CipherSuite)
					Expect(allowedCiphers).To(ContainElement(cipherName),
						"operator negotiated cipher %s, outside the profile ciphers %v", cipherName, tlsProfileSpec.Ciphers)
				}
			})

			It("[test_id:91144] should reject TLS connections below the minimum profile version", func() {
				if minVersion == tls.VersionTLS10 {
					Skip("cluster minimum is TLS 1.0: there is no lower TLS version to probe with, so the version probe does not apply")
				}
				belowMinVersion, err := intls.TLSVersionBelow(minVersion)
				Expect(err).ToNot(HaveOccurred(), "cannot determine a TLS version below %s", tls.VersionName(minVersion))

				e2efixture.By("verifying the operator rejects TLS %s", tls.VersionName(belowMinVersion))
				err = intls.ProbeMaxTLSVersion(e2eclient.K8sClient, &operatorPod, operatorMetricsPort, belowMinVersion)
				Expect(err).To(HaveOccurred(), "operator accepted a connection capped at %s", tls.VersionName(belowMinVersion))
				Expect(errors.Is(err, intls.ErrTLSHandshakeRejected)).To(BeTrue(), "expected a TLS handshake rejection, got: %v", err)
			})

			It("[test_id:91176] should reject a cipher the profile does not allow", func() {
				if minVersion == tls.VersionTLS13 {
					Skip(fmt.Sprintf("cluster minimum is %s: TLS 1.3 cipher suites are not individually configurable, so the cipher probe does not apply", tls.VersionName(minVersion)))
				}
				e2efixture.By("probing the operator to learn its certificate key type")
				state, err := intls.ProbeTLSState(e2eclient.K8sClient, &operatorPod, operatorMetricsPort)
				Expect(err).ToNot(HaveOccurred(), "operator pod %q: TLS handshake failed", operatorPod.Name)
				keyType := intls.ServerCertificateKeyType(state)
				disallowedCipher := intls.FindDisallowedCipherForKey(tlsProfileSpec.Ciphers, keyType)
				if disallowedCipher == "" {
					Skip(fmt.Sprintf("no cipher excluded by profile %v can be selected by a %q certificate", tlsProfileSpec.Ciphers, keyType))
				}

				e2efixture.By("verifying the operator rejects cipher %s for a %q certificate", disallowedCipher, keyType)
				err = intls.ProbeTLSCipher(e2eclient.K8sClient, &operatorPod, operatorMetricsPort, disallowedCipher)
				Expect(err).To(HaveOccurred(), "operator accepted cipher %s, which the profile does not allow", disallowedCipher)
				Expect(errors.Is(err, intls.ErrTLSHandshakeRejected)).To(BeTrue(),
					"cipher %s: expected a TLS handshake rejection, got: %v", disallowedCipher, err)
				logRejectionSide("operator", operatorPod.Name, err)
			})

			It("[test_id:91145] should negotiate a curve allowed by the cluster TLS profile", func() {
				if len(allowedCurves) == 0 {
					Skip(fmt.Sprintf("no recognized Go curves in %v (unrecognized: %v)", tlsProfileSpec.Groups, unrecognizedGrps))
				}

				e2efixture.By("reading the negotiated curve from the operator")
				state, err := intls.ProbeTLSState(e2eclient.K8sClient, &operatorPod, operatorMetricsPort)
				Expect(err).ToNot(HaveOccurred(), "operator pod %q: TLS handshake failed", operatorPod.Name)
				klog.InfoS("negotiated TLS settings", "pod", operatorPod.Name,
					"version", tls.VersionName(state.Version), "cipher", tls.CipherSuiteName(state.CipherSuite),
					"curve", state.CurveID.String())

				Expect(state.CurveID).ToNot(BeZero(),
					"operator negotiated no ECDHE curve; TLS 1.3 and the TLS 1.2 profiles in use always use (EC)DHE, so this is a probe or server defect")
				Expect(allowedCurves).To(ContainElement(state.CurveID),
					"operator negotiated curve %s (id %d), outside the profile groups %v; the operator applies the cluster groups at startup",
					state.CurveID.String(), uint16(state.CurveID), tlsProfileSpec.Groups)
			})

			It("[test_id:91146] should reject a handshake that offers only a curve disallowed by the cluster TLS profile", func() {
				if len(allowedCurves) == 0 {
					Skip(fmt.Sprintf("no recognized Go curves in %v (unrecognized: %v)", tlsProfileSpec.Groups, unrecognizedGrps))
				}
				disallowed, err := intls.FindDisallowedCurve(allowedCurves)
				if err != nil {
					Skip(fmt.Sprintf("no curve outside the profile groups is available to probe with; reason: %v, allowed: %v", err, allowedCurves))
				}

				e2efixture.By("probing the operator with curve %s", disallowed)
				_, err = intls.ProbeTLSCurve(e2eclient.K8sClient, &operatorPod, operatorMetricsPort, disallowed)
				Expect(err).To(HaveOccurred(), "operator accepted curve %s (id %d), which the profile groups %v do not allow",
					disallowed, uint16(disallowed), allowedCurves)
				Expect(errors.Is(err, intls.ErrTLSServerRejected)).To(BeTrue(),
					"curve %s: expected a server-side TLS alert, got: %v", disallowed, err)
				logRejectionSide("operator", operatorPod.Name, err)
			})
		})
})

// waitForReadyPods waits for at least one Running and Ready pod for a single
// owner. It skips when the operand is not deployed at all.
func waitForReadyPods(ctx context.Context, resolve func(context.Context) ([]corev1.Pod, error)) []corev1.Pod {
	GinkgoHelper()
	return waitForReadyPodsByOwner(ctx, func(ctx context.Context) (map[string][]corev1.Pod, error) {
		pods, err := resolve(ctx)
		if err != nil {
			return nil, err
		}
		return map[string][]corev1.Pod{"operand": pods}, nil
	})
}

// waitForReadyPodsByOwner waits until every owner listed by resolve has at least
// one Running and Ready pod. Requiring one ready pod per owner is what keeps a
// node group whose DaemonSet has no pods yet from being silently dropped from the
// returned list.
func waitForReadyPodsByOwner(ctx context.Context, resolve func(context.Context) (map[string][]corev1.Pod, error)) []corev1.Pod {
	GinkgoHelper()

	if _, err := resolve(ctx); errors.Is(err, errNotDeployed) {
		Skip(err.Error())
	}

	var ready []corev1.Pod
	Eventually(func() error {
		podsByOwner, err := resolve(ctx)
		if err != nil {
			return err
		}
		ready = ready[:0]
		for owner, pods := range podsByOwner {
			ownerReady := 0
			for i := range pods {
				if !podIsReady(&pods[i]) {
					continue
				}
				ready = append(ready, pods[i])
				ownerReady++
			}
			if ownerReady == 0 {
				return fmt.Errorf("%s: no Running and Ready pod yet", owner)
			}
		}
		sort.Slice(ready, func(i, j int) bool { return ready[i].Name < ready[j].Name })
		return nil
	}).WithTimeout(podDiscoveryTimeout).WithPolling(podDiscoveryPolling).Should(Succeed())
	return ready
}

func podIsReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning || pod.DeletionTimestamp != nil {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func resolveSchedulerPods(ctx context.Context) ([]corev1.Pod, error) {
	nroSched := &nropv1.NUMAResourcesScheduler{}
	if err := e2eclient.Client.Get(ctx, objects.NROSchedObjectKey(), nroSched); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("NUMAResourcesScheduler: %w", errNotDeployed)
		}
		return nil, err
	}
	deployment, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(ctx, nroSched.GetUID())
	if err != nil {
		return nil, err
	}
	if deployment == nil {
		return nil, errors.New("scheduler Deployment has not been created yet")
	}
	return podlist.With(e2eclient.Client).ByDeployment(ctx, *deployment)
}

func resolveOperatorPods(ctx context.Context) ([]corev1.Pod, error) {
	nropObj := &nropv1.NUMAResourcesOperator{}
	if err := e2eclient.Client.Get(ctx, objects.NROObjectKey(), nropObj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("NUMAResourcesOperator: %w", errNotDeployed)
		}
		return nil, err
	}
	pod, err := deploy.FindNUMAResourcesOperatorPod(ctx, e2eclient.Client, nropObj)
	if err != nil {
		return nil, err
	}
	return []corev1.Pod{*pod}, nil
}

func resolveRTEPods(ctx context.Context) (map[string][]corev1.Pod, error) {
	nropObj := &nropv1.NUMAResourcesOperator{}
	if err := e2eclient.Client.Get(ctx, objects.NROObjectKey(), nropObj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("NUMAResourcesOperator: %w", errNotDeployed)
		}
		return nil, err
	}
	if len(nropObj.Status.NodeGroups) == 0 {
		return nil, errors.New("NUMAResourcesOperator reports no node groups yet")
	}
	podsByDaemonSet := make(map[string][]corev1.Pod, len(nropObj.Status.NodeGroups))
	for _, nodeGroup := range nropObj.Status.NodeGroups {
		dsKey := client.ObjectKey{Namespace: nodeGroup.DaemonSet.Namespace, Name: nodeGroup.DaemonSet.Name}
		ds := &appsv1.DaemonSet{}
		if err := e2eclient.Client.Get(ctx, dsKey, ds); err != nil {
			return nil, err
		}
		dsPods, err := podlist.With(e2eclient.Client).ByDaemonset(ctx, *ds)
		if err != nil {
			return nil, err
		}
		podsByDaemonSet[dsKey.String()] = dsPods
	}
	return podsByDaemonSet, nil
}

func logRejectionSide(component, podName string, err error) {
	switch {
	case errors.Is(err, intls.ErrTLSServerRejected):
		klog.InfoS("the endpoint rejected the probe with a TLS alert", "component", component, "pod", podName)
	case errors.Is(err, intls.ErrTLSClientConfigIncompatible):
		klog.InfoS("the Go client refused the handshake before the server answered; the endpoint did not accept the probe, but this is not server-side proof",
			"component", component, "pod", podName)
	default:
		klog.InfoS("probe failed without a TLS classification", "component", component, "pod", podName, "err", err)
	}
}
