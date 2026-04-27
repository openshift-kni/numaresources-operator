/*
 * Copyright 2022 Red Hat, Inc.
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

package rte

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"reflect"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	configv1 "github.com/openshift/api/config/v1"
	mcov1 "github.com/openshift/api/machineconfiguration/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	ctrltls "github.com/openshift/controller-runtime-common/pkg/tls"

	"github.com/k8stopologyawareschedwg/deployer/pkg/flagcodec"
	k8swgobjupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate"
	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"github.com/k8stopologyawareschedwg/podfingerprint"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/rte"
	objtls "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/tls"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	"github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("with a running cluster with all the components", func() {
	When("[config][rte] NRO CR configured with LogLevel", func() {
		timeout := 30 * time.Second
		interval := 5 * time.Second
		It("should have the corresponding klog under RTE container", func() {
			nropObj := &nropv1.NUMAResourcesOperator{}
			err := clients.Client.Get(context.TODO(), client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				rteDss, err := getOwnedDss(clients.K8sClient, nropObj.ObjectMeta)
				if err != nil {
					return fmt.Errorf("failed to get the owned DaemonSets: %w", err)
				}
				if len(rteDss) == 0 {
					return fmt.Errorf("expect the numaresourcesoperator to own at least one DaemonSet")
				}

				for _, ds := range rteDss {
					rteCnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, rteupdate.MainContainerName)
					if rteCnt == nil {
						return fmt.Errorf("no container %q found in DaemonSet %q", rteupdate.MainContainerName, ds.Name)
					}

					found, match := matchLogLevelToKlog(rteCnt, nropObj.Spec.LogLevel)
					if !found {
						return fmt.Errorf("-v flag doesn't exist in container %q args managed by DaemonSet %q", rteCnt.Name, ds.Name)
					}
					if !match {
						return fmt.Errorf("LogLevel doesn't match the existing -v flag in container %q under DaemonSet %q", rteCnt.Name, ds.Name)
					}
				}
				return nil

			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})

		It("can modify the LogLevel in NRO CR and klog under RTE container should change respectively", func() {
			nropObj := &nropv1.NUMAResourcesOperator{}
			Eventually(func() error {
				err := clients.Client.Get(context.TODO(), client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)
				if err != nil {
					return err
				}
				nropObj.Spec.LogLevel = operatorv1.Trace
				err = clients.Client.Update(context.TODO(), nropObj)
				if err != nil {
					return err
				}
				return nil
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to update LogLevel in NRO CR")

			Eventually(func() error {
				rteDss, err := getOwnedDss(clients.K8sClient, nropObj.ObjectMeta)
				if err != nil {
					return fmt.Errorf("failed to get the owned DaemonSets: %w", err)
				}
				if len(rteDss) == 0 {
					return errors.New("expect the numaresourcesoperator to own at least one DaemonSet")
				}

				for _, ds := range rteDss {
					rteCnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, rteupdate.MainContainerName)
					if rteCnt == nil {
						return fmt.Errorf("no container %q found", rteupdate.MainContainerName)
					}

					found, match := matchLogLevelToKlog(rteCnt, nropObj.Spec.LogLevel)
					if !found {
						return fmt.Errorf("-v flag doesn't exist in container %q args  DaemonSet %q", rteCnt.Name, ds.Name)
					}

					if !match {
						return fmt.Errorf("LogLevel doesn't match the existing -v=%v flag in container %q managed by DaemonSet %q", nropObj.Spec.LogLevel, rteCnt.Name, ds.Name)
					}
				}
				return nil

			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})
	})

	When("[config][kubelet][rte] Kubelet Config includes reservations", func() {
		It("should configure RTE accordingly", func() {
			nropObj := &nropv1.NUMAResourcesOperator{}
			err := clients.Client.Get(context.TODO(), client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)
			Expect(err).ToNot(HaveOccurred())
			Expect(nropObj.Status.DaemonSets).ToNot(BeEmpty())
			// When status.nodeGroups is populated, it must match the legacy daemonSets list; when it is
			// empty the operator may still report daemonSets only
			if len(nropObj.Status.NodeGroups) > 0 {
				dssFromNodeGroupStatus := testobjs.GetDaemonSetListFromNodeGroupStatuses(nropObj.Status.NodeGroups)
				Expect(reflect.DeepEqual(nropObj.Status.DaemonSets, dssFromNodeGroupStatus)).To(BeTrue())
			}
			klog.InfoS("using NRO instance", "name", nropObj.Name)

			// NROP guarantees all the daemonsets are in the same namespace,
			// so we pick the first for the sake of brevity
			namespace := nropObj.Status.DaemonSets[0].Namespace
			klog.InfoS("Using NRO namespace", "namespace", namespace)

			mcpList := &mcov1.MachineConfigPoolList{}
			err = clients.Client.List(context.TODO(), mcpList)
			Expect(err).ToNot(HaveOccurred())
			klog.InfoS("detected MCPs", "count", len(mcpList.Items))

			mcoKcList := &mcov1.KubeletConfigList{}
			err = clients.Client.List(context.TODO(), mcoKcList)
			Expect(err).ToNot(HaveOccurred())
			for _, mcoKc := range mcoKcList.Items {
				By(fmt.Sprintf("Considering MCO KubeletConfig %q", mcoKc.Name))

				kc, err := mcoKubeletConfToKubeletConf(&mcoKc)
				Expect(err).ToNot(HaveOccurred())

				mcps, err := nodegroupv1.FindMachineConfigPools(mcpList, nropObj.Spec.NodeGroups)
				Expect(err).ToNot(HaveOccurred())

				mcp, err := machineconfigpools.FindBySelector(mcps, mcoKc.Spec.MachineConfigPoolSelector)
				By(fmt.Sprintf("Considering MCP %q", mcp.Name))
				Expect(err).ToNot(HaveOccurred())

				generatedName := objectnames.GetComponentName(nropObj.Name, mcp.Name)
				klog.InfoS("generated config map", "name", generatedName)
				cm, err := clients.K8sClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), generatedName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				rc, err := rteConfigMapToRTEConfig(cm)
				klog.InfoS("Using RTE", "config", rc)
				Expect(err).ToNot(HaveOccurred())

				Expect(rc.Kubelet.TopologyManagerPolicy).To(Equal(kc.TopologyManagerPolicy), "TopologyManager Policy mismatch")
				Expect(rc.Kubelet.TopologyManagerScope).To(Equal(kc.TopologyManagerScope), "TopologyManager Scope mismatch")
			}
		})

		It("should keep the ConfigMap aligned with the KubeletConfig info", func() {
			nropObj := &nropv1.NUMAResourcesOperator{}
			err := clients.Client.Get(context.TODO(), client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)
			Expect(err).ToNot(HaveOccurred())
			Expect(nropObj.Status.DaemonSets).ToNot(BeEmpty())
			// When status.nodeGroups is populated, it must match the legacy daemonSets list; when it is
			// empty the operator may still report daemonSets only (backward-compatible status).
			if len(nropObj.Status.NodeGroups) > 0 {
				dssFromNodeGroupStatus := testobjs.GetDaemonSetListFromNodeGroupStatuses(nropObj.Status.NodeGroups)
				Expect(reflect.DeepEqual(nropObj.Status.DaemonSets, dssFromNodeGroupStatus)).To(BeTrue())
			}
			klog.InfoS("Using NRO instance", "name", nropObj.Name)

			// NROP guarantees all the daemonsets are in the same namespace,
			// so we pick the first for the sake of brevity
			namespace := nropObj.Status.DaemonSets[0].Namespace
			klog.InfoS("Using NRO namespace", "namespace", namespace)

			mcpList := &mcov1.MachineConfigPoolList{}
			err = clients.Client.List(context.TODO(), mcpList)
			Expect(err).ToNot(HaveOccurred())
			klog.InfoS("detected MCPs", "count", len(mcpList.Items))

			mcoKcList := &mcov1.KubeletConfigList{}
			err = clients.Client.List(context.TODO(), mcoKcList)
			Expect(err).ToNot(HaveOccurred())

			// pick the first for the sake of brevity
			mcoKc := mcoKcList.Items[0]
			By(fmt.Sprintf("Considering MCO KubeletConfig %q", mcoKc.Name))

			mcps, err := nodegroupv1.FindMachineConfigPools(mcpList, nropObj.Spec.NodeGroups)
			Expect(err).ToNot(HaveOccurred())

			mcp, err := machineconfigpools.FindBySelector(mcps, mcoKc.Spec.MachineConfigPoolSelector)
			By(fmt.Sprintf("Considering MCP %q", mcp.Name))
			Expect(err).ToNot(HaveOccurred())

			generatedName := objectnames.GetComponentName(nropObj.Name, mcp.Name)
			klog.InfoS("generated config map", "name", generatedName)

			var desiredMapState map[string]string
			Eventually(func() error {
				cm, err := clients.K8sClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), generatedName, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("failed to get ConfigMap %q: %w", generatedName, err)
				}
				desiredMapState = maps.Clone(cm.Data)
				cm.Data = nil
				_, err = clients.K8sClient.CoreV1().ConfigMaps(namespace).Update(context.TODO(), cm, metav1.UpdateOptions{})
				if err != nil {
					return fmt.Errorf("failed to update ConfigMap %q: %w", generatedName, err)
				}
				return nil
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to clear ConfigMap data")

			Eventually(func() error {
				cm, err := clients.K8sClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), generatedName, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("failed to get ConfigMap %q: %w", generatedName, err)
				}

				if !reflect.DeepEqual(cm.Data, desiredMapState) {
					return fmt.Errorf("ConfigMap %q data is not in its desired state, waiting for controller to update it", cm.Name)
				}
				return nil
			}).WithTimeout(time.Minute * 5).WithPolling(time.Second * 30).Should(Succeed())
		})
	})

	It("[rte][podfingerprint] should expose the pod set fingerprint in NRT objects", func() {
		nrtList := &nrtv1alpha2.NodeResourceTopologyList{}
		err := clients.Client.List(context.TODO(), nrtList)
		Expect(err).ToNot(HaveOccurred())

		for _, nrt := range nrtList.Items {
			pfp, ok := nrt.Annotations[podfingerprint.Annotation]
			Expect(ok).To(BeTrue(), "missing podfingerprint annotation %q from NRT %q", podfingerprint.Annotation, nrt.Name)

			seemsValid := strings.HasPrefix(pfp, podfingerprint.Prefix)
			Expect(seemsValid).To(BeTrue(), "malformed podfingerprint %q from NRT %q", pfp, nrt.Name)
		}
	})

	It("[rte][podfingerprint] should expose the pod set fingerprint status on each worker", func() {
		nropObj := &nropv1.NUMAResourcesOperator{}
		err := clients.Client.Get(context.TODO(), client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)
		Expect(err).ToNot(HaveOccurred())

		rteDss, err := getOwnedDss(clients.K8sClient, nropObj.ObjectMeta)
		Expect(err).ToNot(HaveOccurred())
		Expect(rteDss).ToNot(BeEmpty(), "no RTE DS found")

		for _, rteDs := range rteDss {
			By(fmt.Sprintf("checking DS: %s/%s status=[%v]", rteDs.Namespace, rteDs.Name, toJSON(rteDs.Status)))

			rtePods, err := podlist.With(clients.Client).ByDaemonset(context.TODO(), rteDs)
			Expect(err).ToNot(HaveOccurred())
			Expect(rtePods).ToNot(BeEmpty(), "no RTE pods found for %s/%s", rteDs.Namespace, rteDs.Name)

			for _, rtePod := range rtePods {
				By(fmt.Sprintf("checking DS: %s/%s POD %s/%s (node=%s)", rteDs.Namespace, rteDs.Name, rtePod.Namespace, rtePod.Name, rtePod.Spec.NodeName))

				rteCnt := k8swgobjupdate.FindContainerByName(rtePod.Spec.Containers, rteupdate.MainContainerName)
				Expect(rteCnt).ToNot(BeNil())

				// TODO: hardcoded path. Any smarter option?
				cmd := []string{"/bin/cat", "/run/pfpstatus/dump.json"}
				stdout, stderr, err := remoteexec.CommandOnPod(context.Background(), clients.K8sClient, &rtePod, cmd...)
				if err != nil {
					_ = objects.LogEventsForPod(clients.K8sClient, rtePod.Namespace, rtePod.Name)
				}
				Expect(err).ToNot(HaveOccurred(), "err=%v stderr=%s", err, stderr)

				var st podfingerprint.Status
				err = json.Unmarshal(stdout, &st)
				Expect(err).ToNot(HaveOccurred())

				klog.InfoS("got status", "podNamespace", rtePod.Namespace, "podName", rtePod.Name, "containerName", rteCnt.Name, "fingerprintComputed", st.FingerprintComputed, "podCount", len(st.Pods))

				Expect(st.FingerprintComputed).ToNot(BeEmpty(), "missing fingerprint - should always be reported")
				Expect(st.Pods).ToNot(BeEmpty(), "missing pods - at least RTE itself should be there")
			}
		}
	})
	Context("[tlscompliance][rte] rte complies with TLS Profile modifications", Label(label.Tier0, "feature:tlscompliance"), func() {
		const (
			rteDaemonSetCheckTimeout  = 30 * time.Second
			rteDaemonSetCheckInterval = 5 * time.Second
			apiServerUpdateTimeout    = 10 * time.Minute
			apiServerUpdateInterval   = 10 * time.Second
		)
		It("[test_id:88380] should have RTE DaemonSet args aligned with the cluster TLS profile", func(ctx context.Context) {
			By("Getting initial OCP TLS profile")
			tlsProfileSpec, err := ctrltls.FetchAPIServerTLSProfile(ctx, clients.Client)
			Expect(err).ToNot(HaveOccurred(), "Unable to get TLS Profile from APIServer")
			tlsConfig, _ := ctrltls.NewTLSConfigFromProfile(tlsProfileSpec)
			tlsCfg := &tls.Config{}
			tlsConfig(tlsCfg)
			tlsSettings := objtls.NewSettings(tlsCfg)
			klog.InfoS("Initial TLS Settings", "tlsSettings", tlsSettings)
			By("Getting the initial NRO operator object")
			nropObj := &nropv1.NUMAResourcesOperator{}
			Expect(clients.Client.Get(ctx, client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)).To(Succeed())
			Eventually(func() error {
				if err := clients.Client.Get(ctx, client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj); err != nil {
					return fmt.Errorf("failed to get NUMAResourcesOperator: %w", err)
				}
				if len(nropObj.Status.NodeGroups) == 0 {
					return fmt.Errorf("expect the numaresourcesoperator to have at least one NodeGroup in status")
				}
				for _, ng := range nropObj.Status.NodeGroups {
					ds := &appsv1.DaemonSet{}
					err := clients.Client.Get(ctx, client.ObjectKey{Namespace: ng.DaemonSet.Namespace, Name: ng.DaemonSet.Name}, ds)
					if err != nil {
						return fmt.Errorf("failed to get DaemonSet %s/%s: %w", ng.DaemonSet.Namespace, ng.DaemonSet.Name, err)
					}
					rteCnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, rteupdate.MainContainerName)
					if rteCnt == nil {
						return fmt.Errorf("main container not found daemonsetName=%q", ds.Name)
					}
					rteFlags := flagcodec.ParseArgvKeyValue(rteCnt.Args, flagcodec.WithFlagNormalization)
					if err := matchTLSFlag(rteFlags, "--metrics-tls-min-version", tlsSettings.MinVersion, ds.Name); err != nil {
						return err
					}
					// if apiServer is configured with TLS1.3 there are no args --metrics-tls-cipher-suites because
					// Go's crypto/tls package handles TLS 1.3 differently than older protocols. In TLS 1.3,
					// the cipher suites are fixed to a secure, built-in list (which will eventually include Post-Quantum Cryptography algorithms)
					// and cannot be customized using the standard CipherSuites field. so tlsSettings.CipherSuites would be empty in that case.
					if err := matchTLSFlag(rteFlags, "--metrics-tls-cipher-suites", tlsSettings.CipherSuites, ds.Name); err != nil {
						return err
					}
				}
				return nil
			}).WithTimeout(rteDaemonSetCheckTimeout).WithPolling(rteDaemonSetCheckInterval).Should(Succeed())
		})

		It("[test_id:88383] should update scheduler deployment TLS args to adhere to modified TLS profile", Label(label.Slow, label.Reboot), func(ctx context.Context) {
			// Maximum time allowed for each MCP reconciliation cycle.
			// Long timeout because MCP updates can require node drains/reboots.
			mcpUpdateTimeout := 40 * time.Minute
			// Polling interval while waiting for MCP status transitions.
			mcpUpdatePolling := 20 * time.Second
			By("Updating TLS profile to a new settings")
			klog.InfoS("determining a different TLS profile to apply")
			apiServerObj := &configv1.APIServer{}
			apiServerKey := client.ObjectKey{Name: ctrltls.APIServerName}
			Expect(clients.Client.Get(ctx, apiServerKey, apiServerObj)).To(Succeed(), "failed to get APIServer object")
			originalTLSProfile := apiServerObj.Spec.TLSSecurityProfile
			originalTLSProfileType := configv1.TLSProfileIntermediateType
			// If profile exists, use its actual type.
			if originalTLSProfile != nil {
				// Record the original type for later restoration.
				originalTLSProfileType = originalTLSProfile.Type
			}

			// Select Modern as the primary target profile for this test.
			newProfile := &configv1.TLSSecurityProfile{
				Type:   configv1.TLSProfileModernType,
				Modern: &configv1.ModernTLSProfile{},
			}

			// If cluster already uses Modern, switch to Intermediate instead.
			if originalTLSProfileType == newProfile.Type {
				newProfile = &configv1.TLSSecurityProfile{
					Type:         configv1.TLSProfileIntermediateType,
					Intermediate: &configv1.IntermediateTLSProfile{},
				}
			}

			klog.InfoS("switching TLS profile", "from", originalTLSProfileType, "to", newProfile.Type)
			By(fmt.Sprintf("updating APIServer TLS profile to %v", newProfile.Type))
			Eventually(func(g Gomega) {
				updatedAPIServer := &configv1.APIServer{}
				g.Expect(clients.Client.Get(ctx, apiServerKey, updatedAPIServer)).To(Succeed(), "failed to get APIServer object")
				updatedAPIServer.Spec = apiServerObj.Spec
				updatedAPIServer.Spec.TLSSecurityProfile = newProfile
				g.Expect(clients.Client.Update(ctx, updatedAPIServer)).To(Succeed(), "failed to updated APIServer TLS profile")
			}).WithTimeout(apiServerUpdateTimeout).WithPolling(apiServerUpdateInterval).Should(Succeed())

			DeferCleanup(func(ctx context.Context) {
				By("reverting the APIServer TLS profile to original setting")
				klog.InfoS("switching TLS profile", "from", newProfile.Type, "to", originalTLSProfileType)
				Eventually(func(g Gomega) {
					updatedAPIServer := &configv1.APIServer{}
					g.Expect(clients.Client.Get(ctx, apiServerKey, updatedAPIServer)).To(Succeed(), "failed to get APIServer object")
					updatedAPIServer.Spec = apiServerObj.Spec
					g.Expect(clients.Client.Update(ctx, updatedAPIServer)).To(Succeed(), "failed to updated APIServer TLS profile")
				}).WithTimeout(apiServerUpdateTimeout).WithPolling(apiServerUpdateInterval).Should(Succeed())

				klog.InfoS("Waiting for all MCPs to get updated")
				eg := errgroup.Group{}
				allMCPs := &mcov1.MachineConfigPoolList{}
				Expect(clients.Client.List(ctx, allMCPs)).To(Succeed())
				for _, mcp := range allMCPs.Items {
					eg.Go(func() error {
						err := waitForMCPConditionWithRetry(ctx, &mcp, mcov1.MachineConfigPoolUpdating, mcpUpdateTimeout, mcpUpdatePolling)
						if err != nil {
							klog.InfoS("failed to wait for MCP to start updating", "mcp", mcp.Name, "err", err)
							return err
						}
						klog.InfoS("MCP started updating; waiting for it to complete", "mcp", mcp.Name)
						return waitForMCPConditionWithRetry(ctx, &mcp, mcov1.MachineConfigPoolUpdated, mcpUpdateTimeout, mcpUpdatePolling)
					})
				}
				Expect(eg.Wait()).To(Succeed(), "failed to wait for all MCPs to start updating")

				By("Verifying per-node MachineConfig alignment after MCP rollback")
				for i := range allMCPs.Items {
					Expect(verifyNodesMatchMCPConfig(ctx, &allMCPs.Items[i])).To(Succeed(),
						"node config mismatch for MCP %q", allMCPs.Items[i].Name)
				}
			})

			// Wait for all pools to complete forward reconciliation.
			klog.InfoS("Waiting for all MCPs to get updated")
			// Group for parallel per-pool wait operations.
			eg := errgroup.Group{}
			// Container for current MCP list.
			allMCPs := &mcov1.MachineConfigPoolList{}
			// List all pools present in cluster.
			Expect(clients.Client.List(ctx, allMCPs)).To(Succeed())
			// Launch one waiter per pool.
			for _, mcp := range allMCPs.Items {
				eg.Go(func() error {
					err := waitForMCPConditionWithRetry(ctx, &mcp, mcov1.MachineConfigPoolUpdating, mcpUpdateTimeout, mcpUpdatePolling)
					// Return early when update transition is not observed.
					if err != nil {
						// Log pool-specific failure details.
						klog.InfoS("failed to wait for MCP to start updating", "mcp", mcp.Name, "err", err)
						// Propagate error to errgroup.
						return err
					}
					// Log that pool started updating.
					klog.InfoS("MCP started updating; waiting for it to complete", "mcp", mcp.Name)
					// Then wait until pool reports Updated condition.
					return waitForMCPConditionWithRetry(ctx, &mcp, mcov1.MachineConfigPoolUpdated, mcpUpdateTimeout, mcpUpdatePolling)
				})
			}
			Expect(eg.Wait()).To(Succeed(), "failed to wait for all MCPs to start updating")

			// We not only want to verify that the MCPs are updated, but also that the nodes are updated with the new config.
			By("Verifying per-node MachineConfig alignment after MCP rollout")
			for i := range allMCPs.Items {
				Expect(verifyNodesMatchMCPConfig(ctx, &allMCPs.Items[i])).To(Succeed(),
					"node config mismatch for MCP %q", allMCPs.Items[i].Name)
			}

			newTLSProfileSpec := *configv1.TLSProfiles[newProfile.Type]
			newTLSConfigFn, _ := ctrltls.NewTLSConfigFromProfile(newTLSProfileSpec)
			newTLSCfg := &tls.Config{}
			newTLSConfigFn(newTLSCfg)
			expectedTLSSettings := objtls.NewSettings(newTLSCfg)
			klog.InfoS("expected TLS settings after update", "tlsSettings", expectedTLSSettings)
			By("Getting the initial NRO operator object")
			nropObj := &nropv1.NUMAResourcesOperator{}
			Expect(clients.Client.Get(ctx, client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)).To(Succeed())
			Eventually(func() error {
				if err := clients.Client.Get(ctx, client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj); err != nil {
					return fmt.Errorf("failed to get NUMAResourcesOperator: %w", err)
				}
				if len(nropObj.Status.NodeGroups) == 0 {
					return fmt.Errorf("expect the numaresourcesoperator to have at least one NodeGroup in status")
				}
				for _, ng := range nropObj.Status.NodeGroups {
					ds, err := clients.K8sClient.AppsV1().DaemonSets(ng.DaemonSet.Namespace).Get(ctx, ng.DaemonSet.Name, metav1.GetOptions{})
					if err != nil {
						return fmt.Errorf("failed to get DaemonSet %s/%s: %w", ng.DaemonSet.Namespace, ng.DaemonSet.Name, err)
					}
					rteCnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, rteupdate.MainContainerName)
					if rteCnt == nil {
						return fmt.Errorf("main container not found daemonsetName=%q", ds.Name)
					}
					rteFlags := flagcodec.ParseArgvKeyValue(rteCnt.Args, flagcodec.WithFlagNormalization)
					if err := matchTLSFlag(rteFlags, "--metrics-tls-min-version", expectedTLSSettings.MinVersion, ds.Name); err != nil {
						return err
					}
					// in the case of TLS1.3 the expectedTLSSettings.CipherSuites will be empty
					// When using a minimum of TLS 1.3—which is mandated by the Modern profile—you generally do not pass or explicitly configure cipher suites
					if err := matchTLSFlag(rteFlags, "--metrics-tls-cipher-suites", expectedTLSSettings.CipherSuites, ds.Name); err != nil {
						return err
					}
				}
				return nil
			}).WithTimeout(rteDaemonSetCheckTimeout).WithPolling(rteDaemonSetCheckInterval).Should(Succeed())
		})
	})
})

func getOwnedDss(cs kubernetes.Interface, owner metav1.ObjectMeta) ([]appsv1.DaemonSet, error) {
	dss, err := cs.AppsV1().DaemonSets("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// multiple DaemonSets in case of multiple nodeGroups
	var rteDss []appsv1.DaemonSet
	for _, ds := range dss.Items {
		if objects.IsOwnedBy(ds.ObjectMeta, owner) {
			rteDss = append(rteDss, ds)
		}
	}
	return rteDss, nil
}

func matchLogLevelToKlog(cnt *corev1.Container, level operatorv1.LogLevel) (bool, bool) {
	rteFlags := flagcodec.ParseArgvKeyValue(cnt.Args, flagcodec.WithFlagNormalization)
	kLvl := loglevel.ToKlog(level)

	val, found := rteFlags.GetFlag("--")
	return found, val.Data == kLvl.String()
}

func matchTLSFlag(rteFlags *flagcodec.Flags, flagName, expected, dsName string) error {
	val, found := rteFlags.GetFlag(flagName)
	if expected == "" {
		if found && val.Data != "" {
			return fmt.Errorf("%s should be absent or empty for this TLS profile daemonsetName=%q got=%q", flagName, dsName, val.Data)
		}
		return nil
	}
	if !found || val.Data != expected {
		got := ""
		if found {
			got = val.Data
		}
		return fmt.Errorf("%s mismatch daemonsetName=%q expected=%q got=%q", flagName, dsName, expected, got)
	}
	return nil
}

func mcoKubeletConfToKubeletConf(mcoKc *mcov1.KubeletConfig) (*kubeletconfigv1beta1.KubeletConfiguration, error) {
	kc := &kubeletconfigv1beta1.KubeletConfiguration{}
	err := json.Unmarshal(mcoKc.Spec.KubeletConfig.Raw, kc)
	return kc, err
}

func rteConfigMapToRTEConfig(cm *corev1.ConfigMap) (*rteconfig.Config, error) {
	rc := &rteconfig.Config{}
	// TODO constant
	err := yaml.Unmarshal([]byte(cm.Data["config.yaml"]), rc)
	return rc, err
}

func toJSON(obj interface{}) string {
	data, err := json.Marshal(obj)
	if err != nil {
		return "<ERROR>"
	}
	return string(data)
}

// waitForMCPConditionWithRetry wraps ForMachineConfigPoolCondition with automatic
// retries for transient network errors (e.g. connection resets, HTTP/2 stream errors,
// 504 gateway timeouts) that are common during control-plane churn.
// Unlike the plain ForMachineConfigPoolCondition call, which treats any API error as
// terminal, this function distinguishes transient transport-level failures from real
// condition failures and retries only the former.
// The overall wall-clock time is bounded by timeout; each retry consumes the remaining
// budget rather than resetting it.
func waitForMCPConditionWithRetry(ctx context.Context, mcp *mcov1.MachineConfigPool, condType mcov1.MachineConfigPoolConditionType, timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if lastErr != nil {
				return fmt.Errorf("timed out waiting for MCP %q condition %q: %w", mcp.Name, condType, lastErr)
			}
			return fmt.Errorf("timed out waiting for MCP %q condition %q", mcp.Name, condType)
		}

		err := wait.With(clients.Client).Timeout(remaining).Interval(interval).ForMachineConfigPoolCondition(ctx, mcp, condType)
		if err == nil {
			return nil
		}
		if !isTransientMCPError(err) {
			return err
		}

		lastErr = err
		klog.InfoS("transient MCP wait error; retrying", "mcp", mcp.Name, "condition", condType, "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// isTransientMCPError returns true for transport-level errors (url.Error, HTTP/2
// stream errors, 504 gateway timeouts) that are expected during MCP transitions
// when the API server is temporarily overloaded or unreachable, and are safe to retry.
func isTransientMCPError(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	if apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || apierrors.IsServiceUnavailable(err) {
		return true
	}
	// API transport hiccups seen during control-plane churn may be wrapped.
	return strings.Contains(err.Error(), "stream error") || strings.Contains(err.Error(), "INTERNAL_ERROR")
}

// verifyNodesMatchMCPConfig re-reads the given MCP, lists the nodes matching
// its NodeSelector, and checks that every node's currentConfig annotation
// matches the MCP's rendered configuration (status.configuration.name).
// This provides per-node confidence that the rollout actually happened.
func verifyNodesMatchMCPConfig(ctx context.Context, mcp *mcov1.MachineConfigPool) error {
	updatedMCP := &mcov1.MachineConfigPool{}
	if err := clients.Client.Get(ctx, client.ObjectKeyFromObject(mcp), updatedMCP); err != nil {
		return fmt.Errorf("failed to re-read MCP %q: %w", mcp.Name, err)
	}
	renderedConfig := updatedMCP.Status.Configuration.Name
	if renderedConfig == "" {
		return fmt.Errorf("MCP %q has empty status.configuration.name", mcp.Name)
	}

	if updatedMCP.Spec.NodeSelector == nil {
		return fmt.Errorf("MCP %q has nil NodeSelector", mcp.Name)
	}
	nodeList := &corev1.NodeList{}
	if err := clients.Client.List(ctx, nodeList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(updatedMCP.Spec.NodeSelector.MatchLabels),
	}); err != nil {
		return fmt.Errorf("failed to list nodes for MCP %q: %w", mcp.Name, err)
	}

	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		currentConfig := node.Annotations[wait.CurrentConfigNodeAnnotation]
		if currentConfig != renderedConfig {
			return fmt.Errorf("node %q currentConfig=%q does not match MCP %q rendered config=%q",
				node.Name, currentConfig, mcp.Name, renderedConfig)
		}
	}
	klog.InfoS("all nodes match MCP config", "mcp", mcp.Name, "config", renderedConfig, "nodeCount", len(nodeList.Items))
	return nil
}
