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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type reconcilerBuilderFunc func(...runtime.Object) (*KubeletConfigReconciler, error)

const (
	bufferSize = 1024
)

func NewFakeKubeletConfigReconciler(initObjects ...runtime.Object) (*KubeletConfigReconciler, error) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(initObjects...).Build()
	return &KubeletConfigReconciler{
		Client:    fakeClient,
		Scheme:    scheme.Scheme,
		Namespace: testNamespace,
		Recorder:  record.NewFakeRecorder(bufferSize),
		Platform:  platform.OpenShift,
	}, nil
}

func NewFakeKubeletConfigReconcilerForHyperShift(initObjects ...runtime.Object) (*KubeletConfigReconciler, error) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(initObjects...).Build()
	return &KubeletConfigReconciler{
		Client:    fakeClient,
		Scheme:    scheme.Scheme,
		Namespace: testNamespace,
		Recorder:  record.NewFakeRecorder(bufferSize),
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
			It("should send events when NRO present and operation successful", func() {
				reconciler, err := newFakeReconciler(nro, mcp1, mcoKc1, cmKc1)
				Expect(err).ToNot(HaveOccurred())

				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))

				// verify creation event
				fakeRecorder, ok := reconciler.Recorder.(*record.FakeRecorder)
				Expect(ok).To(BeTrue())
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("ProcessOK"))
			})

			It("should send events when NRO present and operation failure", func() {
				brokenMcoKc := testobjs.NewKubeletConfigWithData("broken", label1, mcp1.Spec.MachineConfigSelector, []byte(""))
				// on HyperShift we can mimic this behavior by not having a ConfigMap with a KubeletConfig
				// present on the cluster at all
				reconciler, err := newFakeReconciler(nro, mcp1, brokenMcoKc)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(brokenMcoKc)
				_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).To(HaveOccurred())

				// verify creation event
				fakeRecorder, ok := reconciler.Recorder.(*record.FakeRecorder)
				Expect(ok).To(BeTrue())
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("ProcessFailed"))
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

				// verify creation event
				fakeRecorder, ok := reconciler.Recorder.(*record.FakeRecorder)
				Expect(ok).To(BeTrue())
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("ProcessSkip"))
				Expect(event).To(ContainSubstring(invalidMcoKc.Name))
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

				// verify creation event
				fakeRecorder, ok := reconciler.Recorder.(*record.FakeRecorder)
				Expect(ok).To(BeTrue())
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("ProcessSkip"))
				Expect(event).To(ContainSubstring(ctrlPlaneKc.Name))
			})

			It("should process matching kubeletconfig, then ignore non-matching kubeketconfig", func() {
				reconciler, err := newFakeReconciler(nro, mcp1)
				Expect(err).ToNot(HaveOccurred())

				fakeRecorder, ok := reconciler.Recorder.(*record.FakeRecorder)
				Expect(ok).To(BeTrue())

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
				// verify creation event
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("ProcessOK"))
				Expect(event).To(ContainSubstring(reconciledObj.GetName()))

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

				// verify creation event
				event = <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("ProcessSkip"))
				Expect(event).To(ContainSubstring(ctrlPlaneKc.Name))
			})
		})
	},
		Entry("OpenShift Platform", NewFakeKubeletConfigReconciler, platform.OpenShift),
		Entry("HyperShift Platform", NewFakeKubeletConfigReconcilerForHyperShift, platform.HyperShift),
	)

	Context("kubeletconfig updates with paused MCPs", func() {
		var nro *nropv1.NUMAResourcesOperator
		var mcp1, mcpPaused *machineconfigv1.MachineConfigPool
		var mcoKC1, mcoKCPaused *machineconfigv1.KubeletConfig
		var label1 map[string]string
		var kc1Key, kc2Key client.ObjectKey
		var currentMachineConfig *machineconfigv1.MachineConfig
		var poolName1, poolName2 string
		var reconciler *KubeletConfigReconciler
		var err error

		BeforeEach(func() {
			label1 = map[string]string{
				"test1": "test1",
			}
			mcp1 = testobjs.NewMachineConfigPool("test1", label1, &metav1.LabelSelector{MatchLabels: label1}, &metav1.LabelSelector{MatchLabels: label1})
			poolName1 = mcp1.Name
			kubeletConfig := &kubeletconfigv1beta1.KubeletConfiguration{}
			mcoKC1 = testobjs.NewKubeletConfig(poolName1, label1, mcp1.Spec.MachineConfigSelector, kubeletConfig)
			kc1Key = client.ObjectKeyFromObject(mcoKC1)

			label2 := map[string]string{
				"test2": "test2",
			}
			mcpPaused = testobjs.NewMachineConfigPool("test2", label2, &metav1.LabelSelector{MatchLabels: label2}, &metav1.LabelSelector{MatchLabels: label2})
			mcpPaused.Spec.Paused = true
			poolName2 = mcpPaused.Name
			kubeletConfigPaused := &kubeletconfigv1beta1.KubeletConfiguration{
				TopologyManagerPolicy: "restricted",
				TopologyManagerScope:  "container",
			}
			mcoKCPaused = testobjs.NewKubeletConfig(poolName2, label2, mcpPaused.Spec.MachineConfigSelector, kubeletConfigPaused)
			kc2Key = client.ObjectKeyFromObject(mcoKCPaused)
			currentMachineConfig = &machineconfigv1.MachineConfig{
				// this is a fake machineconfig that will be used to as the old machineconfig for the paused MCP
				// it currently uses single-numa-node policy and pod scope
				ObjectMeta: metav1.ObjectMeta{
					Name: "test2-mc",
				},
				Spec: machineconfigv1.MachineConfigSpec{
					Config: runtime.RawExtension{
						Raw: []byte(`{
				"ignition": {
					"version": "3.2.0"
				},
				"storage": {
					"files": [
						{
							"contents": {
								"source": "data:text/plain;charset=utf-8;base64,a2luZDogS3ViZWxldENvbmZpZ3VyYXRpb24KdG9wb2xvZ3lNYW5hZ2VyUG9saWN5OiBzaW5nbGUtbnVtYS1ub2RlCnRvcG9sb2d5TWFuYWdlclNjb3BlOiBwb2QK"
							},
							"mode": 420,
							"overwrite": true,
							"path": "/etc/kubernetes/kubelet.conf"
						}
					]
				}
			}`),
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
		It("should create configmap for active MCP", func() {
			result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: kc1Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
			cm := &corev1.ConfigMap{}
			key := client.ObjectKey{
				Namespace: testNamespace,
				Name:      objectnames.GetComponentName(nro.Name, poolName1),
			}
			Expect(reconciler.Client.Get(context.TODO(), key, cm)).To(Succeed())
		})

		It("should not create configmap for paused MCP if the current machineconfig is not found", func() {
			result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{Requeue: true, RequeueAfter: MachineConfigPoolPausedRetryPeriod}))

			fakeRecorder, ok := reconciler.Recorder.(*record.FakeRecorder)
			Expect(ok).To(BeTrue())
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("ProcessSkip"))
			Expect(event).To(ContainSubstring("failed to find the current machineConfig"))

			cm := &corev1.ConfigMap{}
			key := client.ObjectKey{
				Namespace: testNamespace,
				Name:      objectnames.GetComponentName(nro.Name, poolName2),
			}
			Expect(reconciler.Client.Get(context.TODO(), key, cm)).To(HaveOccurred())
		})
		It("should create configmap for paused MCP when machineconfig exists, with requeue", func() {
			Expect(reconciler.Client.Create(context.TODO(), currentMachineConfig)).To(Succeed())
			result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{Requeue: true, RequeueAfter: MachineConfigPoolPausedRetryPeriod}))

			fakeRecorder, ok := reconciler.Recorder.(*record.FakeRecorder)
			Expect(ok).To(BeTrue())
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("ProcessOK"))
			Expect(event).To(ContainSubstring("Updated RTE config"))

			cm := &corev1.ConfigMap{}
			key := client.ObjectKey{
				Namespace: testNamespace,
				Name:      objectnames.GetComponentName(nro.Name, poolName2),
			}
			Expect(reconciler.Client.Get(context.TODO(), key, cm)).To(Succeed())

			cmData, ok := cm.Data["config.yaml"]
			Expect(ok).To(BeTrue())
			Expect(cmData).To(ContainSubstring("single-numa-node"))
			Expect(cmData).To(ContainSubstring("pod"))
		})

		It("should update configmap for paused MCP when MCP is unpaused", func() {
			Expect(reconciler.Client.Create(context.TODO(), currentMachineConfig)).To(Succeed())
			result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{Requeue: true, RequeueAfter: MachineConfigPoolPausedRetryPeriod}))

			clonedMCP := mcpPaused.DeepCopy()
			clonedMCP.Spec.Paused = false
			Expect(reconciler.Client.Update(context.TODO(), clonedMCP)).To(Succeed())

			result, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: kc2Key})
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			cm := &corev1.ConfigMap{}
			key := client.ObjectKey{
				Namespace: testNamespace,
				Name:      objectnames.GetComponentName(nro.Name, poolName2),
			}
			Expect(reconciler.Client.Get(context.TODO(), key, cm)).To(Succeed())

			fakeRecorder, ok := reconciler.Recorder.(*record.FakeRecorder)
			Expect(ok).To(BeTrue())
			event := <-fakeRecorder.Events
			Expect(event).To(ContainSubstring("ProcessOK"))
			Expect(event).To(ContainSubstring(mcoKCPaused.Name))

			cmData, ok := cm.Data["config.yaml"]
			Expect(ok).To(BeTrue())
			Expect(cmData).To(ContainSubstring("restricted"))
			Expect(cmData).To(ContainSubstring("container"))
		})
	})

})
