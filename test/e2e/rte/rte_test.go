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
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	mcov1 "github.com/openshift/api/machineconfiguration/v1"
	operatorv1 "github.com/openshift/api/operator/v1"

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
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/rte"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
	"github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[nrop] with a running cluster with all the components", func() {
	When("[config][rte] NRO CR configured with LogLevel", func() {
		var timeout time.Duration
		var interval time.Duration

		BeforeEach(func() {
			timeout = 30 * time.Second
			interval = 5 * time.Second
		})

		It("should have the corresponding klog under RTE container", func(ctx context.Context) {
			nropObj := &nropv1.NUMAResourcesOperator{}
			Expect(clients.Client.Get(ctx, client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)).Should(Succeed())

			Eventually(func() bool {
				rteDss, err := getOwnedDss(clients.K8sClient, nropObj.ObjectMeta)
				if err != nil {
					klog.ErrorS(err, "failed to get the owned DaemonSets")
					return false
				}
				if len(rteDss) == 0 {
					klog.InfoS("expect the numaresourcesoperator to own at least one DaemonSet")
					return false
				}

				for _, ds := range rteDss {
					rteCnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, rteupdate.MainContainerName)
					Expect(rteCnt).ToNot(BeNil())

					found, match := matchLogLevelToKlog(rteCnt, nropObj.Spec.LogLevel)
					if !found {
						klog.InfoS("-v flag doesn't exist in container args managed by DaemonSet", "containerName", rteCnt.Name, "daemonsetName", ds.Name)
						return false
					}
					if !match {
						klog.InfoS("LogLevel doesn't match the existing -v flag in container under DaemonSet", "logLevel", nropObj.Spec.LogLevel, "containerName", rteCnt.Name, "daemonsetName", ds.Name)
						return false
					}
				}
				return true

			}).WithTimeout(timeout).WithPolling(interval).Should(BeTrue())
		})

		It("can modify the LogLevel in NRO CR and klog under RTE container should change respectively", func(ctx context.Context) {
			nropObj := &nropv1.NUMAResourcesOperator{}
			Expect(clients.Client.Get(ctx, client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)).Should(Succeed())

			nropObj.Spec.LogLevel = operatorv1.Trace
			Expect(clients.Client.Update(ctx, nropObj)).Should(Succeed())

			Eventually(func() bool {
				rteDss, err := getOwnedDss(clients.K8sClient, nropObj.ObjectMeta)
				if err != nil {
					klog.ErrorS(err, "failed to get the owned DaemonSets")
					return false
				}
				if len(rteDss) == 0 {
					klog.InfoS("expect the numaresourcesoperator to own at least one DaemonSet")
					return false
				}

				for _, ds := range rteDss {
					rteCnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, rteupdate.MainContainerName)
					Expect(rteCnt).ToNot(BeNil())

					found, match := matchLogLevelToKlog(rteCnt, nropObj.Spec.LogLevel)
					if !found {
						klog.InfoS("-v flag doesn't exist in container args under DaemonSet", "containerName", rteCnt.Name, "daemonsetName", ds.Name)
						return false
					}

					if !match {
						klog.InfoS("LogLevel doesn't match the existing -v flag in container managed by DaemonSet", "logLevel", nropObj.Spec.LogLevel, "containerName", rteCnt.Name, "daemonsetName", ds.Name)
						return false
					}
				}
				return true

			}).WithTimeout(timeout).WithPolling(interval).Should(BeTrue())
		})
	})

	When("[config][kubelet][rte] Kubelet Config includes reservations", func() {
		It("should configure RTE accordingly", func(ctx context.Context) {
			nropObj := &nropv1.NUMAResourcesOperator{}
			Expect(clients.Client.Get(ctx, client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)).Should(Succeed())
			Expect(nropObj.Status.DaemonSets).ToNot(BeEmpty())
			dssFromNodeGroupStatus := testobjs.GetDaemonSetListFromNodeGroupStatuses(nropObj.Status.NodeGroups)
			Expect(reflect.DeepEqual(nropObj.Status.DaemonSets, dssFromNodeGroupStatus)).To(BeTrue())
			klog.InfoS("using NRO instance", "name", nropObj.Name)

			// NROP guarantees all the daemonsets are in the same namespace,
			// so we pick the first for the sake of brevity
			namespace := nropObj.Status.DaemonSets[0].Namespace
			klog.InfoS("Using NRO namespace", "namespace", namespace)

			mcpList := &mcov1.MachineConfigPoolList{}
			Expect(clients.Client.List(ctx, mcpList)).Should(Succeed())
			klog.InfoS("detected MCPs", "count", len(mcpList.Items))

			mcoKcList := &mcov1.KubeletConfigList{}
			Expect(clients.Client.List(ctx, mcoKcList)).Should(Succeed())
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
				cm, err := clients.K8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, generatedName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				rc, err := rteConfigMapToRTEConfig(cm)
				klog.InfoS("Using RTE", "config", rc)
				Expect(err).ToNot(HaveOccurred())

				Expect(rc.Kubelet.TopologyManagerPolicy).To(Equal(kc.TopologyManagerPolicy), "TopologyManager Policy mismatch")
				Expect(rc.Kubelet.TopologyManagerScope).To(Equal(kc.TopologyManagerScope), "TopologyManager Scope mismatch")
			}
		})

		It("should keep the ConfigMap aligned with the KubeletConfig info", func(ctx context.Context) {
			nropObj := &nropv1.NUMAResourcesOperator{}
			Expect(clients.Client.Get(ctx, client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)).Should(Succeed())
			Expect(nropObj.Status.DaemonSets).ToNot(BeEmpty())
			dssFromNodeGroupStatus := testobjs.GetDaemonSetListFromNodeGroupStatuses(nropObj.Status.NodeGroups)
			Expect(reflect.DeepEqual(nropObj.Status.DaemonSets, dssFromNodeGroupStatus)).To(BeTrue())
			klog.InfoS("Using NRO instance", "name", nropObj.Name)

			// NROP guarantees all the daemonsets are in the same namespace,
			// so we pick the first for the sake of brevity
			namespace := nropObj.Status.DaemonSets[0].Namespace
			klog.InfoS("Using NRO namespace", "namespace", namespace)

			mcpList := &mcov1.MachineConfigPoolList{}
			Expect(clients.Client.List(ctx, mcpList)).Should(Succeed())
			klog.InfoS("detected MCPs", "count", len(mcpList.Items))

			mcoKcList := &mcov1.KubeletConfigList{}
			Expect(clients.Client.List(ctx, mcoKcList)).Should(Succeed())

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
			cm, err := clients.K8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, generatedName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			desiredMapState := make(map[string]string)
			for k, v := range cm.Data {
				desiredMapState[k] = v
			}

			cm.Data = nil
			cm, err = clients.K8sClient.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				cm, err = clients.K8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, generatedName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				if !reflect.DeepEqual(cm.Data, desiredMapState) {
					klog.InfoS("ConfigMap data is not in it's desired state, waiting for controller to update it", "configMapName", cm.Name)
					return false
				}
				return true
			}).WithTimeout(time.Minute * 5).WithPolling(time.Second * 30).Should(BeTrue())
		})
	})

	It("[rte][podfingerprint] should expose the pod set fingerprint in NRT objects", func(ctx context.Context) {
		nrtList := &nrtv1alpha2.NodeResourceTopologyList{}
		Expect(clients.Client.List(ctx, nrtList)).Should(Succeed())

		for _, nrt := range nrtList.Items {
			pfp, ok := nrt.Annotations[podfingerprint.Annotation]
			Expect(ok).To(BeTrue(), "missing podfingerprint annotation %q from NRT %q", podfingerprint.Annotation, nrt.Name)

			seemsValid := strings.HasPrefix(pfp, podfingerprint.Prefix)
			Expect(seemsValid).To(BeTrue(), "malformed podfingerprint %q from NRT %q", pfp, nrt.Name)
		}
	})

	It("[rte][podfingerprint] should expose the pod set fingerprint status on each worker", func(ctx context.Context) {
		nropObj := &nropv1.NUMAResourcesOperator{}
		Expect(clients.Client.Get(ctx, client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)).Should(Succeed())

		rteDss, err := getOwnedDss(clients.K8sClient, nropObj.ObjectMeta)
		Expect(err).ToNot(HaveOccurred())
		Expect(rteDss).ToNot(BeEmpty(), "no RTE DS found")

		for _, rteDs := range rteDss {
			By(fmt.Sprintf("checking DS: %s/%s status=[%v]", rteDs.Namespace, rteDs.Name, toJSON(rteDs.Status)))

			rtePods, err := podlist.With(clients.Client).ByDaemonset(ctx, rteDs)
			Expect(err).ToNot(HaveOccurred())
			Expect(rtePods).ToNot(BeEmpty(), "no RTE pods found for %s/%s", rteDs.Namespace, rteDs.Name)

			for _, rtePod := range rtePods {
				By(fmt.Sprintf("checking DS: %s/%s POD %s/%s (node=%s)", rteDs.Namespace, rteDs.Name, rtePod.Namespace, rtePod.Name, rtePod.Spec.NodeName))

				rteCnt := k8swgobjupdate.FindContainerByName(rtePod.Spec.Containers, rteupdate.MainContainerName)
				Expect(rteCnt).ToNot(BeNil())

				// TODO: hardcoded path. Any smarter option?
				cmd := []string{"/bin/cat", "/run/pfpstatus/dump.json"}
				stdout, stderr, err := remoteexec.CommandOnPod(ctx, clients.K8sClient, &rtePod, cmd...)
				if err != nil {
					_ = objects.LogEventsForPod(clients.K8sClient, rtePod.Namespace, rtePod.Name)
				}
				Expect(err).ToNot(HaveOccurred(), "err=%v stderr=%s", err, stderr)

				var st podfingerprint.Status
				Expect(json.Unmarshal(stdout, &st)).Should(Succeed())

				klog.InfoS("got status", "podNamespace", rtePod.Namespace, "podName", rtePod.Name, "containerName", rteCnt.Name, "fingerprintComputed", st.FingerprintComputed, "podCount", len(st.Pods))

				Expect(st.FingerprintComputed).ToNot(BeEmpty(), "missing fingerprint - should always be reported")
				Expect(st.Pods).ToNot(BeEmpty(), "missing pods - at least RTE itself should be there")
			}
		}
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
