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
		minVersion     uint16
		tlsProfileSpec configv1.TLSProfileSpec
		allowedCiphers []string
	)

	BeforeEach(func(ctx context.Context) {
		var err error
		minVersion, tlsProfileSpec, err = intls.GetClusterTLSProfile(ctx, e2eclient.Client)
		Expect(err).ToNot(HaveOccurred(), "failed to read the cluster TLS profile")
		allowedCiphers = libgocrypto.OpenSSLToIANACipherSuites(tlsProfileSpec.Ciphers)
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
					rtePods = waitForReadyPods(ctx, resolveRTEPods)
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

		})
})

func waitForReadyPods(ctx context.Context, resolve func(context.Context) ([]corev1.Pod, error)) []corev1.Pod {
	GinkgoHelper()

	if _, err := resolve(ctx); errors.Is(err, errNotDeployed) {
		Skip(err.Error())
	}

	var ready []corev1.Pod
	Eventually(func() error {
		pods, err := resolve(ctx)
		if err != nil {
			return err
		}
		ready = ready[:0]
		for i := range pods {
			if pods[i].Status.Phase != corev1.PodRunning || pods[i].DeletionTimestamp != nil {
				continue
			}
			for _, cond := range pods[i].Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					ready = append(ready, pods[i])
					break
				}
			}
		}
		if len(ready) == 0 {
			return fmt.Errorf("no Running and Ready pod yet")
		}
		return nil
	}).WithTimeout(podDiscoveryTimeout).WithPolling(podDiscoveryPolling).Should(Succeed())
	return ready
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

func resolveRTEPods(ctx context.Context) ([]corev1.Pod, error) {
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

	var pods []corev1.Pod
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
		pods = append(pods, dsPods...)
	}
	return pods, nil
}
