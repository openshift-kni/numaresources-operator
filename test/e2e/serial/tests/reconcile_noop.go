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

package tests

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/deploy"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	e2eNoopReconcileAnnotation = "numaresources.openshift.io/e2e-noop-reconcile"
	managerMetricsPort         = "8080"
	managerMetricsAddr         = "127.0.0.1"
)

// parseApplyClientUpdatesScalar returns the value of numaresources_operator_apply_client_updates_total.
func parseApplyClientUpdatesScalar(metricsText string) (float64, error) {
	metricsText = strings.ReplaceAll(metricsText, "\r", "")
	name := regexp.QuoteMeta(apply.MetricApplyClientUpdatesTotal)
	// Line-anchored metric sample: NAME optional{label="pairs"} WS VALUE
	//   (?m)^          start of a line (multiline)
	//   (?:\{[^}]*\})? optional label set for this counter (empty or any labels)
	//   \s+            whitespace before the numeric sample
	//   ([0-9.eE+-]+)  counter value, including scientific notation
	re := regexp.MustCompile(`(?m)^` + name + `(?:\{[^}]*\})?\s+([0-9.eE+-]+)`)
	m := re.FindStringSubmatch(metricsText)
	if len(m) < 2 {
		return 0, fmt.Errorf("metric %s not found in metrics output", apply.MetricApplyClientUpdatesTotal)
	}
	return strconv.ParseFloat(m[1], 64)
}

func fetchManagerMetricsFromPod(ctx context.Context, pod *corev1.Pod) string {
	GinkgoHelper()
	endpoint := net.JoinHostPort(managerMetricsAddr, managerMetricsPort)
	key := client.ObjectKeyFromObject(pod)
	cmd := []string{
		"/bin/curl", "-k",
		fmt.Sprintf("https://%s/metrics", endpoint),
	}
	klog.V(2).InfoS("executing command", "args", cmd, "pod", key.String())
	stdout, stderr, err := remoteexec.CommandOnPod(ctx, e2eclient.K8sClient, pod, cmd...)
	Expect(err).ToNot(HaveOccurred(), "exec curl on pod=%q: %v; stderr=%q", key.String(), err, stderr)
	Expect(stdout).NotTo(BeEmpty())
	return string(stdout)
}

var _ = Describe("[serial] numaresources reconcile no-op updates", Serial, Label(label.Tier2, "feature:nonreg"), func() {
	var nropKey client.ObjectKey

	BeforeEach(func() {
		Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create kubernetes client")
		nropKey = objects.NROObjectKey()
	})

	It("should not call apply Update() for operands when only NUMAResourcesOperator metadata changes", func(ctx context.Context) {
		nro := &nropv1.NUMAResourcesOperator{}
		Expect(e2eclient.Client.Get(ctx, nropKey, nro)).To(Succeed(), "get NUMAResourcesOperator")

		if len(nro.Status.DaemonSets) == 0 {
			Skip("no RTE DaemonSets in status yet")
		}

		managerPod, err := deploy.FindNUMAResourcesOperatorPod(ctx, e2eclient.Client, nro)
		Expect(err).ToNot(HaveOccurred())

		beforeText := fetchManagerMetricsFromPod(ctx, managerPod)
		beforeTotal, err := parseApplyClientUpdatesScalar(beforeText)
		Expect(err).ToNot(HaveOccurred())

		baseForPatch := nro.DeepCopy()
		if nro.Annotations == nil {
			nro.Annotations = map[string]string{}
		}
		nro.Annotations[e2eNoopReconcileAnnotation] = fmt.Sprintf("%d", time.Now().UnixNano())
		Expect(e2eclient.Client.Patch(ctx, nro, client.MergeFrom(baseForPatch))).To(Succeed(), "patch NRO annotation")

		defer func() {
			cleanup := &nropv1.NUMAResourcesOperator{}
			if err := e2eclient.Client.Get(ctx, nropKey, cleanup); err != nil {
				return
			}
			base := cleanup.DeepCopy()
			if cleanup.Annotations == nil {
				return
			}
			delete(cleanup.Annotations, e2eNoopReconcileAnnotation)
			if len(cleanup.Annotations) == 0 {
				cleanup.Annotations = nil
			}
			_ = e2eclient.Client.Patch(ctx, cleanup, client.MergeFrom(base))
		}()

		Consistently(func() error {
			afterText := fetchManagerMetricsFromPod(ctx, managerPod)
			afterTotal, err := parseApplyClientUpdatesScalar(afterText)
			if err != nil {
				return err
			}
			delta := afterTotal - beforeTotal
			if delta > 0 {
				return fmt.Errorf("%s increased during metadata-only reconcile (delta=%v)", apply.MetricApplyClientUpdatesTotal, delta)
			}
			return nil
		}).WithTimeout(90*time.Second).WithPolling(5*time.Second).Should(Succeed(),
			"pkg/apply ApplyObject should not invoke client.Update when spec is unchanged")

		By("checking the NRO object itself was updated (annotation patch persisted)")
		nroAfter := &nropv1.NUMAResourcesOperator{}
		Expect(e2eclient.Client.Get(ctx, nropKey, nroAfter)).To(Succeed())
		Expect(nroAfter.GetAnnotations()).To(HaveKey(e2eNoopReconcileAnnotation))
		Expect(nroAfter.GetUID()).To(Equal(nro.GetUID()))
	})
})
