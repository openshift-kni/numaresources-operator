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

	"github.com/ghodss/yaml"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/flagcodec"
	k8swgobjupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"github.com/k8stopologyawareschedwg/podfingerprint"
	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/rte"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
	"github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	operatorv1 "github.com/openshift/api/operator/v1"
	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

var _ = ginkgo.Describe("with a running cluster with all the components", func() {
	ginkgo.When("[config][rte] NRO CR configured with LogLevel", func() {
		timeout := 30 * time.Second
		interval := 5 * time.Second
		ginkgo.It("should have the corresponding klog under RTE container", func() {
			nropObj := &nropv1.NUMAResourcesOperator{}
			err := clients.Client.Get(context.TODO(), client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				rteDss, err := getOwnedDss(clients.K8sClient, nropObj.ObjectMeta)
				if err != nil {
					klog.Warningf("failed to get the owned DaemonSets: %v", err)
					return false
				}
				if len(rteDss) == 0 {
					klog.Warningf("expect the numaresourcesoperator to own at least one DaemonSet: %v")
					return false
				}

				for _, ds := range rteDss {
					rteCnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, rteupdate.MainContainerName)
					gomega.Expect(rteCnt).ToNot(gomega.BeNil())

					found, match := matchLogLevelToKlog(rteCnt, nropObj.Spec.LogLevel)
					if !found {
						klog.Warningf("--v flag doesn't exist in container %q args managed by DaemonSet: %q", rteCnt.Name, ds.Name)
						return false
					}
					if !match {
						klog.Warningf("LogLevel %s doesn't match the existing --v flag in container: %q under DaemonSet: %q", nropObj.Spec.LogLevel, rteCnt.Name, ds.Name)
						return false
					}
				}
				return true

			}).WithTimeout(timeout).WithPolling(interval).Should(gomega.BeTrue())
		})

		ginkgo.It("can modify the LogLevel in NRO CR and klog under RTE container should change respectively", func() {
			nropObj := &nropv1.NUMAResourcesOperator{}
			err := clients.Client.Get(context.TODO(), client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			nropObj.Spec.LogLevel = operatorv1.Trace
			err = clients.Client.Update(context.TODO(), nropObj)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				rteDss, err := getOwnedDss(clients.K8sClient, nropObj.ObjectMeta)
				if err != nil {
					klog.Warningf("failed to get the owned DaemonSets: %v", err)
					return false
				}
				if len(rteDss) == 0 {
					klog.Warningf("expect the numaresourcesoperator to own at least one DaemonSet: %v")
					return false
				}

				for _, ds := range rteDss {
					rteCnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, rteupdate.MainContainerName)
					gomega.Expect(rteCnt).ToNot(gomega.BeNil())

					found, match := matchLogLevelToKlog(rteCnt, nropObj.Spec.LogLevel)
					if !found {
						klog.Warningf("--v flag doesn't exist in container %q args under DaemonSet: %q", rteCnt.Name, ds.Name)
						return false
					}

					if !match {
						klog.Warningf("LogLevel %s doesn't match the existing --v flag in container: %q managed by DaemonSet: %q", nropObj.Spec.LogLevel, rteCnt.Name, ds.Name)
						return false
					}
				}
				return true

			}).WithTimeout(timeout).WithPolling(interval).Should(gomega.BeTrue())
		})
	})

	ginkgo.When("[config][kubelet][rte] Kubelet Config includes reservations", func() {
		ginkgo.It("should configure RTE accordingly", func() {
			nropObj := &nropv1.NUMAResourcesOperator{}
			err := clients.Client.Get(context.TODO(), client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(nropObj.Status.DaemonSets).ToNot(gomega.BeEmpty())
			klog.Infof("NRO %q", nropObj.Name)

			// NROP guarantees all the daemonsets are in the same namespace,
			// so we pick the first for the sake of brevity
			namespace := nropObj.Status.DaemonSets[0].Namespace
			klog.Infof("namespace %q", namespace)

			mcpList := &mcov1.MachineConfigPoolList{}
			err = clients.Client.List(context.TODO(), mcpList)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			klog.Infof("MCPs count: %d", len(mcpList.Items))

			mcoKcList := &mcov1.KubeletConfigList{}
			err = clients.Client.List(context.TODO(), mcoKcList)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			for _, mcoKc := range mcoKcList.Items {
				ginkgo.By(fmt.Sprintf("Considering MCO KubeletConfig %q", mcoKc.Name))

				kc, err := mcoKubeletConfToKubeletConf(&mcoKc)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				mcps, err := nodegroupv1.FindMachineConfigPools(mcpList, nropObj.Spec.NodeGroups)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				mcp, err := machineconfigpools.FindBySelector(mcps, mcoKc.Spec.MachineConfigPoolSelector)
				ginkgo.By(fmt.Sprintf("Considering MCP %q", mcp.Name))
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				generatedName := objectnames.GetComponentName(nropObj.Name, mcp.Name)
				klog.Infof("generated config map name: %q", generatedName)
				cm, err := clients.K8sClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), generatedName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				rc, err := rteConfigMapToRTEConfig(cm)
				klog.Infof("RTE config: %#v", rc)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				gomega.Expect(rc.TopologyManagerPolicy).To(gomega.Equal(kc.TopologyManagerPolicy), "TopologyManager Policy mismatch")
				gomega.Expect(rc.TopologyManagerScope).To(gomega.Equal(kc.TopologyManagerScope), "TopologyManager Scope mismatch")
			}
		})

		ginkgo.It("should keep the ConfigMap aligned with the KubeletConfig info", func() {
			nropObj := &nropv1.NUMAResourcesOperator{}
			err := clients.Client.Get(context.TODO(), client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(nropObj.Status.DaemonSets).ToNot(gomega.BeEmpty())
			klog.Infof("NRO %q", nropObj.Name)

			// NROP guarantees all the daemonsets are in the same namespace,
			// so we pick the first for the sake of brevity
			namespace := nropObj.Status.DaemonSets[0].Namespace
			klog.Infof("namespace %q", namespace)

			mcpList := &mcov1.MachineConfigPoolList{}
			err = clients.Client.List(context.TODO(), mcpList)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			klog.Infof("MCPs count: %d", len(mcpList.Items))

			mcoKcList := &mcov1.KubeletConfigList{}
			err = clients.Client.List(context.TODO(), mcoKcList)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// pick the first for the sake of brevity
			mcoKc := mcoKcList.Items[0]
			ginkgo.By(fmt.Sprintf("Considering MCO KubeletConfig %q", mcoKc.Name))

			mcps, err := nodegroupv1.FindMachineConfigPools(mcpList, nropObj.Spec.NodeGroups)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			mcp, err := machineconfigpools.FindBySelector(mcps, mcoKc.Spec.MachineConfigPoolSelector)
			ginkgo.By(fmt.Sprintf("Considering MCP %q", mcp.Name))
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			generatedName := objectnames.GetComponentName(nropObj.Name, mcp.Name)
			klog.Infof("generated config map name: %q", generatedName)
			cm, err := clients.K8sClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), generatedName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			desiredMapState := make(map[string]string)
			for k, v := range cm.Data {
				desiredMapState[k] = v
			}

			cm.Data = nil
			cm, err = clients.K8sClient.CoreV1().ConfigMaps(namespace).Update(context.TODO(), cm, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				cm, err = clients.K8sClient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), generatedName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				if !reflect.DeepEqual(cm.Data, desiredMapState) {
					klog.Warningf("ConfigMap %q data is not in it's desired state, waiting for controller to update it")
					return false
				}
				return true
			}).WithTimeout(time.Minute * 5).WithPolling(time.Second * 30).Should(gomega.BeTrue())
		})
	})

	ginkgo.It("[rte][podfingerprint] should expose the pod set fingerprint in NRT objects", func() {
		nrtList := &nrtv1alpha2.NodeResourceTopologyList{}
		err := clients.Client.List(context.TODO(), nrtList)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		for _, nrt := range nrtList.Items {
			pfp, ok := nrt.Annotations[podfingerprint.Annotation]
			gomega.Expect(ok).To(gomega.BeTrue(), "missing podfingerprint annotation %q from NRT %q", podfingerprint.Annotation, nrt.Name)

			seemsValid := strings.HasPrefix(pfp, podfingerprint.Prefix)
			gomega.Expect(seemsValid).To(gomega.BeTrue(), "malformed podfingerprint %q from NRT %q", pfp, nrt.Name)
		}
	})

	ginkgo.It("[rte][podfingerprint] should expose the pod set fingerprint status on each worker", func() {
		nropObj := &nropv1.NUMAResourcesOperator{}
		err := clients.Client.Get(context.TODO(), client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj)

		rteDss, err := getOwnedDss(clients.K8sClient, nropObj.ObjectMeta)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(rteDss).ToNot(gomega.BeEmpty(), "no RTE DS found")

		for _, rteDs := range rteDss {
			ginkgo.By(fmt.Sprintf("checking DS: %s/%s status=[%v]", rteDs.Namespace, rteDs.Name, toJSON(rteDs.Status)))

			rtePods, err := podlistByDaemonset(clients.K8sClient, rteDs)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(rtePods).ToNot(gomega.BeEmpty(), "no RTE pods found for %s/%s", rteDs.Namespace, rteDs.Name)

			for _, rtePod := range rtePods {
				ginkgo.By(fmt.Sprintf("checking DS: %s/%s POD %s/%s (node=%s)", rteDs.Namespace, rteDs.Name, rtePod.Namespace, rtePod.Name, rtePod.Spec.NodeName))

				rteCnt := k8swgobjupdate.FindContainerByName(rtePod.Spec.Containers, rteupdate.MainContainerName)
				gomega.Expect(rteCnt).ToNot(gomega.BeNil())

				// TODO: hardcoded path. Any smarter option?
				cmd := []string{"/bin/cat", "/run/pfpstatus/dump.json"}
				stdout, stderr, err := remoteexec.CommandOnPod(context.Background(), clients.K8sClient, &rtePod, cmd...)
				if err != nil {
					_ = objects.LogEventsForPod(clients.K8sClient, rtePod.Namespace, rtePod.Name)
				}
				gomega.Expect(err).ToNot(gomega.HaveOccurred(), "err=%v stderr=%s", err, stderr)

				var st podfingerprint.Status
				err = json.Unmarshal(stdout, &st)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				klog.Infof("got status from %s/%s/%s -> %q (%d pods)", rtePod.Namespace, rtePod.Name, rteCnt.Name, st.FingerprintComputed, len(st.Pods))

				gomega.Expect(st.FingerprintComputed).ToNot(gomega.BeEmpty(), "missing fingerprint - should always be reported")
				gomega.Expect(st.Pods).ToNot(gomega.BeEmpty(), "missing pods - at least RTE itself should be there")
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
	rteFlags := flagcodec.ParseArgvKeyValue(cnt.Args)
	kLvl := loglevel.ToKlog(level)

	val, found := rteFlags.GetFlag("--v")
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

func podlistByDaemonset(cs kubernetes.Interface, ds appsv1.DaemonSet) ([]corev1.Pod, error) {
	sel, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
	if err != nil {
		return nil, err
	}

	podList, err := cs.CoreV1().Pods(ds.Namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}

func toJSON(obj interface{}) string {
	data, err := json.Marshal(obj)
	if err != nil {
		return "<ERROR>"
	}
	return string(data)
}
