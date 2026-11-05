/*
 * Copyright 2021 Red Hat, Inc.
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

package controller

import (
	"context"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/platform"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type reconcilerBuilderFunc func(...runtime.Object) (*KubeletConfigReconciler, error)

func NewFakeKubeletConfigReconciler(initObjects ...runtime.Object) (*KubeletConfigReconciler, error) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(initObjects...).Build()
	return &KubeletConfigReconciler{
		Client:    fakeClient,
		Scheme:    scheme.Scheme,
		Namespace: testNamespace,
		Platform:  platform.OpenShift,
	}, nil
}

func NewFakeKubeletConfigReconcilerForHyperShift(initObjects ...runtime.Object) (*KubeletConfigReconciler, error) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(initObjects...).Build()
	return &KubeletConfigReconciler{
		Client:    fakeClient,
		Scheme:    scheme.Scheme,
		Namespace: testNamespace,
		Platform:  platform.HyperShift,
	}, nil
}

var _ = Describe("Test KubeletConfig Reconcile", func() {
	DescribeTableSubtree("On different platforms with KubeletConfig objects already present in the cluster", func(newFakeReconciler reconcilerBuilderFunc, clusterPlatform platform.Platform) {
		var nro *nropv1.NUMAResourcesOperator
		var mcp1 *machineconfigv1.MachineConfigPool
		var mcoKc1 *machineconfigv1.KubeletConfig
		var label1 map[string]string
		var key client.ObjectKey
		var poolName string
		cmKc1 := &corev1.ConfigMap{}

		BeforeEach(func() {
			label1 = map[string]string{
				"test1": "test1",
			}
			mcp1 = testobjs.NewMachineConfigPool("test1", label1, &metav1.LabelSelector{MatchLabels: label1}, &metav1.LabelSelector{MatchLabels: label1})
			ng := nropv1.NodeGroup{
				MachineConfigPoolSelector: &metav1.LabelSelector{
					MatchLabels: label1,
				},
			}
			nro = testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng)
			kubeletConfig := &kubeletconfigv1beta1.KubeletConfiguration{}
			mcoKc1 = testobjs.NewKubeletConfig("test1", label1, mcp1.Spec.MachineConfigSelector, kubeletConfig)
			key = client.ObjectKeyFromObject(mcoKc1)
			poolName = mcp1.Name

			if clusterPlatform == platform.HyperShift {
				poolName = "test-hostedcluster1"
				label1[HyperShiftNodePoolLabel] = poolName
				cmKc1 = testobjs.NewKubeletConfigConfigMap("test1", label1, mcoKc1)
				key = client.ObjectKeyFromObject(cmKc1)
			}
		})

		Context("on the first iteration", func() {
			It("without NRO present, should wait", func() {
				reconciler, err := newFakeReconciler(mcp1, mcoKc1, cmKc1)
				Expect(err).ToNot(HaveOccurred())

				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{RequeueAfter: kubeletConfigRetryPeriod}))
			})
			It("with NRO present, should create configmap", func() {
				reconciler, err := newFakeReconciler(nro, mcp1, mcoKc1, cmKc1)
				Expect(err).ToNot(HaveOccurred())

				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))

				cm := &corev1.ConfigMap{}
				key = client.ObjectKey{
					Namespace: testNamespace,
					Name:      objectnames.GetComponentName(nro.Name, poolName),
				}
				Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(HaveOccurred())
			})
			It("with NRO present, the created configmap should have the linking labels", func() {
				reconciler, err := newFakeReconciler(nro, mcp1, mcoKc1, cmKc1)
				Expect(err).ToNot(HaveOccurred())

				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))

				cm := &corev1.ConfigMap{}
				key = client.ObjectKey{
					Namespace: testNamespace,
					Name:      objectnames.GetComponentName(nro.Name, poolName),
				}
				Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(HaveOccurred())
				Expect(cm.Labels).To(HaveKeyWithValue(rteconfig.LabelOperatorName, nro.Name))
				Expect(cm.Labels).To(HaveKeyWithValue(rteconfig.LabelNodeGroupName+"/"+rteconfig.LabelNodeGroupKindMachineConfigPool, poolName))
			})
			It("should fail when NRO present and operation failure", func() {
				brokenMcoKc := testobjs.NewKubeletConfigWithData("broken", label1, mcp1.Spec.MachineConfigSelector, []byte(""))
				// on HyperShift we can mimic this behavior by not having a ConfigMap with a KubeletConfig
				// present on the cluster at all
				reconciler, err := newFakeReconciler(nro, mcp1, brokenMcoKc)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(brokenMcoKc)
				_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).To(HaveOccurred())
			})

			It("should skip invalid kubeletconfig", func() {
				invalidMcoKc := testobjs.NewKubeletConfigWithoutData("payloadless", label1, mcp1.Spec.MachineConfigSelector)
				// adding a CM for when this test emulates HyperShift platform
				invalidCmMcoKc := testobjs.NewKubeletConfigConfigMap("payloadless", label1, invalidMcoKc)
				reconciler, err := newFakeReconciler(nro, mcp1, invalidMcoKc, invalidCmMcoKc)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(invalidMcoKc)
				_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
			})

			It("should ignore non-matching kubeketconfigs", func() {
				ctrlPlaneKc := testobjs.NewKubeletConfigAutoresizeControlPlane()
				// adding a CM for when this test emulates HyperShift platform
				ctrlPlaneCmKc := testobjs.NewKubeletConfigConfigMap(ctrlPlaneKc.Name, label1, ctrlPlaneKc)
				reconciler, err := newFakeReconciler(nro, mcp1, mcoKc1, ctrlPlaneKc, ctrlPlaneCmKc)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(ctrlPlaneKc)
				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))
			})

			It("should process matching kubeletconfig, then ignore non-matching kubeketconfig", func() {
				reconciler, err := newFakeReconciler(nro, mcp1)
				Expect(err).ToNot(HaveOccurred())

				var reconciledObj client.Object
				reconciledObj = mcoKc1
				if clusterPlatform == platform.HyperShift {
					reconciledObj = cmKc1
				}
				err = reconciler.Client.Create(context.TODO(), reconciledObj)
				Expect(err).ToNot(HaveOccurred())

				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))

				cm := &corev1.ConfigMap{}
				key = client.ObjectKey{
					Namespace: testNamespace,
					Name:      objectnames.GetComponentName(nro.Name, poolName),
				}
				Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(HaveOccurred())

				ctrlPlaneKc := testobjs.NewKubeletConfigAutoresizeControlPlane()
				err = reconciler.Client.Create(context.TODO(), ctrlPlaneKc)
				Expect(err).ToNot(HaveOccurred())

				// adding a CM for when this test emulates HyperShift platform
				ctrlPlaneCmKc := testobjs.NewKubeletConfigConfigMap(ctrlPlaneKc.Name, label1, ctrlPlaneKc)
				err = reconciler.Client.Create(context.TODO(), ctrlPlaneCmKc)
				Expect(err).ToNot(HaveOccurred())

				key = client.ObjectKeyFromObject(ctrlPlaneKc)
				result, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))
			})
		})
	},
		Entry("OpenShift Platform", NewFakeKubeletConfigReconciler, platform.OpenShift),
		Entry("HyperShift Platform", NewFakeKubeletConfigReconcilerForHyperShift, platform.HyperShift),
	)

	Context("kubeletconfig updates with paused MCPs", func() {
		const (
			test1 = "test1"
			test2 = "test2"
		)

		var (
			nro                  *nropv1.NUMAResourcesOperator
			mcp1, mcpPaused      *machineconfigv1.MachineConfigPool
			mcoKC1, mcoKCPaused  *machineconfigv1.KubeletConfig
			label1               map[string]string
			kc1Key, kc2Key       client.ObjectKey
			currentMachineConfig *machineconfigv1.MachineConfig
			poolName1, poolName2 string
			reconciler           *KubeletConfigReconciler
			err                  error
		)

		BeforeEach(func() {
			label1 = map[string]string{
				test1: test1,
			}
			mcp1 = testobjs.NewMachineConfigPool(test1, label1, &metav1.LabelSelector{MatchLabels: label1}, &metav1.LabelSelector{MatchLabels: label1})
			poolName1 = mcp1.Name
			kubeletConfig := &kubeletconfigv1beta1.KubeletConfiguration{}
			mcoKC1 = testobjs.NewKubeletConfig(poolName1, label1, mcp1.Spec.MachineConfigSelector, kubeletConfig)
			kc1Key = client.ObjectKeyFromObject(mcoKC1)

			label2 := map[string]string{
				test2: test2,
			}
			mcpPaused = testobjs.NewMachineConfigPool(test2, label2, &metav1.LabelSelector{MatchLabels: label2}, &metav1.LabelSelector{MatchLabels: label2})
			mcpPaused.Spec.Paused = true
			poolName2 = mcpPaused.Name
			kubeletConfigPaused := &kubeletconfigv1beta1.KubeletConfiguration{
				TopologyManagerPolicy: "restricted",
				TopologyManagerScope:  "container",
			}
			mcoKCPaused = testobjs.NewKubeletConfig(poolName2, label2, mcpPaused.Spec.MachineConfigSelector, kubeletConfigPaused)
			mcoKCPaused.UID = "kubeletconfig-paused-uid"
			kc2Key = client.ObjectKeyFromObject(mcoKCPaused)
			// active MachineConfig for the paused MCP still carries the older kubelet settings
			activeKubeletYAML := `kind: KubeletConfiguration
apiVersion: kubelet.config.k8s.io/v1beta1
topologyManagerPolicy: single-numa-node
topologyManagerScope: pod
`
			encodedActiveKubelet := base64.StdEncoding.EncodeToString([]byte(activeKubeletYAML))
			currentMachineConfig = &machineconfigv1.MachineConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test2-mc",
				},
				Spec: machineconfigv1.MachineConfigSpec{
					Config: runtime.RawExtension{
						Raw: []byte(fmt.Sprintf(`{
				"ignition": {
					"version": "3.2.0"
				},
				"storage": {
					"files": [
						{
							"contents": {
								"source": "data:text/plain;charset=utf-8;base64,%s"
							},
							"mode": 420,
							"overwrite": true,
							"path": "/etc/kubernetes/kubelet.conf"
						}
					]
				}
			}`, encodedActiveKubelet)),
					},
				},
			}
			// we want a state where the active MachineConfig of a paused MCP is old
			// intentionally because the MCP would be paused, so we can test that the old
			// data is reflected in the generatedConfigMap
			mcpPaused.Status.Configuration.Name = currentMachineConfig.Name

			ng1 := nropv1.NodeGroup{
				PoolName: &poolName1,
			}
			ng2 := nropv1.NodeGroup{
				PoolName: &poolName2,
			}
			nro = testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng1, ng2)

			reconciler, err = NewFakeKubeletConfigReconciler(nro, mcp1, mcoKC1, mcpPaused, mcoKCPaused)
			Expect(err).ToNot(HaveOccurred())
		})
		It("should create configmap for active MCP", func(ctx context.Context) {
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: kc1Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
			cm := &corev1.ConfigMap{}
			key := client.ObjectKey{
				Namespace: testNamespace,
				Name:      objectnames.GetComponentName(nro.Name, poolName1),
			}
			Expect(reconciler.Client.Get(ctx, key, cm)).To(Succeed())
		})

		It("should not create configmap for paused MCP if the current machineconfig is not found", func(ctx context.Context) {
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: kc2Key})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot find the machineConfig test2-mc"))
			Expect(result).To(Equal(reconcile.Result{}))

			cm := &corev1.ConfigMap{}
			key := client.ObjectKey{
				Namespace: testNamespace,
				Name:      objectnames.GetComponentName(nro.Name, poolName2),
			}
			Expect(reconciler.Client.Get(ctx, key, cm)).To(HaveOccurred())
		})
		It("should create configmap for paused MCP when machineconfig exists", func(ctx context.Context) {
			Expect(reconciler.Client.Create(ctx, currentMachineConfig)).To(Succeed())
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			cm := &corev1.ConfigMap{}
			key := client.ObjectKey{
				Namespace: testNamespace,
				Name:      objectnames.GetComponentName(nro.Name, poolName2),
			}
			Expect(reconciler.Client.Get(ctx, key, cm)).To(Succeed())

			cmData, ok := cm.Data["config.yaml"]
			Expect(ok).To(BeTrue())
			Expect(cmData).To(ContainSubstring("single-numa-node"))
			Expect(cmData).To(ContainSubstring("pod"))

			ownerRef := metav1.GetControllerOf(cm)
			Expect(ownerRef).NotTo(BeNil())
			Expect(ownerRef.Name).To(Equal(mcoKCPaused.Name))
			Expect(ownerRef.UID).To(Equal(mcoKCPaused.UID))
		})

		It("should rewrite stale configmap for paused MCP to match active machineconfig", func(ctx context.Context) {
			Expect(reconciler.Client.Create(ctx, currentMachineConfig)).To(Succeed())
			staleCM := testobjs.NewRTEConfigMap(objectnames.GetComponentName(nro.Name, poolName2), testNamespace, "restricted", "container")
			Expect(reconciler.Client.Create(ctx, staleCM)).To(Succeed())

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			cm := &corev1.ConfigMap{}
			key := client.ObjectKey{
				Namespace: testNamespace,
				Name:      objectnames.GetComponentName(nro.Name, poolName2),
			}
			Expect(reconciler.Client.Get(ctx, key, cm)).To(Succeed())
			cmData, ok := cm.Data["config.yaml"]
			Expect(ok).To(BeTrue())
			Expect(cmData).To(ContainSubstring("single-numa-node"))
			Expect(cmData).To(ContainSubstring("pod"))
			Expect(cmData).NotTo(ContainSubstring("restricted"))
		})

		It("should keep matching configmap for paused MCP", func(ctx context.Context) {
			Expect(reconciler.Client.Create(ctx, currentMachineConfig)).To(Succeed())
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			cm := &corev1.ConfigMap{}
			key := client.ObjectKey{
				Namespace: testNamespace,
				Name:      objectnames.GetComponentName(nro.Name, poolName2),
			}
			Expect(reconciler.Client.Get(ctx, key, cm)).To(Succeed())
			resourceVersion := cm.ResourceVersion

			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
			Expect(reconciler.Client.Get(ctx, key, cm)).To(Succeed())
			Expect(cm.ResourceVersion).To(Equal(resourceVersion))
		})

		It("should include podExcludes in configmap for paused MCP", func(ctx context.Context) {
			nro.Spec.PodExcludes = []nropv1.NamespacedName{
				{Namespace: "openshift-*", Name: "node-*"},
			}
			reconciler, err = NewFakeKubeletConfigReconciler(nro, mcp1, mcoKC1, mcpPaused, mcoKCPaused, currentMachineConfig)
			Expect(err).ToNot(HaveOccurred())

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			cm := &corev1.ConfigMap{}
			key := client.ObjectKey{
				Namespace: testNamespace,
				Name:      objectnames.GetComponentName(nro.Name, poolName2),
			}
			Expect(reconciler.Client.Get(ctx, key, cm)).To(Succeed())
			cmData, ok := cm.Data[rteconfig.Key]
			Expect(ok).To(BeTrue())
			Expect(cmData).To(ContainSubstring("single-numa-node"))
			Expect(cmData).To(ContainSubstring("podExclude"))
			Expect(cmData).To(ContainSubstring("openshift-*"))
			Expect(cmData).To(ContainSubstring("node-*"))
		})

		It("should rewrite paused MCP configmap when podExcludes change", func(ctx context.Context) {
			Expect(reconciler.Client.Create(ctx, currentMachineConfig)).To(Succeed())
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			cm := &corev1.ConfigMap{}
			key := client.ObjectKey{
				Namespace: testNamespace,
				Name:      objectnames.GetComponentName(nro.Name, poolName2),
			}
			Expect(reconciler.Client.Get(ctx, key, cm)).To(Succeed())
			Expect(cm.Data[rteconfig.Key]).NotTo(ContainSubstring("podExclude"))

			Expect(reconciler.Client.Get(ctx, client.ObjectKeyFromObject(nro), nro)).To(Succeed())
			nro.Spec.PodExcludes = []nropv1.NamespacedName{
				{Namespace: "openshift-*", Name: "node-*"},
			}
			Expect(reconciler.Client.Update(ctx, nro)).To(Succeed())

			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			Expect(reconciler.Client.Get(ctx, key, cm)).To(Succeed())
			cmData := cm.Data[rteconfig.Key]
			Expect(cmData).To(ContainSubstring("single-numa-node"))
			Expect(cmData).To(ContainSubstring("podExclude"))
			Expect(cmData).To(ContainSubstring("openshift-*"))
			Expect(cmData).To(ContainSubstring("node-*"))
		})

		It("should update configmap for paused MCP when MCP is unpaused", func(ctx context.Context) {
			Expect(reconciler.Client.Create(ctx, currentMachineConfig)).To(Succeed())
			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			clonedMCP := mcpPaused.DeepCopy()
			clonedMCP.Spec.Paused = false
			Expect(reconciler.Client.Update(ctx, clonedMCP)).To(Succeed())

			result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			cm := &corev1.ConfigMap{}
			key := client.ObjectKey{
				Namespace: testNamespace,
				Name:      objectnames.GetComponentName(nro.Name, poolName2),
			}
			Expect(reconciler.Client.Get(ctx, key, cm)).To(Succeed())

			cmData, ok := cm.Data["config.yaml"]
			Expect(ok).To(BeTrue())
			Expect(cmData).To(ContainSubstring("restricted"))
			Expect(cmData).To(ContainSubstring("container"))
		})
	})
})
