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

package controllers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"

	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
)

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
	}, nil
}

var _ = Describe("Test KubeletConfig Reconcile", func() {
	Context("with KubeletConfig objects already present in the cluster", func() {
		var nro *nropv1.NUMAResourcesOperator
		var mcp1 *machineconfigv1.MachineConfigPool
		var mcoKc1 *machineconfigv1.KubeletConfig
		var label1 map[string]string

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
		})

		Context("on the first iteration", func() {
			It("without NRO present, should wait", func() {
				reconciler, err := NewFakeKubeletConfigReconciler(mcp1, mcoKc1)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(mcoKc1)
				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{RequeueAfter: kubeletConfigRetryPeriod}))
			})
			It("with NRO present, should create configmap", func() {
				reconciler, err := NewFakeKubeletConfigReconciler(nro, mcp1, mcoKc1)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(mcoKc1)
				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))

				cm := &corev1.ConfigMap{}
				key = client.ObjectKey{
					Namespace: testNamespace,
					Name:      objectnames.GetComponentName(nro.Name, mcp1.Name),
				}
				Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(HaveOccurred())
			})
			It("with NRO present, the created configmap should have the linking labels", func() {
				reconciler, err := NewFakeKubeletConfigReconciler(nro, mcp1, mcoKc1)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(mcoKc1)
				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))

				cm := &corev1.ConfigMap{}
				key = client.ObjectKey{
					Namespace: testNamespace,
					Name:      objectnames.GetComponentName(nro.Name, mcp1.Name),
				}
				Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(HaveOccurred())
				Expect(cm.Labels).To(HaveKeyWithValue(rteconfig.LabelOperatorName, nro.Name))
				Expect(cm.Labels).To(HaveKeyWithValue(rteconfig.LabelNodeGroupName+"/"+rteconfig.LabelNodeGroupKindMachineConfigPool, mcp1.Name))
			})
			It("should send events when NRO present and operation succesfull", func() {
				reconciler, err := NewFakeKubeletConfigReconciler(nro, mcp1, mcoKc1)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(mcoKc1)
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
				reconciler, err := NewFakeKubeletConfigReconciler(nro, mcp1, brokenMcoKc)
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
				reconciler, err := NewFakeKubeletConfigReconciler(nro, mcp1, invalidMcoKc)
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

				reconciler, err := NewFakeKubeletConfigReconciler(nro, mcp1, mcoKc1, ctrlPlaneKc)
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
				reconciler, err := NewFakeKubeletConfigReconciler(nro, mcp1)
				Expect(err).ToNot(HaveOccurred())

				fakeRecorder, ok := reconciler.Recorder.(*record.FakeRecorder)
				Expect(ok).To(BeTrue())

				err = reconciler.Client.Create(context.TODO(), mcoKc1)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(mcoKc1)
				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))

				cm := &corev1.ConfigMap{}
				key = client.ObjectKey{
					Namespace: testNamespace,
					Name:      objectnames.GetComponentName(nro.Name, mcp1.Name),
				}
				Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(HaveOccurred())
				// verify creation event
				event := <-fakeRecorder.Events
				Expect(event).To(ContainSubstring("ProcessOK"))
				Expect(event).To(ContainSubstring(mcoKc1.Name))

				ctrlPlaneKc := testobjs.NewKubeletConfigAutoresizeControlPlane()
				err = reconciler.Client.Create(context.TODO(), ctrlPlaneKc)
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
	})
})
