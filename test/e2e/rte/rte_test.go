/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rte

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ghodss/yaml"
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"k8s.io/kubernetes/test/e2e/framework"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	mcov1cli "github.com/openshift/machine-config-operator/pkg/generated/clientset/versioned/typed/machineconfiguration.openshift.io/v1"

	"github.com/openshift-kni/numaresources-operator/pkg/flagcodec"
	nropv1alpha1cli "github.com/openshift-kni/numaresources-operator/pkg/k8sclientset/generated/clientset/versioned/typed/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
	mcpfind "github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools/find"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"

	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

const defaultNUMAResourcesOperatorCrName = "numaresourcesoperator"

var _ = ginkgo.Describe("with a running cluster with all the components", func() {
	var (
		initialized bool
		nropcli     *nropv1alpha1cli.NumaresourcesoperatorV1alpha1Client
		mcocli      *mcov1cli.MachineconfigurationV1Client
	)

	f := framework.NewDefaultFramework("rte")

	ginkgo.BeforeEach(func() {
		if !initialized {
			var err error

			nropcli, err = newNUMAResourcesOperatorWithConfig(f.ClientConfig())
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			mcocli, err = newMachineConfigClientWithConfig(f.ClientConfig())
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			initialized = true
		}
	})

	ginkgo.When("[config][rte] NRO CR configured with LogLevel", func() {
		ginkgo.It("should have the corresponding klog under RTE container", func() {
			nropObj, err := nropcli.NUMAResourcesOperators().Get(context.TODO(), defaultNUMAResourcesOperatorCrName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			rteDss, err := getOwnedDss(f, nropObj.ObjectMeta)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(rteDss)).ToNot(gomega.BeZero())

			for _, ds := range rteDss {
				// TODO better match by name than assumes container #0 is the right one
				rteCnt := &ds.Spec.Template.Spec.Containers[0]
				found, match := matchLogLevelToKlog(rteCnt, nropObj.Spec.LogLevel)
				gomega.Expect(found).To(gomega.BeTrue(), fmt.Sprintf("--v flag doesn't exist in container %s args", rteCnt.Name))
				gomega.Expect(match).To(gomega.BeTrue(), fmt.Sprintf("LogLevel %s doesn't match the existing --v flag under %s container", nropObj.Spec.LogLevel, rteCnt))
			}
		})

		ginkgo.It("can modify the LogLevel in NRO CR and klog under RTE container should change respectively", func() {
			nropObj, err := nropcli.NUMAResourcesOperators().Get(context.TODO(), defaultNUMAResourcesOperatorCrName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			nropObj.Spec.LogLevel = operatorv1.Trace
			nropObj, err = nropcli.NUMAResourcesOperators().Update(context.TODO(), nropObj, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			rteDss, err := getOwnedDss(f, nropObj.ObjectMeta)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(rteDss)).ToNot(gomega.BeZero())

			for _, ds := range rteDss {
				// TODO better match by name than assumes container #0 is the right one
				rteCnt := &ds.Spec.Template.Spec.Containers[0]
				found, match := matchLogLevelToKlog(rteCnt, nropObj.Spec.LogLevel)
				gomega.Expect(found).To(gomega.BeTrue(), fmt.Sprintf("--v flag doesn't exist in container %s args", rteCnt.Name))
				gomega.Expect(match).To(gomega.BeTrue(), fmt.Sprintf("LogLevel %s doesn't match the existing --v flag under %s container", nropObj.Spec.LogLevel, rteCnt))
			}
		})
	})

	ginkgo.When("[config][kubelet][rte] Kubelet Config includes reservations", func() {
		ginkgo.It("should configure RTE accordingly", func() {
			nroObj, err := nropcli.NUMAResourcesOperators().Get(context.TODO(), defaultNUMAResourcesOperatorCrName, metav1.GetOptions{})
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

				mcps, err := mcpfind.NodeGroupsMCPs(mcpList, nroObj.Spec.NodeGroups)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				mcp, err := mcpfind.MCPBySelector(mcps, mcoKc.Spec.MachineConfigPoolSelector)
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
	})
})

func newMachineConfigClientWithConfig(cfg *rest.Config) (*mcov1cli.MachineconfigurationV1Client, error) {
	clientset, err := mcov1cli.NewForConfig(cfg)
	if err != nil {
		klog.Exit(err.Error())
	}
	return clientset, nil
}

func newNUMAResourcesOperatorWithConfig(cfg *rest.Config) (*nropv1alpha1cli.NumaresourcesoperatorV1alpha1Client, error) {
	clientset, err := nropv1alpha1cli.NewForConfig(cfg)
	if err != nil {
		klog.Exit(err.Error())
	}
	return clientset, nil
}

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
