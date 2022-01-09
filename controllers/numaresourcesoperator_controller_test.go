/*
Copyright 2021.

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

package controllers

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	securityv1 "github.com/openshift/api/security/v1"
	appsv1 "k8s.io/api/apps/v1"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	"github.com/k8stopologyawareschedwg/deployer/pkg/tlog"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nrov1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/pkg/testutils"
	"github.com/openshift-kni/numaresources-operator/pkg/validation"
)

const (
	testImageSpec = "quay.io/openshift-kni/numaresources-operator:ci-test"
)

func NewFakeNUMAResourcesOperatorReconciler(plat platform.Platform, initObjects ...runtime.Object) (*NUMAResourcesOperatorReconciler, error) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(initObjects...).Build()
	helper := deployer.NewHelperWithClient(fakeClient, "", tlog.NewNullLogAdapter())
	apiManifests, err := apimanifests.GetManifests(plat)
	if err != nil {
		return nil, err
	}

	rteManifests, err := rtemanifests.GetManifests(plat, testNamespace)
	if err != nil {
		return nil, err
	}

	recorder := record.NewFakeRecorder(bufferSize)

	return &NUMAResourcesOperatorReconciler{
		Client:       fakeClient,
		Scheme:       scheme.Scheme,
		Platform:     plat,
		APIManifests: apiManifests,
		RTEManifests: rteManifests,
		Helper:       helper,
		Namespace:    testNamespace,
		ImageSpec:    testImageSpec,
		Recorder:     recorder,
	}, nil
}

var _ = Describe("Test NUMAResourcesOperator Reconcile", func() {
	verifyDegradedCondition := func(nro *nrov1alpha1.NUMAResourcesOperator, reason string) {
		reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, nro)
		Expect(err).ToNot(HaveOccurred())

		key := client.ObjectKeyFromObject(nro)
		result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		Expect(reconciler.Client.Get(context.TODO(), key, nro)).ToNot(HaveOccurred())
		degradedCondition := getConditionByType(nro.Status.Conditions, status.ConditionDegraded)
		Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(degradedCondition.Reason).To(Equal(reason))
	}

	Context("with unexpected NRO CR name", func() {
		It("should updated the CR condition to degraded", func() {
			nro := testutils.NewNUMAResourcesOperator("test", nil)
			verifyDegradedCondition(nro, status.ConditionTypeIncorrectNUMAResourcesOperatorResourceName)
		})
	})

	Context("with NRO empty machine config pool selector node group", func() {
		It("should updated the CR condition to degraded", func() {
			nro := testutils.NewNUMAResourcesOperator(defaultNUMAResourcesOperatorCrName, []*metav1.LabelSelector{nil})
			verifyDegradedCondition(nro, validation.NodeGroupsError)
		})
	})

	Context("without available machine config pools", func() {
		It("should updated the CR condition to degraded", func() {
			nro := testutils.NewNUMAResourcesOperator(defaultNUMAResourcesOperatorCrName, []*metav1.LabelSelector{
				{
					MatchLabels: map[string]string{"test": "test"},
				},
			})
			verifyDegradedCondition(nro, validation.NodeGroupsError)
		})
	})

	Context("with correct NRO CR", func() {
		var nro *nrov1alpha1.NUMAResourcesOperator
		var mcp1 *machineconfigv1.MachineConfigPool
		var mcp2 *machineconfigv1.MachineConfigPool

		BeforeEach(func() {
			label1 := map[string]string{
				"test1": "test1",
			}
			label2 := map[string]string{
				"test2": "test2",
			}

			nro = testutils.NewNUMAResourcesOperator(defaultNUMAResourcesOperatorCrName, []*metav1.LabelSelector{
				{MatchLabels: label1},
				{MatchLabels: label2},
			})

			mcp1 = testutils.NewMachineConfigPool("test1", label1, &metav1.LabelSelector{MatchLabels: label1}, &metav1.LabelSelector{MatchLabels: label1})
			mcp2 = testutils.NewMachineConfigPool("test2", label2, &metav1.LabelSelector{MatchLabels: label2}, &metav1.LabelSelector{MatchLabels: label2})
		})

		Context("on the first iteration", func() {
			It("should create CRD, machine configs and wait for MCPs updates", func() {
				reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, nro, mcp1, mcp2)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(nro)
				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

				crd := &apiextensionsv1.CustomResourceDefinition{}
				key = client.ObjectKey{
					Name: "noderesourcetopologies.topology.node.k8s.io",
				}
				Expect(reconciler.Client.Get(context.TODO(), key, crd)).ToNot(HaveOccurred())

				mc := &machineconfigv1.MachineConfig{}

				key = client.ObjectKey{
					Name: rte.GetMachineConfigName(nro.Name, mcp1.Name),
				}
				Expect(reconciler.Client.Get(context.TODO(), key, mc)).ToNot(HaveOccurred())

				key = client.ObjectKey{
					Name: rte.GetMachineConfigName(nro.Name, mcp2.Name),
				}
				Expect(reconciler.Client.Get(context.TODO(), key, mc)).ToNot(HaveOccurred())
			})

			Context("with machine config pool with complex machine config selector", func() {
				var mcpWithComplexMachineConfigSelector *machineconfigv1.MachineConfigPool

				BeforeEach(func() {
					label3 := map[string]string{"test3": "test3"}
					mcpWithComplexMachineConfigSelector = testutils.NewMachineConfigPool(
						"complex-machine-config-selector",
						label3,
						&metav1.LabelSelector{MatchLabels: label3},
						&metav1.LabelSelector{MatchLabels: label3},
					)
					nro.Spec.NodeGroups = []nrov1alpha1.NodeGroup{
						{
							MachineConfigPoolSelector: &metav1.LabelSelector{
								MatchLabels: label3,
							},
						},
					}
				})

				When("machine config selector matches machine config labels", func() {
					BeforeEach(func() {
						mcpWithComplexMachineConfigSelector.Spec.MachineConfigSelector = &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      rte.MachineConfigLabelKey,
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{mcpWithComplexMachineConfigSelector.Name, "worker"},
								},
							},
						}
					})

					It("should create the machine config", func() {
						reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, nro, mcpWithComplexMachineConfigSelector)
						Expect(err).ToNot(HaveOccurred())

						key := client.ObjectKeyFromObject(nro)
						result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
						Expect(err).ToNot(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

						mc := &machineconfigv1.MachineConfig{}
						key = client.ObjectKey{
							Name: rte.GetMachineConfigName(nro.Name, mcpWithComplexMachineConfigSelector.Name),
						}
						Expect(reconciler.Client.Get(context.TODO(), key, mc)).ToNot(HaveOccurred())
					})
				})

				When("machine config selector does not match machine config labels", func() {
					BeforeEach(func() {
						mcpWithComplexMachineConfigSelector.Spec.MachineConfigSelector = &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      rte.MachineConfigLabelKey,
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"worker", "worker-cnf"},
								},
							},
						}
					})

					It("should not create the machine config and set the degraded condition", func() {
						reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, nro, mcpWithComplexMachineConfigSelector)
						Expect(err).ToNot(HaveOccurred())

						key := client.ObjectKeyFromObject(nro)
						result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
						Expect(err).To(HaveOccurred())
						Expect(result).To(Equal(reconcile.Result{}))

						mc := &machineconfigv1.MachineConfig{}
						key = client.ObjectKey{
							Name: rte.GetMachineConfigName(nro.Name, mcpWithComplexMachineConfigSelector.Name),
						}
						err = reconciler.Client.Get(context.TODO(), key, mc)
						Expect(apierrors.IsNotFound(err)).To(BeTrue())

						Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(nro), nro)).ToNot(HaveOccurred())
						degradedCondition := getConditionByType(nro.Status.Conditions, status.ConditionDegraded)
						Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue))
						Expect(degradedCondition.Message).To(ContainSubstring("labels does not match the machine config pool"))
					})
				})
			})
		})

		Context("on the second iteration", func() {
			When("machine config pools still are not ready", func() {
				It("should wait", func() {
					reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, nro, mcp1, mcp2)
					Expect(err).ToNot(HaveOccurred())

					key := client.ObjectKeyFromObject(nro)
					result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
					Expect(result).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

					result, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
					Expect(result).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

					Expect(reconciler.Client.Get(context.TODO(), key, nro)).ToNot(HaveOccurred())
					Expect(len(nro.Status.MachineConfigPools)).To(Equal(1))
					Expect(nro.Status.MachineConfigPools[0].Name).To(Equal("test1"))
				})
			})

			When("machine config pools are ready", func() {
				It("should continue with creation of additional components", func() {
					reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, nro, mcp1, mcp2)
					Expect(err).ToNot(HaveOccurred())

					key := client.ObjectKeyFromObject(nro)
					result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
					Expect(result).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp1), mcp1)).ToNot(HaveOccurred())
					mcp1.Status.Configuration.Source = []corev1.ObjectReference{
						{
							Name: rte.GetMachineConfigName(nro.Name, mcp1.Name),
						},
					}
					mcp1.Status.Conditions = []machineconfigv1.MachineConfigPoolCondition{
						{
							Type:   machineconfigv1.MachineConfigPoolUpdated,
							Status: corev1.ConditionTrue,
						},
					}
					Expect(reconciler.Client.Status().Update(context.TODO(), mcp1))

					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp2), mcp2)).ToNot(HaveOccurred())
					mcp2.Status.Configuration.Source = []corev1.ObjectReference{
						{
							Name: rte.GetMachineConfigName(nro.Name, mcp2.Name),
						},
					}
					mcp2.Status.Conditions = []machineconfigv1.MachineConfigPoolCondition{
						{
							Type:   machineconfigv1.MachineConfigPoolUpdated,
							Status: corev1.ConditionTrue,
						},
					}
					Expect(reconciler.Client.Status().Update(context.TODO(), mcp2))

					result, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
					Expect(result).To(Equal(reconcile.Result{RequeueAfter: 5 * time.Second}))

					key = client.ObjectKey{
						Name:      "rte",
						Namespace: testNamespace,
					}
					role := &rbacv1.Role{}
					Expect(reconciler.Client.Get(context.TODO(), key, role)).ToNot(HaveOccurred())

					rb := &rbacv1.RoleBinding{}
					Expect(reconciler.Client.Get(context.TODO(), key, rb)).ToNot(HaveOccurred())

					sa := &corev1.ServiceAccount{}
					Expect(reconciler.Client.Get(context.TODO(), key, sa)).ToNot(HaveOccurred())

					key.Namespace = ""
					cr := &rbacv1.ClusterRole{}
					Expect(reconciler.Client.Get(context.TODO(), key, cr)).ToNot(HaveOccurred())

					crb := &rbacv1.ClusterRoleBinding{}
					Expect(reconciler.Client.Get(context.TODO(), key, crb)).ToNot(HaveOccurred())

					key.Name = "resource-topology-exporter"
					scc := &securityv1.SecurityContextConstraints{}
					Expect(reconciler.Client.Get(context.TODO(), key, scc)).ToNot(HaveOccurred())

					key = client.ObjectKey{
						Name:      rte.GetComponentName(nro.Name, mcp1.Name),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), key, ds)).ToNot(HaveOccurred())

					key.Name = rte.GetComponentName(nro.Name, mcp2.Name)
					Expect(reconciler.Client.Get(context.TODO(), key, ds)).ToNot(HaveOccurred())
				})
			})
		})
	})
})

func getConditionByType(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		c := &conditions[i]
		if c.Type == conditionType {
			return c
		}
	}

	return nil
}
