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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"

	configv1 "github.com/openshift/api/config/v1"
	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
	"github.com/openshift-kni/numaresources-operator/pkg/images"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/pkg/validation"
)

const (
	testImageSpec     = "quay.io/openshift-kni/numaresources-operator:ci-test"
	defaultOCPVersion = "v4.14"
)

func NewFakeNUMAResourcesOperatorReconciler(plat platform.Platform, platVersion platform.Version, initObjects ...runtime.Object) (*NUMAResourcesOperatorReconciler, error) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithStatusSubresource(&nropv1.NUMAResourcesOperator{}).WithRuntimeObjects(initObjects...).Build()
	apiManifests, err := apimanifests.GetManifests(plat)
	if err != nil {
		return nil, err
	}
	rteManifests, err := rtemanifests.GetManifests(plat, platVersion, testNamespace, false, true)
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
		Images: images.Data{
			Builtin: testImageSpec,
		},
		Recorder: recorder,
	}, nil
}

var _ = Describe("Test NUMAResourcesOperator Reconcile", func() {
	verifyDegradedCondition := func(nro *nropv1.NUMAResourcesOperator, reason string, platf platform.Platform) {
		GinkgoHelper()

		reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platf, defaultOCPVersion, nro)
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

	DescribeTableSubtree("Running on different platforms", func(platf platform.Platform) {

		Context("with unexpected NRO CR name", func() {
			It("should updated the CR condition to degraded", func() {
				nro := testobjs.NewNUMAResourcesOperator("test")
				verifyDegradedCondition(nro, status.ConditionTypeIncorrectNUMAResourcesOperatorResourceName, platf)
			})
		})

		Context("with NRO empty selectors node group", func() {
			It("should update the CR condition to degraded", func() {
				ng := nropv1.NodeGroup{}
				nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng)
				verifyDegradedCondition(nro, validation.NodeGroupsError, platf)
			})
		})

		Context("with PoolName set to empty string", func() {
			It("should update the CR condition to degraded", func() {
				pn := ""
				ng := nropv1.NodeGroup{
					PoolName: &pn,
				}
				nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng)
				verifyDegradedCondition(nro, validation.NodeGroupsError, platf)
			})
		})

		Context("with NRO mutiple pool specifiers set on same node group", func() {
			It("should update the CR condition to degraded", func() {
				pn := "test"
				ng := nropv1.NodeGroup{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{pn: pn},
					},
					PoolName: &pn,
				}
				nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng)
				verifyDegradedCondition(nro, validation.NodeGroupsError, platf)
			})
		})

		Context("with two node groups while both point to same pool using same pool specifier", func() {
			It("should update the CR condition to degraded - PoolName", func() {
				poolName := "test"

				ng1 := nropv1.NodeGroup{
					PoolName: &poolName,
				}
				ng2 := nropv1.NodeGroup{
					PoolName: &poolName,
				}
				nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng1, ng2)

				var err error
				reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platf, defaultOCPVersion, nro)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(nro)
				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))

				Expect(reconciler.Client.Get(context.TODO(), key, nro)).ToNot(HaveOccurred())
				degradedCondition := getConditionByType(nro.Status.Conditions, status.ConditionDegraded)
				Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue))
				Expect(degradedCondition.Reason).To(Equal(validation.NodeGroupsError))
			})
		})

		Context("with default RTE SELinux and PoolName set", func() {
			Context("with correct NRO and more than one NodeGroup", func() {
				var nro *nropv1.NUMAResourcesOperator
				var mcp1 *machineconfigv1.MachineConfigPool
				var mcp2 *machineconfigv1.MachineConfigPool
				var mcp1Selector, mcp2Selector *metav1.LabelSelector
				var nroKey client.ObjectKey

				var reconciler *NUMAResourcesOperatorReconciler
				var ng1, ng2 nropv1.NodeGroup

				pn1 := "test1"
				pn2 := "test2"

				BeforeEach(func() {
					mcp1Selector = &metav1.LabelSelector{
						MatchLabels: map[string]string{
							pn1: pn1,
						},
					}
					mcp2Selector = &metav1.LabelSelector{
						MatchLabels: map[string]string{
							pn2: pn2,
						},
					}

					ng1 = nropv1.NodeGroup{
						PoolName: &pn1,
					}
					ng2 = nropv1.NodeGroup{
						PoolName: &pn2,
					}

					nro = testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng1, ng2)
					nroKey = client.ObjectKeyFromObject(nro)

					// would be used only if the platform supports MCP
					mcp1 = testobjs.NewMachineConfigPool(pn1, mcp1Selector.MatchLabels, mcp1Selector, mcp1Selector)
					mcp2 = testobjs.NewMachineConfigPool(pn2, mcp2Selector.MatchLabels, mcp2Selector, mcp2Selector)

					var err error
					reconciler, err = NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcp1, mcp2)
					Expect(err).ToNot(HaveOccurred())

					// on the first iteration with the default RTE SELinux policy we expect immediate update, thus the reconciliation result is empty
					result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: nroKey})
					Expect(err).ToNot(HaveOccurred())
					Expect(result).To(Equal(reconcile.Result{}))
				})

				It("should create all CRDs and objects and operator status are updated from the first reconcile iteration", func() {
					By("Check DaemonSets are created")
					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn1),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					dsKey = client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn2),
						Namespace: testNamespace,
					}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).To(Succeed())

					By("Check status is updated")
					Expect(reconciler.Client.Get(context.TODO(), nroKey, nro)).ToNot(HaveOccurred())
					availableCondition := getConditionByType(nro.Status.Conditions, status.ConditionAvailable)
					Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))

					Expect(len(nro.Status.MachineConfigPools)).To(Equal(2))
					Expect(nro.Status.MachineConfigPools[0].Name).To(Equal(pn1))
					Expect(nro.Status.MachineConfigPools[1].Name).To(Equal(pn2))

					conf := nropv1.DefaultNodeGroupConfig()
					Expect(nro.Status.NodeGroups[0].Config).To(Equal(conf), "default node group config for %q was not updated in the operator status", nro.Status.NodeGroups[0].PoolName)
					Expect(nro.Status.NodeGroups[1].Config).To(Equal(conf), "default node group config for %q was not updated in the operator status", nro.Status.NodeGroups[1].PoolName)
				})

				It("should update node group statuses with the updated configuration", func() {
					defaultConf := nropv1.DefaultNodeGroupConfig()

					conf1 := defaultConf.DeepCopy()
					pfpModeDisabled := nropv1.PodsFingerprintingDisabled
					rteMode := nropv1.InfoRefreshPauseEnabled
					conf1.PodsFingerprinting = &pfpModeDisabled
					conf1.InfoRefreshPause = &rteMode

					conf2 := defaultConf.DeepCopy()
					pfpModeEnabled := nropv1.PodsFingerprintingEnabled
					refMode := nropv1.InfoRefreshPeriodicAndEvents
					conf2.PodsFingerprinting = &pfpModeEnabled
					conf2.InfoRefreshMode = &refMode
					conf2.Tolerations = []corev1.Toleration{
						{
							Key:    "foo",
							Value:  "1",
							Effect: corev1.TaintEffectNoSchedule,
						},
					}

					nroUpdated := &nropv1.NUMAResourcesOperator{}
					Eventually(func() error {
						Expect(reconciler.Client.Get(context.TODO(), nroKey, nroUpdated)).NotTo(HaveOccurred())
						nroUpdated.Spec.NodeGroups[0].Config = conf1
						nroUpdated.Spec.NodeGroups[1].Config = conf2
						return reconciler.Client.Update(context.TODO(), nroUpdated)
					}).WithPolling(1 * time.Second).WithTimeout(30 * time.Second).ShouldNot(HaveOccurred())

					//  immediate update
					result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: nroKey})
					Expect(err).ToNot(HaveOccurred())
					Expect(result).To(Equal(reconcile.Result{}))

					Expect(reconciler.Client.Get(context.TODO(), nroKey, nroUpdated)).ToNot(HaveOccurred())
					Expect(nroUpdated.Spec.NodeGroups[0].Config).To(Equal(conf1))

					Expect(len(nroUpdated.Status.MachineConfigPools)).To(Equal(2))
					Expect(len(nroUpdated.Status.DaemonSets)).To(Equal(2))

					// the order of the NodeGroups also preserved under the operator status
					Expect(nroUpdated.Status.MachineConfigPools[0].Name).To(Equal(pn1))
					Expect(nroUpdated.Status.NodeGroups[0].PoolName).To(Equal(pn1))
					Expect(nroUpdated.Status.MachineConfigPools[0].Config).To(Equal(conf1))
					Expect(nroUpdated.Status.NodeGroups[0].Config).To(Equal(*conf1))
					Expect(nroUpdated.Status.NodeGroups[0].DaemonSet).To(Equal(nroUpdated.Status.DaemonSets[0]))

					Expect(nroUpdated.Status.MachineConfigPools[1].Name).To(Equal(pn2))
					Expect(nroUpdated.Status.NodeGroups[1].PoolName).To(Equal(pn2))
					Expect(nroUpdated.Status.MachineConfigPools[1].Config).To(Equal(conf2))
					Expect(nroUpdated.Status.NodeGroups[1].Config).To(Equal(*conf2))
					Expect(nroUpdated.Status.NodeGroups[1].DaemonSet).To(Equal(nroUpdated.Status.DaemonSets[1]))

				})

				When("a NodeGroup is deleted", func() {
					BeforeEach(func() {
						// check we have at least two NodeGroups
						Expect(len(nro.Spec.NodeGroups)).To(BeNumerically(">", 1))

						By("Update NRO to have just one NodeGroup")
						Expect(reconciler.Client.Get(context.TODO(), nroKey, nro)).NotTo(HaveOccurred())

						nro.Spec.NodeGroups = []nropv1.NodeGroup{{
							PoolName: &pn1,
						}}
						Expect(reconciler.Client.Update(context.TODO(), nro)).NotTo(HaveOccurred())

						// immediate update reflection with no reboot needed -> no need to reconcileafter this
						thirdLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: nroKey})
						Expect(err).ToNot(HaveOccurred())
						Expect(thirdLoopResult).To(Equal(reconcile.Result{}))
					})
					It("should delete also the corresponding DaemonSet", func() {
						ds := &appsv1.DaemonSet{}

						// Check ds1 still exist
						dsKey := client.ObjectKey{
							Name:      objectnames.GetComponentName(nro.Name, pn1),
							Namespace: testNamespace,
						}
						Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).NotTo(HaveOccurred())

						// check ds2 has been deleted
						dsKey = client.ObjectKey{
							Name:      objectnames.GetComponentName(nro.Name, pn2),
							Namespace: testNamespace,
						}
						Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).To(HaveOccurred(), "error: Daemonset %v should have been deleted", dsKey)
					})

					When("a NOT owned Daemonset exists", func() {
						BeforeEach(func() {
							By("Create a new Daemonset with correct name but not owner reference")

							ds := reconciler.RTEManifests.DaemonSet.DeepCopy()
							ds.Name = objectnames.GetComponentName(nro.Name, pn2)
							ds.Namespace = testNamespace

							Expect(reconciler.Client.Create(context.TODO(), ds)).ToNot(HaveOccurred())

							var err error
							_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: nroKey})
							Expect(err).ToNot(HaveOccurred())
						})

						It("should NOT delete not Owned DaemonSets", func() {
							ds := &appsv1.DaemonSet{}

							// Check ds1 still exist
							dsKey := client.ObjectKey{
								Name:      objectnames.GetComponentName(nro.Name, pn1),
								Namespace: testNamespace,
							}
							Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).NotTo(HaveOccurred())

							// Check not owned DS is NOT deleted even if the name corresponds to mcp2
							dsKey = client.ObjectKey{
								Name:      objectnames.GetComponentName(nro.Name, pn2),
								Namespace: testNamespace,
							}
							Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).NotTo(HaveOccurred(), "error: Daemonset %v should NOT have been deleted", dsKey)
						})
					})
				})

				When("add a second pool specifier on existing node group", func() {
					It("should update the CR condition to degraded", func(ctx context.Context) {
						pnNew := "pool-1"
						ng1WithNodeSelector := ng1.DeepCopy()
						if ng1.MachineConfigPoolSelector != nil {
							ng1WithNodeSelector.PoolName = &pnNew
						} else {
							//must be PoolName that's set, so set the MCP selector
							ng1WithNodeSelector.MachineConfigPoolSelector = &metav1.LabelSelector{
								MatchLabels: map[string]string{pnNew: pnNew},
							}
						}
						ng1WithNodeSelector.PoolName = &pnNew
						Eventually(func() error {
							Expect(reconciler.Client.Get(ctx, nroKey, nro))
							nro.Spec.NodeGroups[0] = *ng1WithNodeSelector
							return reconciler.Client.Update(context.TODO(), nro)
						}).WithPolling(1 * time.Second).WithTimeout(30 * time.Second).ShouldNot(HaveOccurred())
						verifyDegradedCondition(nro, validation.NodeGroupsError, platf)
					})
				})
			})

			Context("with NodeGroupConfig", func() {
				var labSel metav1.LabelSelector
				var mcp *machineconfigv1.MachineConfigPool
				pn := "test"

				BeforeEach(func() {
					labSel = metav1.LabelSelector{
						MatchLabels: map[string]string{
							pn: pn,
						},
					}

					mcp = testobjs.NewMachineConfigPool(pn, labSel.MatchLabels, &labSel, &labSel)
				})

				It("should set defaults in the DS objects", func() {
					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, nil)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, mcp.Name),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					args := ds.Spec.Template.Spec.Containers[0].Args
					Expect(args).To(ContainElement("--sleep-interval=10s"), "malformed args: %v", args)
					Expect(args).To(ContainElement("--pods-fingerprint"), "malformed args: %v", args)
				})

				It("should report the observed values per Node Group in status", func() {
					d, err := time.ParseDuration("33s")
					Expect(err).ToNot(HaveOccurred())

					period := metav1.Duration{
						Duration: d,
					}
					pfpMode := nropv1.PodsFingerprintingEnabled
					refMode := nropv1.InfoRefreshPeriodic
					rteMode := nropv1.InfoRefreshPauseEnabled
					conf := nropv1.NodeGroupConfig{
						PodsFingerprinting: &pfpMode,
						InfoRefreshPeriod:  &period,
						InfoRefreshMode:    &refMode,
						InfoRefreshPause:   &rteMode,
					}

					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					nroUpdated := &nropv1.NUMAResourcesOperator{}
					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(nro), nroUpdated)).ToNot(HaveOccurred())

					Expect(len(nroUpdated.Status.NodeGroups)).To(Equal(1))
					Expect(nroUpdated.Status.NodeGroups[0].Config).To(Equal(conf), "operator status was not updated under NodeGroupStatus field")

					if platf != platform.HyperShift {
						Expect(len(nroUpdated.Status.MachineConfigPools)).To(Equal(1))
						Expect(nroUpdated.Status.MachineConfigPools[0].Name).To(Equal(pn))
						Expect(*nroUpdated.Status.MachineConfigPools[0].Config).To(Equal(conf), "operator status was not updated")
					}
				})

				It("should allow to alter all the settings of the DS objects", func() {
					d, err := time.ParseDuration("33s")
					Expect(err).ToNot(HaveOccurred())

					period := metav1.Duration{
						Duration: d,
					}
					pfpMode := nropv1.PodsFingerprintingEnabled
					refMode := nropv1.InfoRefreshPeriodic
					rteMode := nropv1.InfoRefreshPauseEnabled
					conf := nropv1.NodeGroupConfig{
						PodsFingerprinting: &pfpMode,
						InfoRefreshPeriod:  &period,
						InfoRefreshMode:    &refMode,
						InfoRefreshPause:   &rteMode,
					}

					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					args := ds.Spec.Template.Spec.Containers[0].Args
					Expect(args).To(ContainElement("--sleep-interval=33s"), "malformed args: %v", args)
					Expect(args).ToNot(ContainElement(ContainSubstring("--notify-file=")), "malformed args: %v", args)
					Expect(args).To(ContainElement("--pods-fingerprint"), "malformed args: %v", args)
					Expect(args).To(ContainElement(ContainSubstring("--no-publish")), "malformed args: %v", args)
				})

				It("should allow to disable pods fingerprinting", func() {
					pfpMode := nropv1.PodsFingerprintingDisabled
					conf := nropv1.NodeGroupConfig{
						PodsFingerprinting: &pfpMode,
					}
					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					args := ds.Spec.Template.Spec.Containers[0].Args
					Expect(args).ToNot(ContainElement("--pods-fingerprint"), "malformed args: %v", args)

					nroUpdated := &nropv1.NUMAResourcesOperator{}
					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(nro), nroUpdated)).ToNot(HaveOccurred())
					Expect(*nroUpdated.Status.NodeGroups[0].Config.PodsFingerprinting).To(Equal(pfpMode), "node group config was not updated under NodeGroupStatus field")
					if platf != platform.HyperShift {
						Expect(*nroUpdated.Status.MachineConfigPools[0].Config.PodsFingerprinting).To(Equal(pfpMode), "node group config was not updated in the operator status")
					}
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
					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					args := ds.Spec.Template.Spec.Containers[0].Args
					Expect(args).To(ContainElement("--sleep-interval=42s"), "malformed args: %v", args)

					nroUpdated := &nropv1.NUMAResourcesOperator{}
					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(nro), nroUpdated)).ToNot(HaveOccurred())
					Expect(*nroUpdated.Status.NodeGroups[0].Config.InfoRefreshPeriod).To(Equal(period), "node group config was not updated under NodeGroupStatus field")
					if platf != platform.HyperShift {
						Expect(*nroUpdated.Status.MachineConfigPools[0].Config.InfoRefreshPeriod).To(Equal(period), "node group config was not updated in the operator status")
					}
				})

				It("should allow to tune the update mechanism", func() {
					refMode := nropv1.InfoRefreshPeriodic
					conf := nropv1.NodeGroupConfig{
						InfoRefreshMode: &refMode,
					}

					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					args := ds.Spec.Template.Spec.Containers[0].Args
					Expect(args).ToNot(ContainElement(ContainSubstring("--notify-file")), "malformed args: %v", args)

					nroUpdated := &nropv1.NUMAResourcesOperator{}
					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(nro), nroUpdated)).ToNot(HaveOccurred())
					Expect(*nroUpdated.Status.NodeGroups[0].Config.InfoRefreshMode).To(Equal(refMode), "node group config was not updated under NodeGroupStatus field")
					if platf != platform.HyperShift {
						Expect(*nroUpdated.Status.MachineConfigPools[0].Config.InfoRefreshMode).To(Equal(refMode), "node group config was not updated in the operator status")
					}
				})

				It("should find default behavior to update NRT data", func() {
					conf := nropv1.DefaultNodeGroupConfig()

					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					key := client.ObjectKeyFromObject(nro)

					nroCurrent := &nropv1.NUMAResourcesOperator{}
					Expect(reconciler.Client.Get(context.TODO(), key, nroCurrent)).NotTo(HaveOccurred())
					Expect(nroCurrent.Spec.NodeGroups[0].Config.InfoRefreshPause).To(Equal(conf.InfoRefreshPause))

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					args := ds.Spec.Template.Spec.Containers[0].Args
					Expect(args).ToNot(ContainElement(ContainSubstring("--no-publish")), "malformed args: %v", args)
				})

				It("should allow to disabling NRT updates and enabling it back", func() {
					rteMode := nropv1.InfoRefreshPauseEnabled
					conf := nropv1.NodeGroupConfig{
						InfoRefreshPause: &rteMode,
					}
					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					key := client.ObjectKeyFromObject(nro)

					nroCurrent := &nropv1.NUMAResourcesOperator{}
					Expect(reconciler.Client.Get(context.TODO(), key, nroCurrent)).NotTo(HaveOccurred())
					Expect(*nroCurrent.Status.NodeGroups[0].Config.InfoRefreshPause).To(Equal(rteMode), "node group config was not updated under NodeGroupStatus field")
					if platf != platform.HyperShift {
						Expect(*nroCurrent.Status.MachineConfigPools[0].Config.InfoRefreshPause).To(Equal(rteMode), "node group config was not updated in the operator status")
					}

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					args := ds.Spec.Template.Spec.Containers[0].Args
					Expect(args).To(ContainElement(ContainSubstring("--no-publish")), "malformed args: %v", args)

					rteModeOpp := nropv1.InfoRefreshPauseDisabled
					confUpdated := nropv1.NodeGroupConfig{
						InfoRefreshPause: &rteModeOpp,
					}

					Eventually(func() error {
						nroUpdated := &nropv1.NUMAResourcesOperator{}
						Expect(reconciler.Client.Get(context.TODO(), key, nroUpdated)).NotTo(HaveOccurred())
						nroUpdated.Spec.NodeGroups[0].Config = &confUpdated
						return reconciler.Client.Update(context.TODO(), nroUpdated)
					}).WithPolling(1 * time.Second).WithTimeout(30 * time.Second).ShouldNot(HaveOccurred())

					// immediate update reflection with no reboot needed -> no need to reconcile after this
					result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
					Expect(result).To(Equal(reconcile.Result{}))

					dsUpdated := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, dsUpdated)).ToNot(HaveOccurred())

					argsUpdated := dsUpdated.Spec.Template.Spec.Containers[0].Args
					Expect(argsUpdated).ToNot(ContainElement(ContainSubstring("--no-publish")), "malformed args: %v", argsUpdated)

					nroUpdated := &nropv1.NUMAResourcesOperator{}
					Expect(reconciler.Client.Get(context.TODO(), key, nroUpdated)).ToNot(HaveOccurred())
					Expect(*nroUpdated.Status.NodeGroups[0].Config.InfoRefreshPause).To(Equal(rteModeOpp), "node group config was not updated under NodeGroupStatus field")
					if platf != platform.HyperShift {
						Expect(*nroUpdated.Status.MachineConfigPools[0].Config.InfoRefreshPause).To(Equal(rteModeOpp), "node group config was not updated in the operator status")
					}
				})

				It("should allow to update all the settings of the DS objects", func() {
					conf := nropv1.DefaultNodeGroupConfig()
					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					args := ds.Spec.Template.Spec.Containers[0].Args
					Expect(args).To(ContainElement("--sleep-interval=10s"), "malformed args: %v", args)
					Expect(args).To(ContainElement("--pods-fingerprint"), "malformed args: %v", args)
					Expect(args).ToNot(ContainElement(ContainSubstring("--no-publish")), "malformed args: %v", args)

					d, err := time.ParseDuration("12s")
					Expect(err).ToNot(HaveOccurred())

					pfpMode := nropv1.PodsFingerprintingEnabled
					period := metav1.Duration{
						Duration: d,
					}
					refMode := nropv1.InfoRefreshPeriodic
					rteMode := nropv1.InfoRefreshPauseEnabled
					confUpdated := nropv1.NodeGroupConfig{
						PodsFingerprinting: &pfpMode,
						InfoRefreshPeriod:  &period,
						InfoRefreshMode:    &refMode,
						InfoRefreshPause:   &rteMode,
					}

					key := client.ObjectKeyFromObject(nro)

					Eventually(func() error {
						nroUpdated := &nropv1.NUMAResourcesOperator{}
						Expect(reconciler.Client.Get(context.TODO(), key, nroUpdated)).NotTo(HaveOccurred())
						nroUpdated.Spec.NodeGroups[0].Config = &confUpdated
						return reconciler.Client.Update(context.TODO(), nroUpdated)
					}).WithPolling(1 * time.Second).WithTimeout(30 * time.Second).ShouldNot(HaveOccurred())

					// immediate update reflection with no reboot needed -> no need to reconcile after this
					thirdLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
					Expect(thirdLoopResult).To(Equal(reconcile.Result{}))

					dsUpdated := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, dsUpdated)).ToNot(HaveOccurred())

					argsUpdated := dsUpdated.Spec.Template.Spec.Containers[0].Args
					Expect(argsUpdated).To(ContainElement("--sleep-interval=12s"), "malformed updated args: %v", argsUpdated)
					Expect(argsUpdated).ToNot(ContainElement(ContainSubstring("--notify-file=")), "malformed updated args: %v", argsUpdated)
					Expect(argsUpdated).To(ContainElement("--pods-fingerprint"), "malformed updated args: %v", argsUpdated)
					Expect(argsUpdated).To(ContainElement(ContainSubstring("--no-publish")), "malformed args: %v", argsUpdated)

					nroUpdated := &nropv1.NUMAResourcesOperator{}
					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(nro), nroUpdated)).ToNot(HaveOccurred())
					Expect(nroUpdated.Status.NodeGroups[0].Config).To(Equal(confUpdated), "node group config was not updated under NodeGroupStatus field")
					if platf != platform.HyperShift {
						Expect(*nroUpdated.Status.MachineConfigPools[0].Config).To(Equal(confUpdated), "node group config was not updated in the operator status")
					}
				})

				It("should allow to change the PFP method dynamically", func() {
					pfpMode := nropv1.PodsFingerprintingEnabledExclusiveResources
					conf := nropv1.NodeGroupConfig{
						PodsFingerprinting: &pfpMode,
					}

					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					args := ds.Spec.Template.Spec.Containers[0].Args
					Expect(args).To(ContainElement("--pods-fingerprint-method=with-exclusive-resources"), "malformed args: %v", args)

					key := client.ObjectKeyFromObject(nro)
					nroUpdated := &nropv1.NUMAResourcesOperator{}
					Expect(reconciler.Client.Get(context.TODO(), key, nroUpdated)).ToNot(HaveOccurred())
					Expect(*nroUpdated.Status.NodeGroups[0].Config.PodsFingerprinting).To(Equal(pfpMode), "node group config was not updated under NodeGroupStatus field")
					if platf != platform.HyperShift {
						Expect(*nroUpdated.Status.MachineConfigPools[0].Config.PodsFingerprinting).To(Equal(pfpMode), "node group config was not updated in the operator status")
					}

					updatedPFPMode := nropv1.PodsFingerprintingEnabled
					Eventually(func() error {
						nroUpdated := &nropv1.NUMAResourcesOperator{}
						Expect(reconciler.Client.Get(context.TODO(), key, nroUpdated)).NotTo(HaveOccurred())
						nroUpdated.Spec.NodeGroups[0].Config.PodsFingerprinting = &updatedPFPMode
						return reconciler.Client.Update(context.TODO(), nroUpdated)
					}).WithPolling(1 * time.Second).WithTimeout(30 * time.Second).ShouldNot(HaveOccurred())

					// we need to do the first iteration here because the DS object is created in the second
					_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())

					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					args = ds.Spec.Template.Spec.Containers[0].Args
					Expect(args).To(ContainElement("--pods-fingerprint-method=all"), "malformed args: %v", args)

					Expect(reconciler.Client.Get(context.TODO(), key, nroUpdated)).ToNot(HaveOccurred())
					Expect(*nroUpdated.Status.NodeGroups[0].Config.PodsFingerprinting).To(Equal(updatedPFPMode), "node group config was not updated under NodeGroupStatus field")
					if platf != platform.HyperShift {
						Expect(*nroUpdated.Status.MachineConfigPools[0].Config.PodsFingerprinting).To(Equal(updatedPFPMode), "node group config was not updated in the operator status")
					}
				})

				It("should keep the manifest tolerations if not set", func() {
					conf := nropv1.DefaultNodeGroupConfig()
					Expect(conf.Tolerations).To(BeEmpty())
					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).To(Succeed())

					Expect(ds.Spec.Template.Spec.Tolerations).To(Equal(reconciler.RTEManifests.DaemonSet.Spec.Template.Spec.Tolerations), "mismatched DS default tolerations")
				})

				It("should add the extra tolerations in the DS objects", func() {
					conf := nropv1.DefaultNodeGroupConfig()
					conf.Tolerations = []corev1.Toleration{
						{
							Key:    "foo",
							Value:  "1",
							Effect: corev1.TaintEffectNoSchedule,
						},
					}
					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).To(Succeed())

					Expect(ds.Spec.Template.Spec.Tolerations).To(Equal(conf.Tolerations), "mismatched DS tolerations")

					nroUpdated := &nropv1.NUMAResourcesOperator{}
					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(nro), nroUpdated)).ToNot(HaveOccurred())
					Expect(nroUpdated.Status.NodeGroups[0].Config.Tolerations).To(Equal(conf.Tolerations), "node group config was not updated under NodeGroupStatus field")
					if platf != platform.HyperShift {
						Expect(nroUpdated.Status.MachineConfigPools[0].Config.Tolerations).To(Equal(conf.Tolerations), "node group config was not updated in the operator status")
					}
				})

				It("should replace the extra tolerations in the DS objects", func() {
					conf := nropv1.DefaultNodeGroupConfig()
					conf.Tolerations = []corev1.Toleration{
						{
							Key:    "foo",
							Value:  "1",
							Effect: corev1.TaintEffectNoSchedule,
						},
					}
					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).To(Succeed())

					Expect(ds.Spec.Template.Spec.Tolerations).To(Equal(conf.Tolerations), "mismatched DS tolerations (round 1)")

					key := client.ObjectKeyFromObject(nro)

					newTols := []corev1.Toleration{
						{
							Key:    "bar",
							Value:  "2",
							Effect: corev1.TaintEffectNoSchedule,
						},
						{
							Key:    "baz",
							Value:  "3",
							Effect: corev1.TaintEffectNoSchedule,
						},
					}
					nroUpdated := &nropv1.NUMAResourcesOperator{}
					Eventually(func() error {
						Expect(reconciler.Client.Get(context.TODO(), key, nroUpdated)).To(Succeed())
						nroUpdated.Spec.NodeGroups[0].Config.Tolerations = newTols
						return reconciler.Client.Update(context.TODO(), nroUpdated)
					}).WithPolling(1 * time.Second).WithTimeout(30 * time.Second).Should(Succeed())

					_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())

					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).To(Succeed())
					Expect(ds.Spec.Template.Spec.Tolerations).To(Equal(nroUpdated.Spec.NodeGroups[0].Config.Tolerations), "mismatched DS tolerations (round 2)")

					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(nro), nroUpdated)).ToNot(HaveOccurred())
					Expect(nroUpdated.Status.NodeGroups[0].Config.Tolerations).To(Equal(newTols), "node group config was not updated under NodeGroupStatus field")
					if platf != platform.HyperShift {
						Expect(nroUpdated.Status.MachineConfigPools[0].Config.Tolerations).To(Equal(newTols), "node group config was not updated in the operator status")
					}
				})

				It("should remove the extra tolerations in the DS objects", func() {
					conf := nropv1.DefaultNodeGroupConfig()
					conf.Tolerations = []corev1.Toleration{
						{
							Key:    "foo",
							Value:  "1",
							Effect: corev1.TaintEffectNoSchedule,
						},
					}
					nro := testobjs.NewNUMAResourcesOperatorWithNodeGroupConfig(objectnames.DefaultNUMAResourcesOperatorCrName, pn, &conf)

					var reconciler *NUMAResourcesOperatorReconciler
					if platf == platform.HyperShift {
						reconciler = reconcileObjectsHypershift(nro)
					} else {
						reconciler = reconcileObjectsOpenshift(nro, mcp)
					}

					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, pn),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).To(Succeed())

					Expect(ds.Spec.Template.Spec.Tolerations).To(Equal(conf.Tolerations), "mismatched DS tolerations")

					key := client.ObjectKeyFromObject(nro)

					Eventually(func() error {
						nroUpdated := &nropv1.NUMAResourcesOperator{}
						Expect(reconciler.Client.Get(context.TODO(), key, nroUpdated)).To(Succeed())
						nroUpdated.Spec.NodeGroups[0].Config.Tolerations = nil
						return reconciler.Client.Update(context.TODO(), nroUpdated)
					}).WithPolling(1 * time.Second).WithTimeout(30 * time.Second).Should(Succeed())

					_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())

					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).To(Succeed())
					Expect(ds.Spec.Template.Spec.Tolerations).To(Equal(reconciler.RTEManifests.DaemonSet.Spec.Template.Spec.Tolerations), "DS tolerations not restored to defaults")

					nroUpdated := &nropv1.NUMAResourcesOperator{}
					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(nro), nroUpdated)).ToNot(HaveOccurred())
					Expect(nroUpdated.Status.NodeGroups[0].Config.Tolerations).To(BeNil(), "node group config was not updated under NodeGroupStatus field")
					if platf != platform.HyperShift {
						Expect(nroUpdated.Status.MachineConfigPools[0].Config.Tolerations).To(BeNil(), "node group config was not updated in the operator status")
					}
				})
			})
		})
	},
		Entry("Openshift Platform", platform.OpenShift),
		Entry("Hypershift Platform", platform.HyperShift),
	)

	Describe("Openshift only", func() {
		Context("[openshift] without available machine config pools", Label("platform:openshift"), func() {
			It("should update the CR condition to degraded when MachineConfigPoolSelector is set", func() {
				ng := nropv1.NodeGroup{
					MachineConfigPoolSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"test": "test"}},
				}
				nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng)
				verifyDegradedCondition(nro, validation.NodeGroupsError, platform.OpenShift)
			})
			It("should update the CR condition to degraded when PoolName set", func() {
				pn := "pn-1"
				ng := nropv1.NodeGroup{
					PoolName: &pn,
				}
				nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng)
				verifyDegradedCondition(nro, validation.NodeGroupsError, platform.OpenShift)
			})
		})

		Context("[openshift] with two node groups each with different pool specifier type and both point to same MCP", Label("platform:openshift"), func() {
			It("should update the CR condition to degraded", func() {
				mcpName := "test1"
				label1 := map[string]string{
					"test1": "test1",
				}

				ng1 := nropv1.NodeGroup{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"test": "test"},
					},
				}
				ng2 := nropv1.NodeGroup{
					PoolName: &mcpName,
				}
				nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng1, ng2)

				mcp1 := testobjs.NewMachineConfigPool(mcpName, label1, &metav1.LabelSelector{MatchLabels: label1}, &metav1.LabelSelector{MatchLabels: label1})

				var err error
				reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcp1)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(nro)
				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))

				Expect(reconciler.Client.Get(context.TODO(), key, nro)).ToNot(HaveOccurred())
				degradedCondition := getConditionByType(nro.Status.Conditions, status.ConditionDegraded)
				Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue))
				Expect(degradedCondition.Reason).To(Equal(validation.NodeGroupsError))
			})
		})

		Context("[openshift] with node group with MCP selector that matches more than one MCP", Label("platform:openshift"), func() {
			It("should update the CR condition to degraded when annotation is not enabled but still create all needed objects", func() {
				mcpName1 := "test1"
				label1 := map[string]string{
					"test1": "test1",
					"test":  "common",
				}

				mcpName2 := "test2"
				label2 := map[string]string{
					"test2": "test2",
					"test":  "common",
				}
				ng1 := nropv1.NodeGroup{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"test": "common"},
					},
				}

				nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng1)

				mcp1 := testobjs.NewMachineConfigPool(mcpName1, label1, &metav1.LabelSelector{MatchLabels: label1}, &metav1.LabelSelector{MatchLabels: label1})
				mcp2 := testobjs.NewMachineConfigPool(mcpName2, label2, &metav1.LabelSelector{MatchLabels: label2}, &metav1.LabelSelector{MatchLabels: label2})

				var err error
				reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcp1, mcp2)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(nro)
				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))

				// we still expect that all objects be created as usual without blockers (no hard requirement for now) in addition to the Degraded condition
				By("Verify all needed objects were created as expected: CRD, MCP, RTE DSs")
				crd := &apiextensionsv1.CustomResourceDefinition{}
				crdKey := client.ObjectKey{
					Name: "noderesourcetopologies.topology.node.k8s.io",
				}
				Expect(reconciler.Client.Get(context.TODO(), crdKey, crd)).ToNot(HaveOccurred())
				Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp1), mcp1)).To(Succeed())
				Expect(mcp1.Status.Configuration.Source).To(BeNil()) // default RTE SElinux policy don't expect MCs
				Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp2), mcp2)).To(Succeed())
				Expect(mcp2.Status.Configuration.Source).To(BeNil()) // default RTE SElinux policy don't expect MCs
				mcpDSKey := client.ObjectKey{
					Name:      objectnames.GetComponentName(nro.Name, mcp1.Name),
					Namespace: testNamespace,
				}
				ds := &appsv1.DaemonSet{}
				Expect(reconciler.Client.Get(context.TODO(), mcpDSKey, ds)).ToNot(HaveOccurred())
				mcpDSKey.Name = objectnames.GetComponentName(nro.Name, mcp2.Name)
				Expect(reconciler.Client.Get(context.TODO(), mcpDSKey, ds)).To(Succeed())

				By("Verify operator condition is Degraded")
				Expect(reconciler.Client.Get(context.TODO(), key, nro)).ToNot(HaveOccurred())
				degradedCondition := getConditionByType(nro.Status.Conditions, status.ConditionDegraded)
				Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue))
				Expect(degradedCondition.Reason).To(Equal(validation.NodeGroupsError))
			})
			It("should create objects and CR will be in Available condition when annotation is enabled - legacy", func() {
				mcpName1 := "test1"
				label1 := map[string]string{
					"test1": "test1",
					"test":  "common",
				}

				mcpName2 := "test2"
				label2 := map[string]string{
					"test2": "test2",
					"test":  "common",
				}
				ng1 := nropv1.NodeGroup{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"test": "common"},
					},
				}

				nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng1)
				nro.Annotations = map[string]string{
					annotations.MultiplePoolsPerTreeAnnotation: annotations.MultiplePoolsPerTreeEnabled,
					annotations.SELinuxPolicyConfigAnnotation:  annotations.SELinuxPolicyCustom,
				}
				mcp1 := testobjs.NewMachineConfigPool(mcpName1, label1, &metav1.LabelSelector{MatchLabels: label1}, &metav1.LabelSelector{MatchLabels: label1})
				mcp2 := testobjs.NewMachineConfigPool(mcpName2, label2, &metav1.LabelSelector{MatchLabels: label2}, &metav1.LabelSelector{MatchLabels: label2})

				var err error
				reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcp1, mcp2)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(nro)
				// on the first iteration we expect the CRDs and MCPs to be created, yet, it will wait one minute to update MC, thus RTE daemonsets and complete status update is not going to be achieved at this point
				firstLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(firstLoopResult).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

				// Ensure mcp1 is ready
				Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp1), mcp1)).To(Succeed())
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
				Expect(reconciler.Client.Update(context.TODO(), mcp1)).To(Succeed())

				// ensure mcp2 is ready
				Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp2), mcp2)).To(Succeed())
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
				Expect(reconciler.Client.Update(context.TODO(), mcp2)).To(Succeed())

				// triggering a second reconcile will create the RTEs and fully update the statuses making the operator in Available condition -> no more reconciliation needed thus the result is clean
				secondLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(secondLoopResult).To(Equal(reconcile.Result{}))

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
				Expect(reconciler.Client.Get(context.TODO(), mcp2DSKey, ds)).To(Succeed())

				Expect(reconciler.Client.Get(context.TODO(), key, nro)).ToNot(HaveOccurred())
				availableCondition := getConditionByType(nro.Status.Conditions, status.ConditionAvailable)
				Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
			})
		})

		Context("[openshift] with two node groups while both point to same pool using same pool specifier", Label("platform:openshift"), func() {
			It("should update the CR condition to degraded - MachineConfigSelector", func() {
				mcpName := "test1"
				label := map[string]string{
					"test1": "test1",
					"test2": "test2",
				}

				ng1 := nropv1.NodeGroup{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"test1": "test1"},
					},
				}
				ng2 := nropv1.NodeGroup{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"test2": "test2"},
					},
				}

				nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng1, ng2)
				mcp := testobjs.NewMachineConfigPool(mcpName, label, &metav1.LabelSelector{MatchLabels: label}, &metav1.LabelSelector{MatchLabels: label})

				var err error
				reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcp)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(nro)
				result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{}))

				Expect(reconciler.Client.Get(context.TODO(), key, nro)).ToNot(HaveOccurred())
				degradedCondition := getConditionByType(nro.Status.Conditions, status.ConditionDegraded)
				Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue))
				Expect(degradedCondition.Reason).To(Equal(validation.NodeGroupsError))
			})
		})

		Context("[openshift] with correct NRO CR", Label("platform:openshift"), func() {
			var nro *nropv1.NUMAResourcesOperator
			var mcp1 *machineconfigv1.MachineConfigPool
			var mcp2 *machineconfigv1.MachineConfigPool

			var reconciler *NUMAResourcesOperatorReconciler
			var label1 map[string]string
			var key client.ObjectKey

			BeforeEach(func() {
				label1 = map[string]string{
					"test1": "test1",
				}
				label2 := map[string]string{
					"test2": "test2",
				}

				ng1 := nropv1.NodeGroup{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: label1,
					},
				}
				ng2 := nropv1.NodeGroup{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: label2,
					},
				}
				nro = testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng1, ng2)
				key = client.ObjectKeyFromObject(nro)
				nro.Annotations = map[string]string{annotations.SELinuxPolicyConfigAnnotation: annotations.SELinuxPolicyCustom}

				mcp1 = testobjs.NewMachineConfigPool("test1", label1, &metav1.LabelSelector{MatchLabels: label1}, &metav1.LabelSelector{MatchLabels: label1})
				mcp2 = testobjs.NewMachineConfigPool("test2", label2, &metav1.LabelSelector{MatchLabels: label2}, &metav1.LabelSelector{MatchLabels: label2})
			})

			Context("test MCP selector labels", func() {
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

							firstLoopResult, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
							Expect(err).ToNot(HaveOccurred())
						})
						It("should create CRD, machine configs and wait for MCPs updates", func() {
							// check reconcile loop result
							// wait one minute to update MCP, thus RTE daemonsets and complete status update is not going to be achieved at this point
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
						var result reconcile.Result
						When("machine config pools still are not ready", func() {
							BeforeEach(func() {
								var err error

								result, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
								Expect(err).ToNot(HaveOccurred())
							})
							It("should wait", func() {
								// check reconcile first loop result
								// wait one minute to update MCP, thus RTE daemonsets and complete status update is not going to be achieved at this point
								Expect(result).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

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
								Expect(reconciler.Client.Update(context.TODO(), mcp1))

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
								Expect(reconciler.Client.Update(context.TODO(), mcp2))

								result, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
								Expect(err).ToNot(HaveOccurred())
							})
							It("should continue with creation of additional components", func() {
								// check reconcile second loop result
								//	triggering a second reconcile will create the RTEs and fully update the statuses making the operator in Available condition -> no more reconciliation needed thus the result is clean
								Expect(result).To(Equal(reconcile.Result{}))

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
							When("daemonsets are ready", func() {
								var dsDesiredNumberScheduled int32
								var dsNumReady int32
								BeforeEach(func() {
									dsDesiredNumberScheduled = reconciler.RTEManifests.DaemonSet.Status.DesiredNumberScheduled
									dsNumReady = reconciler.RTEManifests.DaemonSet.Status.NumberReady

									reconciler.RTEManifests.DaemonSet.Status.DesiredNumberScheduled = int32(len(nro.Spec.NodeGroups))
									reconciler.RTEManifests.DaemonSet.Status.NumberReady = reconciler.RTEManifests.DaemonSet.Status.DesiredNumberScheduled

									_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
									Expect(err).ToNot(HaveOccurred())
								})
								AfterEach(func() {
									reconciler.RTEManifests.DaemonSet.Status.DesiredNumberScheduled = dsDesiredNumberScheduled
									reconciler.RTEManifests.DaemonSet.Status.NumberReady = dsNumReady

									_, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
									Expect(err).ToNot(HaveOccurred())
								})
								It(" operator status should report RelatedObjects as expected", func() {
									By("Getting updated NROP Status")
									nroUpdated := &nropv1.NUMAResourcesOperator{}
									Expect(reconciler.Client.Get(context.TODO(), key, nroUpdated)).ToNot(HaveOccurred())

									//Should have this object references ...
									expected := []configv1.ObjectReference{
										{
											Resource: "namespaces",
											Name:     reconciler.Namespace,
										},
										{
											Group:    "machineconfiguration.openshift.io",
											Resource: "kubeletconfigs",
										},
										{
											Group:    "machineconfiguration.openshift.io",
											Resource: "machineconfigs",
										},
										{
											Group:    "topology.node.k8s.io",
											Resource: "noderesourcetopologies",
										},
									}
									// ... and one for each DaemonSet
									for _, ds := range nroUpdated.Status.DaemonSets {
										expected = append(expected, configv1.ObjectReference{
											Group:     "apps",
											Resource:  "daemonsets",
											Namespace: ds.Namespace,
											Name:      ds.Name,
										})
									}

									Expect(len(nroUpdated.Status.RelatedObjects)).To(Equal(len(expected)))
									Expect(nroUpdated.Status.RelatedObjects).To(ContainElements(expected))
								})
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

									// immediate update reflection with no reboot needed -> no need to reconcileafter this
									firstLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
									Expect(err).ToNot(HaveOccurred())
									Expect(firstLoopResult).To(Equal(reconcile.Result{}))
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
							// on the first iteration we expect the CRDs and MCPs to be created, yet, it will wait one minute to update MC, thus RTE daemonsets and complete status update is not going to be achieved at this point
							firstLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
							Expect(err).ToNot(HaveOccurred())
							Expect(firstLoopResult).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

							mc := &machineconfigv1.MachineConfig{}
							mcKey := client.ObjectKey{
								Name: objectnames.GetMachineConfigName(nro.Name, mcpWithComplexMachineConfigSelector.Name),
							}
							Expect(reconciler.Client.Get(context.TODO(), mcKey, mc)).ToNot(HaveOccurred())
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
							// degraded condition doesn't require reconciliation thus expect empty reconcile result
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

			When("NRO updated to remove the custom policy annotation", func() {
				BeforeEach(func() {
					var err error
					reconciler, err = NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcp1, mcp2)
					Expect(err).ToNot(HaveOccurred())

					key := client.ObjectKeyFromObject(nro)
					// on the first iteration we expect the CRDs and MCPs to be created, yet, it will wait one minute to update MC, thus RTE daemonsets and complete status update is not going to be achieved at this point
					firstLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
					Expect(firstLoopResult).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

					// Ensure mcp1 is ready
					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp1), mcp1)).To(Succeed())
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
					Expect(reconciler.Client.Update(context.TODO(), mcp1)).To(Succeed())

					// ensure mcp2 is ready
					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp2), mcp2)).To(Succeed())
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
					Expect(reconciler.Client.Update(context.TODO(), mcp2)).To(Succeed())

					// triggering a second reconcile will create the RTEs and fully update the statuses making the operator in Available condition -> no more reconciliation needed thus the result is clean
					secondLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
					Expect(secondLoopResult).To(Equal(reconcile.Result{}))

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
					Expect(reconciler.Client.Get(context.TODO(), mcp2DSKey, ds)).To(Succeed())
					// check we have at least two NodeGroups
					Expect(len(nro.Spec.NodeGroups)).To(BeNumerically(">", 1))

					By("Update NRO to have both NodeGroups")
					nro := &nropv1.NUMAResourcesOperator{}
					Expect(reconciler.Client.Get(context.TODO(), key, nro)).NotTo(HaveOccurred())

					nro.Annotations = map[string]string{}
					Expect(reconciler.Client.Update(context.TODO(), nro)).NotTo(HaveOccurred())

					// removing the annotation will trigger reboot which requires resync after 1 min
					thirdLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
					Expect(thirdLoopResult).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))
				})
				It("should not create a machine config", func() {
					mc := &machineconfigv1.MachineConfig{}

					// Check mc1 not created
					mc1Key := client.ObjectKey{
						Name: objectnames.GetMachineConfigName(nro.Name, mcp1.Name),
					}
					err := reconciler.Client.Get(context.TODO(), mc1Key, mc)
					Expect(apierrors.IsNotFound(err)).To(BeTrue(), "MachineConfig %s is expected to not be found", mc1Key.String())

					// Check mc1 not created
					mc2Key := client.ObjectKey{
						Name: objectnames.GetMachineConfigName(nro.Name, mcp2.Name),
					}
					err = reconciler.Client.Get(context.TODO(), mc2Key, mc)
					Expect(apierrors.IsNotFound(err)).To(BeTrue(), "MachineConfig %s is expected to not be found", mc2Key.String())
				})
			})

			Context("with correct NRO and SELinuxPolicyConfigAnnotation not set", func() {
				BeforeEach(func() {
					nro.Annotations = map[string]string{}

					var err error
					reconciler, err = NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcp1, mcp2)
					Expect(err).ToNot(HaveOccurred())

					key := client.ObjectKeyFromObject(nro)
					// when the SELinux custom annotation is not set, the controller will not wait for
					// the selinux update on MC thus no reboot is required hence no need to reconcile again
					firstLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
					Expect(err).ToNot(HaveOccurred())
					Expect(firstLoopResult).To(Equal(reconcile.Result{}))
				})

				It("should create RTE daemonsets from the first reconcile iteration - MachineConfigPoolSelector", func() {
					// all objects should be created from the first reconciliation
					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp1), mcp1)).To(Succeed())
					Expect(mcp1.Status.Configuration.Source).To(BeEmpty()) // no need to wait for MC update
					Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp2), mcp2)).To(Succeed())
					Expect(mcp2.Status.Configuration.Source).To(BeEmpty()) // no need to wait for MC update

					By("Check DaemonSet is created")
					dsKey := client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, mcp1.Name),
						Namespace: testNamespace,
					}
					ds := &appsv1.DaemonSet{}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					dsKey = client.ObjectKey{
						Name:      objectnames.GetComponentName(nro.Name, mcp2.Name),
						Namespace: testNamespace,
					}
					Expect(reconciler.Client.Get(context.TODO(), dsKey, ds)).ToNot(HaveOccurred())

					Expect(reconciler.Client.Get(context.TODO(), key, nro)).ToNot(HaveOccurred())
					availableCondition := getConditionByType(nro.Status.Conditions, status.ConditionAvailable)
					Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
				})
			})

		})

		Context("[openshift] emulating upgrade from 4.1X to 4.18 which has a built-in selinux policy for RTE pods", Label("platform:openshift"), func() {
			var nro *nropv1.NUMAResourcesOperator
			var mcp1 *machineconfigv1.MachineConfigPool
			var mcp2 *machineconfigv1.MachineConfigPool

			var reconciler *NUMAResourcesOperatorReconciler

			BeforeEach(func() {
				label1 := map[string]string{
					"test1": "test1",
				}
				label2 := map[string]string{
					"test2": "test2",
				}

				ng1 := nropv1.NodeGroup{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: label1,
					},
				}
				ng2 := nropv1.NodeGroup{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: label2,
					},
				}
				nro = testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, ng1, ng2)
				// reconciling NRO object with custom policy, emulates the old behavior version
				nro.Annotations = map[string]string{annotations.SELinuxPolicyConfigAnnotation: annotations.SELinuxPolicyCustom}

				mcp1 = testobjs.NewMachineConfigPool("test1", label1, &metav1.LabelSelector{MatchLabels: label1}, &metav1.LabelSelector{MatchLabels: label1})
				mcp2 = testobjs.NewMachineConfigPool("test2", label2, &metav1.LabelSelector{MatchLabels: label2}, &metav1.LabelSelector{MatchLabels: label2})

				var err error
				reconciler, err = NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcp1, mcp2)
				Expect(err).ToNot(HaveOccurred())

				key := client.ObjectKeyFromObject(nro)
				// on the first iteration we expect the CRDs and MCPs to be created, yet, it will wait one minute to update MC, thus RTE daemonsets and complete status update is not going to be achieved at this point
				firstLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(firstLoopResult).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))

				// Ensure mcp1 is ready
				Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp1), mcp1)).To(Succeed())
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
				Expect(reconciler.Client.Update(context.TODO(), mcp1)).To(Succeed())

				// ensure mcp2 is ready
				Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp2), mcp2)).To(Succeed())
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
				Expect(reconciler.Client.Update(context.TODO(), mcp2)).To(Succeed())

				// triggering a second reconcile will create the RTEs and fully update the statuses making the operator in Available condition -> no more reconciliation needed thus the result is clean
				secondLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(secondLoopResult).To(Equal(reconcile.Result{RequeueAfter: 0}))

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
				Expect(reconciler.Client.Get(context.TODO(), mcp2DSKey, ds)).To(Succeed())

				By("upgrading from 4.1X to 4.18")
				Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(nro), nro)).To(Succeed())
				nro.Annotations = map[string]string{}
				Expect(reconciler.Client.Update(context.TODO(), nro)).To(Succeed())

				// removing the annotation will trigger reboot which requires resync after 1 min
				thirdLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
				Expect(err).ToNot(HaveOccurred())
				Expect(thirdLoopResult).To(Equal(reconcile.Result{RequeueAfter: time.Minute}))
			})
			It("should delete existing mc", func() {
				mc1Key := client.ObjectKey{
					Name: objectnames.GetMachineConfigName(nro.Name, mcp1.Name),
				}
				mc := &machineconfigv1.MachineConfig{}
				err := reconciler.Client.Get(context.TODO(), mc1Key, mc)
				Expect(apierrors.IsNotFound(err)).To(BeTrue(), "MachineConfig %s expected to be deleted; err=%v", mc1Key.Name, err)

				mc2Key := client.ObjectKey{
					Name: objectnames.GetMachineConfigName(nro.Name, mcp2.Name),
				}
				err = reconciler.Client.Get(context.TODO(), mc2Key, mc)
				Expect(apierrors.IsNotFound(err)).To(BeTrue(), "MachineConfig %s expected to be deleted; err=%v", mc2Key.Name, err)
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

func reconcileObjectsOpenshift(nro *nropv1.NUMAResourcesOperator, mcp *machineconfigv1.MachineConfigPool) *NUMAResourcesOperatorReconciler {
	GinkgoHelper()

	reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.OpenShift, defaultOCPVersion, nro, mcp)
	Expect(err).ToNot(HaveOccurred())

	key := client.ObjectKeyFromObject(nro)

	// immediate update by default
	firstLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
	Expect(err).ToNot(HaveOccurred())
	expectedResult := reconcile.Result{}
	if annotations.IsCustomPolicyEnabled(nro.Annotations) {
		expectedResult = reconcile.Result{RequeueAfter: time.Minute}
	}
	Expect(firstLoopResult).To(Equal(expectedResult))

	By("Ensure MachineConfigPools is ready")
	Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp), mcp)).ToNot(HaveOccurred())

	var mcName string
	if annotations.IsCustomPolicyEnabled(nro.Annotations) {
		mcp.Status.Configuration.Source = []corev1.ObjectReference{
			{
				Name: objectnames.GetMachineConfigName(nro.Name, mcp.Name),
			},
		}
		mcName = objectnames.GetMachineConfigName(nro.Name, mcp.Name)
		mcp.Status.Configuration.Source[0].Name = mcName
	}

	mcp.Status.Conditions = []machineconfigv1.MachineConfigPoolCondition{
		{
			Type:   machineconfigv1.MachineConfigPoolUpdated,
			Status: corev1.ConditionTrue,
		},
	}

	Expect(reconciler.Client.Update(context.TODO(), mcp)).Should(Succeed())
	Expect(reconciler.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp), mcp)).ToNot(HaveOccurred())
	Expect(mcp.Status.Conditions[0].Type).To(Equal(machineconfigv1.MachineConfigPoolUpdated))
	Expect(mcp.Status.Conditions[0].Status).To(Equal(corev1.ConditionTrue))

	if annotations.IsCustomPolicyEnabled(nro.Annotations) {
		Expect(mcp.Status.Configuration.Source[0].Name).To(Equal(mcName))

		// triggering a second reconcile will create the RTEs and fully update the statuses making the operator in Available condition -> no more reconciliation needed thus the result is clean
		secondLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())
		Expect(secondLoopResult).To(Equal(reconcile.Result{}))
	}

	return reconciler
}

func reconcileObjectsHypershift(nro *nropv1.NUMAResourcesOperator) *NUMAResourcesOperatorReconciler {
	GinkgoHelper()

	reconciler, err := NewFakeNUMAResourcesOperatorReconciler(platform.HyperShift, defaultOCPVersion, nro)
	Expect(err).ToNot(HaveOccurred())

	key := client.ObjectKeyFromObject(nro)

	// immediate update
	firstLoopResult, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
	Expect(err).ToNot(HaveOccurred())
	Expect(firstLoopResult).To(Equal(reconcile.Result{}))

	return reconciler
}
