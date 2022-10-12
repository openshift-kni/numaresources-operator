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
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"k8s.io/kubernetes/test/e2e/framework"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	mcov1cli "github.com/openshift/machine-config-operator/pkg/generated/clientset/versioned/typed/machineconfiguration.openshift.io/v1"

	nrtv1alpha1cli "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/generated/clientset/versioned"
	"github.com/k8stopologyawareschedwg/podfingerprint"

	"github.com/openshift-kni/numaresources-operator/pkg/flagcodec"
	nropv1alpha1cli "github.com/openshift-kni/numaresources-operator/pkg/k8sclientset/generated/clientset/versioned/typed/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
	"github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/rte"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"

	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

var _ = ginkgo.Describe("with a running cluster with all the components", func() {
	var (
		initialized bool
		nropcli     *nropv1alpha1cli.NumaresourcesoperatorV1alpha1Client
		mcocli      *mcov1cli.MachineconfigurationV1Client
		nrtcli      *nrtv1alpha1cli.Clientset
	)

	f := framework.NewDefaultFramework("rte")

	ginkgo.BeforeEach(func() {
		if initialized {
			return
		}
		var err error

		nropcli, err = nropv1alpha1cli.NewForConfig(f.ClientConfig())
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		mcocli, err = mcov1cli.NewForConfig(f.ClientConfig())
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		nrtcli, err = nrtv1alpha1cli.NewForConfig(f.ClientConfig())
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		initialized = true
	})

	ginkgo.When("[config][rte] NRO CR configured with LogLevel", func() {
		timeout := 30 * time.Second
		interval := 5 * time.Second
		ginkgo.It("should have the corresponding klog under RTE container", func() {
			nropObj, err := nropcli.NUMAResourcesOperators().Get(context.TODO(), objectnames.DefaultNUMAResourcesOperatorCrName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				rteDss, err := getOwnedDss(f, nropObj.ObjectMeta)
				if err != nil {
					klog.Warningf("failed to get the owned DaemonSets: %v", err)
					return false
				}
				if len(rteDss) == 0 {
					klog.Warningf("expect the numaresourcesoperator to own at least one DaemonSet: %v")
					return false
				}

				for _, ds := range rteDss {
					rteCnt, err := rteupdate.FindContainerByName(&ds.Spec.Template.Spec, rteupdate.MainContainerName)
					gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
			nropObj, err := nropcli.NUMAResourcesOperators().Get(context.TODO(), objectnames.DefaultNUMAResourcesOperatorCrName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			nropObj.Spec.LogLevel = operatorv1.Trace
			nropObj, err = nropcli.NUMAResourcesOperators().Update(context.TODO(), nropObj, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				rteDss, err := getOwnedDss(f, nropObj.ObjectMeta)
				if err != nil {
					klog.Warningf("failed to get the owned DaemonSets: %v", err)
					return false
				}
				if len(rteDss) == 0 {
					klog.Warningf("expect the numaresourcesoperator to own at least one DaemonSet: %v")
					return false
				}

				for _, ds := range rteDss {
					rteCnt, err := rteupdate.FindContainerByName(&ds.Spec.Template.Spec, rteupdate.MainContainerName)
					gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
			nroObj, err := nropcli.NUMAResourcesOperators().Get(context.TODO(), objectnames.DefaultNUMAResourcesOperatorCrName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(nroObj.Status.DaemonSets).ToNot(gomega.BeEmpty())
			klog.Infof("NRO %q", nroObj.Name)

			// NROP guarantees all the daemonsets are in the same namespace,
			// so we pick the first for the sake of brevity
			namespace := nroObj.Status.DaemonSets[0].Namespace
			klog.Infof("namespace %q", namespace)

			mcpList, err := mcocli.MachineConfigPools().List(context.TODO(), metav1.ListOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			klog.Infof("MCPs count: %d", len(mcpList.Items))

			mcoKcList, err := mcocli.KubeletConfigs().List(context.TODO(), metav1.ListOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			for _, mcoKc := range mcoKcList.Items {
				ginkgo.By(fmt.Sprintf("Considering MCO KubeletConfig %q", mcoKc.Name))

				kc, err := mcoKubeletConfToKubeletConf(&mcoKc)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				mcps, err := machineconfigpools.FindListByNodeGroups(mcpList, nroObj.Spec.NodeGroups)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				mcp, err := machineconfigpools.FindBySelector(mcps, mcoKc.Spec.MachineConfigPoolSelector)
				ginkgo.By(fmt.Sprintf("Considering MCP %q", mcp.Name))
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				generatedName := objectnames.GetComponentName(nroObj.Name, mcp.Name)
				klog.Infof("generated config map name: %q", generatedName)
				cm, err := f.ClientSet.CoreV1().ConfigMaps(namespace).Get(context.TODO(), generatedName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				rc, err := rteConfigMapToRTEConfig(cm)
				klog.Infof("RTE config: %#v", rc)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// we intentionally don't check the values themselves - atm this would
				// be to complex, effectively rewriting most of the controller logic
				if len(kc.ReservedSystemCPUs) > 0 {
					gomega.Expect(rc.Resources.ReservedCPUs).ToNot(gomega.BeEmpty())
				}
				if len(kc.ReservedMemory) > 0 {
					gomega.Expect(rc.Resources.ReservedMemory).ToNot(gomega.BeEmpty())
				}
			}
		})

		ginkgo.It("should keep the ConfigMap aligned with the KubeletConfig info", func() {
			nroObj, err := nropcli.NUMAResourcesOperators().Get(context.TODO(), objectnames.DefaultNUMAResourcesOperatorCrName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(nroObj.Status.DaemonSets).ToNot(gomega.BeEmpty())
			klog.Infof("NRO %q", nroObj.Name)

			// NROP guarantees all the daemonsets are in the same namespace,
			// so we pick the first for the sake of brevity
			namespace := nroObj.Status.DaemonSets[0].Namespace
			klog.Infof("namespace %q", namespace)

			mcpList, err := mcocli.MachineConfigPools().List(context.TODO(), metav1.ListOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			klog.Infof("MCPs count: %d", len(mcpList.Items))

			mcoKcList, err := mcocli.KubeletConfigs().List(context.TODO(), metav1.ListOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// pick the first for the sake of brevity
			mcoKc := mcoKcList.Items[0]
			ginkgo.By(fmt.Sprintf("Considering MCO KubeletConfig %q", mcoKc.Name))

			mcps, err := machineconfigpools.FindListByNodeGroups(mcpList, nroObj.Spec.NodeGroups)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			mcp, err := machineconfigpools.FindBySelector(mcps, mcoKc.Spec.MachineConfigPoolSelector)
			ginkgo.By(fmt.Sprintf("Considering MCP %q", mcp.Name))
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			generatedName := objectnames.GetComponentName(nroObj.Name, mcp.Name)
			klog.Infof("generated config map name: %q", generatedName)
			cm, err := f.ClientSet.CoreV1().ConfigMaps(namespace).Get(context.TODO(), generatedName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			desiredMapState := make(map[string]string)
			for k, v := range cm.Data {
				desiredMapState[k] = v
			}

			cm.Data = nil
			cm, err = f.ClientSet.CoreV1().ConfigMaps(namespace).Update(context.TODO(), cm, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				cm, err = f.ClientSet.CoreV1().ConfigMaps(namespace).Get(context.TODO(), generatedName, metav1.GetOptions{})
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
		nrtList, err := nrtcli.TopologyV1alpha1().NodeResourceTopologies().List(context.TODO(), metav1.ListOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		for _, nrt := range nrtList.Items {
			pfp, ok := nrt.Annotations[podfingerprint.Annotation]
			gomega.Expect(ok).To(gomega.BeTrue(), "missing podfingerprint annotation %q from NRT %q", podfingerprint.Annotation, nrt.Name)

			seemsValid := strings.HasPrefix(pfp, podfingerprint.Prefix)
			gomega.Expect(seemsValid).To(gomega.BeTrue(), "malformed podfingerprint %q from NRT %q", pfp, nrt.Name)
		}
	})

	ginkgo.It("[rte][podfingerprint] should expose the pod set fingerprint status on each worker", func() {
		nropObj, err := nropcli.NUMAResourcesOperators().Get(context.TODO(), objectnames.DefaultNUMAResourcesOperatorCrName, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		rteDss, err := getOwnedDss(f, nropObj.ObjectMeta)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(rteDss).ToNot(gomega.BeEmpty(), "no RTE DS found")

		for _, rteDs := range rteDss {
			ginkgo.By(fmt.Sprintf("checking DS: %s/%s", rteDs.Namespace, rteDs.Name))

			rtePods, err := podlistByDaemonset(f, rteDs)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(rtePods).ToNot(gomega.BeEmpty(), "no RTE pods found for %s/%s", rteDs.Namespace, rteDs.Name)

			for _, rtePod := range rtePods {
				ginkgo.By(fmt.Sprintf("checking DS: %s/%s POD %s/%s", rteDs.Namespace, rteDs.Name, rtePod.Namespace, rtePod.Name))

				rteCnt, err := rteupdate.FindContainerByName(&rtePod.Spec, rteupdate.MainContainerName)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// TODO: hardcoded path. Any smarter option?
				cmd := []string{"/bin/cat", "/run/pfpstatus/dump.json"}
				stdout, stderr, err := f.ExecWithOptions(framework.ExecOptions{
					Command:            cmd,
					Namespace:          rtePod.Namespace,
					PodName:            rtePod.Name,
					ContainerName:      rteCnt.Name,
					Stdin:              nil,
					CaptureStdout:      true,
					CaptureStderr:      true,
					PreserveWhitespace: false,
				})
				gomega.Expect(err).ToNot(gomega.HaveOccurred(), "err=%v stderr=%s", err, stderr)

				var st podfingerprint.Status
				err = json.Unmarshal([]byte(stdout), &st)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				klog.Infof("got status from %s/%s/%s -> %q (%d pods)", rtePod.Namespace, rtePod.Name, rteCnt.Name, st.FingerprintComputed, len(st.Pods))

				gomega.Expect(st.FingerprintComputed).ToNot(gomega.BeEmpty(), "missing fingerprint - should always be reported")
				gomega.Expect(st.Pods).ToNot(gomega.BeEmpty(), "missing pods - at least RTE itself should be there")
			}
		}
	})
})

func getOwnedDss(f *framework.Framework, owner metav1.ObjectMeta) ([]appsv1.DaemonSet, error) {
	dss, err := f.ClientSet.AppsV1().DaemonSets("").List(context.TODO(), metav1.ListOptions{})
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

func podlistByDaemonset(f *framework.Framework, ds appsv1.DaemonSet) ([]corev1.Pod, error) {
	sel, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
	if err != nil {
		return nil, err
	}

	podList, err := f.ClientSet.CoreV1().Pods(ds.Namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}
