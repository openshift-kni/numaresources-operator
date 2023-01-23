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
	"encoding/json"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	securityv1 "github.com/openshift/api/security/v1"
	appsv1 "k8s.io/api/apps/v1"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	. "github.com/onsi/ginkgo/v2"
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

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/pkg/validation"

	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
)

const (
	testImageSpec     = "quay.io/openshift-kni/numaresources-operator:ci-test"
	defaultOCPVersion = "v4.11"
)

func NewFakeNUMAResourcesOperatorReconciler(plat platform.Platform, platVersion platform.Version, initObjects ...runtime.Object) (*NUMAResourcesOperatorReconciler, error) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(initObjects...).Build()
	apiManifests, err := apimanifests.GetManifests(plat)
	if err != nil {
		return nil, err
	}

	rteManifests, err := rtemanifests.GetManifests(plat, platVersion, testNamespace)
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
		Namespace:    testNamespace,
		ImageSpec:    testImageSpec,
		Recorder:     recorder,
	}, nil
}

var _ = Describe("Test NUMAResourcesOperator Reconcile", func() {
	verifyDegradedCondition := func(nro *nropv1.NUMAResourcesOperator, reason string) {
		reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro)
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
			nro := testobjs.NewNUMAResourcesOperator("test", nil)
			verifyDegradedCondition(nro, status.ConditionTypeIncorrectNUMAResourcesOperatorResourceName)
		})
	})

	Context("with NRO empty machine config pool selector node group", func() {
		It("should updated the CR condition to degraded", func() {
			nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, []*metav1.LabelSelector{nil})
			verifyDegradedCondition(nro, validation.NodeGroupsError)
		})
	})

	Context("without available machine config pools", func() {
		It("should updated the CR condition to degraded", func() {
			nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, []*metav1.LabelSelector{
				{
					MatchLabels: map[string]string{"test": "test"},
				},
			})
			verifyDegradedCondition(nro, validation.NodeGroupsError)
		})
	})

	Context("with correct NRO and more than one NodeGroup", func() {
		var nro *nropv1.NUMAResourcesOperator
		var mcp1 *machineconfigv1.MachineConfigPool
		var mcp2 *machineconfigv1.MachineConfigPool

		var reconciler *NUMAResourcesOperatorReconciler
		var label1, label2 map[string]string

		BeforeEach(func() {
			label1 = map[string]string{
				"test1": "test1",
			}
			label2 = map[string]string{
				"test2": "test2",
			}

			nro = testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, []*metav1.LabelSelector{
				{MatchLabels: label1},
				{MatchLabels: label2},
			})

			mcp1 = testobjs.NewMachineConfigPool("test1", label1, &metav1.LabelSelector{MatchLabels: label1}, &metav1.LabelSelector{MatchLabels: label1})
			mcp2 = testobjs.NewMachineConfigPool("test2", label2, &metav1.LabelSelector{MatchLabels: label2}, &metav1.LabelSelector{MatchLabels: label2})

			var err error
			reconciler, err = NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcp1, mcp2)
			Expect(err).ToNot(HaveOccurred())

			key := client.ObjectKeyFromObject(nro)
			firstLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())
			Expect(firstLoopResult).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

			// Ensure mcp1 is ready
			Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp1), mcp1)).ToNot(HaveOccurred())
			mcp1.Status.Configuration.Source = []corev1.ObjectReference{
				{
					Name: objectnames.GetMachineConfigName(nro.Name, mcp1.Name),
				},
			}
			mcp1.Status.Conditions = []machineconfigv1.MachineConfigPoolCondition{
				{
					Type:   machineconfigv1.MachineConfigPoolUpdated,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(reconciler.Client.Status().Update(context.TODO(), mcp1))

			// ensure mcp2 is ready
			Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp2), mcp2)).ToNot(HaveOccurred())
			mcp2.Status.Configuration.Source = []corev1.ObjectReference{
				{
					Name: objectnames.GetMachineConfigName(nro.Name, mcp2.Name),
				},
			}
			mcp2.Status.Conditions = []machineconfigv1.MachineConfigPoolCondition{
				{
					Type:   machineconfigv1.MachineConfigPoolUpdated,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(reconciler.Client.Status().Update(context.TODO(), mcp2))

			secondLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			Expect(err).ToNot(HaveOccurred())
			Expect(secondLoopResult).To(Equal(reconcile.Result{RequeueAfter: 5 * time.Second}))

			By("Check DaemonSets are created")
			mcp1DSKey := client.ObjectKey{
				Name:      objectnames.GetComponentName(nro.Name, mcp1.Name),
				Namespace: testNamespace,
			}
			ds := &appsv1.DaemonSet{}
			Expect(reconciler.Client.Get(context.TODO(), mcp1DSKey, ds)).ToNot(HaveOccurred())

			mcp2DSKey := client.ObjectKey{
				Name:      objectnames.GetComponentName(nro.Name, mcp2.Name),
				Namespace: testNamespace,
			}
			Expect(reconciler.Client.Get(context.TODO(), mcp2DSKey, ds)).ToNot(HaveOccurred())
		})
		When("a NodeGroup is deleted", func() {
			BeforeEach(func() {
				// check we have at least two NodeGroups
				Expect(len(nro.Spec.NodeGroups)).To(BeNumerically(">", 1))

				By("Update NRO to have just one NodeGroup")
				key := client.ObjectKeyFromObject(nro)
				nro := &nropv1.NUMAResourcesOperator{}
				Expect(reconciler.Client.Get(context.TODO(), key, nro)).NotTo(HaveOccurred())

				nro.Spec.NodeGroups = []nropv1.NodeGroup{{
					MachineConfigPoolSelector: &metav1.LabelSelector{MatchLabels: label1},
				}}
				Expect(reconciler.Client.Update(context.TODO(), nro)).NotTo(HaveOccurred())

				thirdLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(thirdLoopResult).To(Equal(reconcile.Result{RequeueAfter: 5 * time.Second}))
			})
			It("should delete also the corresponding DaemonSet", func() {

				ds := &appsv1.DaemonSet{}

				// Check ds1 still exist
				ds1Key := client.ObjectKey{
					Name:      objectnames.GetComponentName(nro.Name, mcp1.Name),
					Namespace: testNamespace,
				}
				Expect(reconciler.Client.Get(context.TODO(), ds1Key, ds)).NotTo(HaveOccurred())

				// check ds2 has been deleted
				ds2Key := client.ObjectKey{
					Name:      objectnames.GetComponentName(nro.Name, mcp2.Name),
					Namespace: testNamespace,
				}
				Expect(reconciler.Client.Get(context.TODO(), ds2Key, ds)).To(HaveOccurred(), "error: Daemonset %v should have been deleted", ds2Key)
			})
			It("should delete also the corresponding Machineconfig", func() {

				mc := &machineconfigv1.MachineConfig{}

				// Check ds1 still exist
				mc1Key := client.ObjectKey{
					Name: objectnames.GetMachineConfigName(nro.Name, mcp1.Name),
				}
				Expect(reconciler.Client.Get(context.TODO(), mc1Key, mc)).NotTo(HaveOccurred())

				// check ds2 has been deleted
				mc2Key := client.ObjectKey{
					Name: objectnames.GetMachineConfigName(nro.Name, mcp2.Name),
				}
				Expect(reconciler.Client.Get(context.TODO(), mc2Key, mc)).To(HaveOccurred(), "error: Machineconfig %v should have been deleted", mc2Key)
			})
			When("a NOT owned Daemonset exists", func() {
				BeforeEach(func() {
					By("Create a new Daemonset with correct name but not owner reference")

					ds := reconciler.RTEManifests.DaemonSet.DeepCopy()
					ds.Name = objectnames.GetComponentName(nro.Name, mcp2.Name)
					ds.Namespace = testNamespace

					Expect(reconciler.Client.Create(context.TODO(), ds)).ToNot(HaveOccurred())

					key := client.ObjectKeyFromObject(nro)
					var err error
					_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
				})

				It("should NOT delete not Owned DaemonSets", func() {
					ds := &appsv1.DaemonSet{}

					// Check ds1 still exist
					ds1Key := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, mcp1.Name),
						Namespace: testNamespace,
					}
					Expect(reconciler.Client.Get(context.TODO(), ds1Key, ds)).NotTo(HaveOccurred())

					// Check not owned DS is NOT deleted even if the name corresponds to mcp2
					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, mcp2.Name),
						Namespace: testNamespace,
					}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).NotTo(HaveOccurred(), "error: Daemonset %v should NOT have been deleted", dsKey)
				})
			})
		})
	})
	Context("with correct NRO CR", func() {
		var nro *nropv1.NUMAResourcesOperator
		var mcp1 *machineconfigv1.MachineConfigPool
		var mcp2 *machineconfigv1.MachineConfigPool

		var reconciler *NUMAResourcesOperatorReconciler
		var label1 map[string]string

		BeforeEach(func() {
			label1 = map[string]string{
				"test1": "test1",
			}
			label2 := map[string]string{
				"test2": "test2",
			}

			nro = testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, []*metav1.LabelSelector{
				{MatchLabels: label1},
				{MatchLabels: label2},
			})

			mcp1 = testobjs.NewMachineConfigPool("test1", label1, &metav1.LabelSelector{MatchLabels: label1}, &metav1.LabelSelector{MatchLabels: label1})
			mcp2 = testobjs.NewMachineConfigPool("test2", label2, &metav1.LabelSelector{MatchLabels: label2}, &metav1.LabelSelector{MatchLabels: label2})
		})

		Context("with machine config pool with SIMPLE machine config selector", func() {

			BeforeEach(func() {
				var err error

				reconciler, err = NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcp1, mcp2)
				Expect(err).ToNot(HaveOccurred())
			})
			Context("on the first iteration", func() {
				var firstLoopResult reconcile.Result
				BeforeEach(func() {
					var err error

					key := client.ObjectKeyFromObject(nro)
					firstLoopResult, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
				})
				It("should create CRD, machine configs and wait for MCPs updates", func() {
					// check reconcile loop result
					Expect(firstLoopResult).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

					// check CRD is created
					crd := &apiextensionsv1.CustomResourceDefinition{}
					crdKey := client.ObjectKey{
						Name: "noderesourcetopologies.topology.node.k8s.io",
					}
					Expect(reconciler.Client.Get(context.TODO(), crdKey, crd)).ToNot(HaveOccurred())

					// check MachineConfigs are created
					mc := &machineconfigv1.MachineConfig{}
					mc1Key := client.ObjectKey{
						Name: objectnames.GetMachineConfigName(nro.Name, mcp1.Name),
					}
					Expect(reconciler.Client.Get(context.TODO(), mc1Key, mc)).ToNot(HaveOccurred())

					mc2Key := client.ObjectKey{
						Name: objectnames.GetMachineConfigName(nro.Name, mcp2.Name),
					}
					Expect(reconciler.Client.Get(context.TODO(), mc2Key, mc)).ToNot(HaveOccurred())
				})
			})
			Context("on the second iteration", func() {
				var secondLoopResult reconcile.Result
				When("machine config pools still are not ready", func() {
					BeforeEach(func() {
						var err error

						key := client.ObjectKeyFromObject(nro)
						secondLoopResult, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
						Expect(err).ToNot(HaveOccurred())
					})
					It("should wait", func() {
						//check reconcile second loop result
						Expect(secondLoopResult).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

						key := client.ObjectKeyFromObject(nro)
						Expect(reconciler.Client.Get(context.TODO(), key, nro)).ToNot(HaveOccurred())
						Expect(len(nro.Status.MachineConfigPools)).To(Equal(1))
						Expect(nro.Status.MachineConfigPools[0].Name).To(Equal("test1"))
					})
				})

				When("machine config pools are ready", func() {
					BeforeEach(func() {
						var err error

						By("Ensure both MachineConfigPools are ready")
						// Ensure mcp1 is ready
						Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp1), mcp1)).ToNot(HaveOccurred())
						mcp1.Status.Configuration.Source = []corev1.ObjectReference{
							{
								Name: objectnames.GetMachineConfigName(nro.Name, mcp1.Name),
							},
						}
						mcp1.Status.Conditions = []machineconfigv1.MachineConfigPoolCondition{
							{
								Type:   machineconfigv1.MachineConfigPoolUpdated,
								Status: corev1.ConditionTrue,
							},
						}
						Expect(reconciler.Client.Status().Update(context.TODO(), mcp1))

						// ensure mcp2 is ready
						Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp2), mcp2)).ToNot(HaveOccurred())
						mcp2.Status.Configuration.Source = []corev1.ObjectReference{
							{
								Name: objectnames.GetMachineConfigName(nro.Name, mcp2.Name),
							},
						}
						mcp2.Status.Conditions = []machineconfigv1.MachineConfigPoolCondition{
							{
								Type:   machineconfigv1.MachineConfigPoolUpdated,
								Status: corev1.ConditionTrue,
							},
						}
						Expect(reconciler.Client.Status().Update(context.TODO(), mcp2))

						key := client.ObjectKeyFromObject(nro)
						secondLoopResult, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
						Expect(err).ToNot(HaveOccurred())
					})
					It("should continue with creation of additional components", func() {
						// check reconcile second loop result
						Expect(secondLoopResult).To(Equal(reconcile.Result{RequeueAfter: 5 * time.Second}))

						By("Check All the additional components are created")
						rteKey := client.ObjectKey{
							Name:      "rte",
							Namespace: testNamespace,
						}
						role := &rbacv1.Role{}
						Expect(reconciler.Client.Get(context.TODO(), rteKey, role)).ToNot(HaveOccurred())

						rb := &rbacv1.RoleBinding{}
						Expect(reconciler.Client.Get(context.TODO(), rteKey, rb)).ToNot(HaveOccurred())

						sa := &corev1.ServiceAccount{}
						Expect(reconciler.Client.Get(context.TODO(), rteKey, sa)).ToNot(HaveOccurred())

						crKey := client.ObjectKey{
							Name: "rte",
						}
						cr := &rbacv1.ClusterRole{}
						Expect(reconciler.Client.Get(context.TODO(), crKey, cr)).ToNot(HaveOccurred())

						crb := &rbacv1.ClusterRoleBinding{}
						Expect(reconciler.Client.Get(context.TODO(), crKey, crb)).ToNot(HaveOccurred())

						resourceTopologyExporterKey := client.ObjectKey{
							Name: "resource-topology-exporter",
						}
						scc := &securityv1.SecurityContextConstraints{}
						Expect(reconciler.Client.Get(context.TODO(), resourceTopologyExporterKey, scc)).ToNot(HaveOccurred())

						mcp1DSKey := client.ObjectKey{
							Name:      objectnames.GetComponentName(nro.Name, mcp1.Name),
							Namespace: testNamespace,
						}
						ds := &appsv1.DaemonSet{}
						Expect(reconciler.Client.Get(context.TODO(), mcp1DSKey, ds)).ToNot(HaveOccurred())

						mcp2DSKey := client.ObjectKey{
							Name:      objectnames.GetComponentName(nro.Name, mcp2.Name),
							Namespace: testNamespace,
						}
						Expect(reconciler.Client.Get(context.TODO(), mcp2DSKey, ds)).ToNot(HaveOccurred())
					})

				})
			})
		})

		Context("with machine config pool with complex machine config selector", func() {
			var mcpWithComplexMachineConfigSelector *machineconfigv1.MachineConfigPool

			BeforeEach(func() {
				label3 := map[string]string{"test3": "test3"}
				mcpWithComplexMachineConfigSelector = testobjs.NewMachineConfigPool(
					"complex-machine-config-selector",
					label3,
					&metav1.LabelSelector{MatchLabels: label3},
					&metav1.LabelSelector{MatchLabels: label3},
				)
				nro.Spec.NodeGroups = []nropv1.NodeGroup{
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
					var err error

					reconciler, err = NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcpWithComplexMachineConfigSelector)
					Expect(err).ToNot(HaveOccurred())
				})

				It("should create the machine config", func() {
					key := client.ObjectKeyFromObject(nro)
					firstLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
					Expect(firstLoopResult).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

					mc := &machineconfigv1.MachineConfig{}
					key = client.ObjectKey{
						Name: objectnames.GetMachineConfigName(nro.Name, mcpWithComplexMachineConfigSelector.Name),
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

					var err error
					reconciler, err = NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcpWithComplexMachineConfigSelector)
					Expect(err).ToNot(HaveOccurred())
				})

				It("should not create the machine config and set the degraded condition", func() {
					key := client.ObjectKeyFromObject(nro)
					firstLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).To(HaveOccurred())
					Expect(firstLoopResult).To(Equal(reconcile.Result{}))

					mc := &machineconfigv1.MachineConfig{}
					mcKey := client.ObjectKey{
						Name: objectnames.GetMachineConfigName(nro.Name, mcpWithComplexMachineConfigSelector.Name),
					}
					err = reconciler.Client.Get(context.TODO(), mcKey, mc)
					Expect(apierrors.IsNotFound(err)).To(BeTrue())

					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(nro), nro)).ToNot(HaveOccurred())
					degradedCondition := getConditionByType(nro.Status.Conditions, status.ConditionDegraded)
					Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue))
					Expect(degradedCondition.Message).To(ContainSubstring("labels does not match the machine config pool"))
				})
			})
		})
	})

	Context("with NodeGroupConfig", func() {
		var labels map[string]string
		var labSel metav1.LabelSelector
		var mcp *machineconfigv1.MachineConfigPool

		BeforeEach(func() {
			labels = map[string]string{
				"test": "test",
			}

			labSel = metav1.LabelSelector{
				MatchLabels: labels,
			}

			mcp = testobjs.NewMachineConfigPool("test", labels, &metav1.LabelSelector{MatchLabels: labels}, &metav1.LabelSelector{MatchLabels: labels})
		})

		It("should set defaults in the DS objects", func() {
			nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, &labSel, nil)

			reconciler := reconcileObjects(nro, mcp)

			mcpDSKey := client.ObjectKey{
				Name:      objectnames.GetComponentName(nro.Name, mcp.Name),
				Namespace: testNamespace,
			}
			ds := &appsv1.DaemonSet{}
			Expect(reconciler.Client.Get(context.TODO(), mcpDSKey, ds)).ToNot(HaveOccurred())

			args := ds.Spec.Template.Spec.Containers[0].Args
			Expect(args).To(ContainElement("--sleep-interval=10s"), "malformed args: %v", args)
			Expect(args).To(ContainElement(ContainSubstring("--notify-file=")), "malformed args: %v", args)
			Expect(args).To(ContainElement("--pods-fingerprint"), "malformed args: %v", args)
		})

		It("should report the observed values per-MCP in status", func() {
			d, err := time.ParseDuration("33s")
			Expect(err).ToNot(HaveOccurred())

			period := metav1.Duration{
				Duration: d,
			}
			pfpMode := nropv1.PodsFingerprintingEnabled
			refMode := nropv1.InfoRefreshPeriodic
			conf := nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpMode,
				InfoRefreshPeriod:  &period,
				InfoRefreshMode:    &refMode,
			}

			nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, &labSel, &conf)

			reconciler := reconcileObjects(nro, mcp)

			nroUpdated := &nropv1.NUMAResourcesOperator{}
			Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(nro), nroUpdated)).ToNot(HaveOccurred())

			Expect(len(nroUpdated.Status.MachineConfigPools)).To(Equal(1))
			Expect(nroUpdated.Status.MachineConfigPools[0].Name).To(Equal(mcp.Name))
			// TODO check the actual returned config. We need to force the condition as Available for this.
		})

		It("should allow to alter all the settings of the DS objects", func() {
			d, err := time.ParseDuration("33s")
			Expect(err).ToNot(HaveOccurred())

			period := metav1.Duration{
				Duration: d,
			}
			pfpMode := nropv1.PodsFingerprintingEnabled
			refMode := nropv1.InfoRefreshPeriodic
			conf := nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpMode,
				InfoRefreshPeriod:  &period,
				InfoRefreshMode:    &refMode,
			}

			nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, &labSel, &conf)

			reconciler := reconcileObjects(nro, mcp)

			mcpDSKey := client.ObjectKey{
				Name:      objectnames.GetComponentName(nro.Name, mcp.Name),
				Namespace: testNamespace,
			}
			ds := &appsv1.DaemonSet{}
			Expect(reconciler.Client.Get(context.TODO(), mcpDSKey, ds)).ToNot(HaveOccurred())

			args := ds.Spec.Template.Spec.Containers[0].Args
			Expect(args).To(ContainElement("--sleep-interval=33s"), "malformed args: %v", args)
			Expect(args).ToNot(ContainElement(ContainSubstring("--notify-file=")), "malformed args: %v", args)
			Expect(args).To(ContainElement("--pods-fingerprint"), "malformed args: %v", args)
		})

		It("should allow to disable pods fingerprinting", func() {
			pfpMode := nropv1.PodsFingerprintingDisabled
			conf := nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpMode,
			}
			nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, &labSel, &conf)

			reconciler := reconcileObjects(nro, mcp)

			mcpDSKey := client.ObjectKey{
				Name:      objectnames.GetComponentName(nro.Name, mcp.Name),
				Namespace: testNamespace,
			}
			ds := &appsv1.DaemonSet{}
			Expect(reconciler.Client.Get(context.TODO(), mcpDSKey, ds)).ToNot(HaveOccurred())

			args := ds.Spec.Template.Spec.Containers[0].Args
			Expect(args).ToNot(ContainElement("--pods-fingerprint"), "malformed args: %v", args)
		})

		It("should allow to tune the update period", func() {
			d, err := time.ParseDuration("42s")
			Expect(err).ToNot(HaveOccurred())

			period := metav1.Duration{
				Duration: d,
			}
			conf := nropv1.NodeGroupConfig{
				InfoRefreshPeriod: &period,
			}
			nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, &labSel, &conf)

			reconciler := reconcileObjects(nro, mcp)

			mcpDSKey := client.ObjectKey{
				Name:      objectnames.GetComponentName(nro.Name, mcp.Name),
				Namespace: testNamespace,
			}
			ds := &appsv1.DaemonSet{}
			Expect(reconciler.Client.Get(context.TODO(), mcpDSKey, ds)).ToNot(HaveOccurred())

			args := ds.Spec.Template.Spec.Containers[0].Args
			Expect(args).To(ContainElement("--sleep-interval=42s"), "malformed args: %v", args)
		})

		It("should allow to tune the update mechanism", func() {
			refMode := nropv1.InfoRefreshPeriodic
			conf := nropv1.NodeGroupConfig{
				InfoRefreshMode: &refMode,
			}

			nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, &labSel, &conf)

			reconciler := reconcileObjects(nro, mcp)

			mcpDSKey := client.ObjectKey{
				Name:      objectnames.GetComponentName(nro.Name, mcp.Name),
				Namespace: testNamespace,
			}
			ds := &appsv1.DaemonSet{}
			Expect(reconciler.Client.Get(context.TODO(), mcpDSKey, ds)).ToNot(HaveOccurred())

			args := ds.Spec.Template.Spec.Containers[0].Args
			Expect(args).ToNot(ContainElement(ContainSubstring("--notify-file")), "malformed args: %v", args)
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

func reconcileObjects(nro *nropv1.NUMAResourcesOperator, mcp *machineconfigv1.MachineConfigPool) *NUMAResourcesOperatorReconciler {
	reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcp)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	key := client.ObjectKeyFromObject(nro)

	// we need to do the first iteration here because the DS object is created in the second
	_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	By("Ensure MachineConfigPools is ready")
	ExpectWithOffset(1, reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp), mcp)).ToNot(HaveOccurred())
	mcp.Status.Configuration.Source = []corev1.ObjectReference{
		{
			Name: objectnames.GetMachineConfigName(nro.Name, mcp.Name),
		},
	}
	mcp.Status.Conditions = []machineconfigv1.MachineConfigPoolCondition{
		{
			Type:   machineconfigv1.MachineConfigPoolUpdated,
			Status: corev1.ConditionTrue,
		},
	}
	ExpectWithOffset(1, reconciler.Client.Status().Update(context.TODO(), mcp))

	var secondLoopResult reconcile.Result
	secondLoopResult, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	ExpectWithOffset(1, secondLoopResult).To(Equal(reconcile.Result{RequeueAfter: 5 * time.Second}))
	return reconciler
}

func nodeGroupConfigToString(conf nropv1.NodeGroupConfig) string {
	data, err := json.Marshal(conf)
	if err != nil {
		return "<ERROR>"
	}
	return string(data)
}
