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

package install

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/assets/selinux"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	inthelper "github.com/openshift-kni/numaresources-operator/internal/api/annotations/helper"
	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	nrowait "github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	"github.com/openshift-kni/numaresources-operator/test/internal/crds"
	"github.com/openshift-kni/numaresources-operator/test/internal/deploy"
	e2eimages "github.com/openshift-kni/numaresources-operator/test/internal/images"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// tests here are not interruptible, so they should not accept contexts.
// See: https://onsi.github.io/ginkgo/#interruptible-nodes-and-speccontext

const (
	containerNameRTE = "resource-topology-exporter"
)

var _ = Describe("[Install] continuousIntegration", Serial, func() {
	var initialized bool

	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")
		}
		initialized = true
	})

	Context("with a running cluster with all the components", func() {
		It("[test_id:47574] should perform overall deployment and verify the condition is reported as available", Label(label.Tier0), func() {
			deployer := deploy.NewForPlatform(configuration.Plat)
			nroObj := deployer.Deploy(context.TODO(), configuration.MachineConfigPoolUpdateTimeout)
			nname := client.ObjectKeyFromObject(nroObj)
			Expect(nname.Name).ToNot(BeEmpty())

			By("checking that the condition Available=true")
			updatedNROObj := &nropv1.NUMAResourcesOperator{}
			immediate := true
			err := wait.PollUntilContextTimeout(context.Background(), 10*time.Second, 5*time.Minute, immediate, func(ctx context.Context) (bool, error) {
				err := e2eclient.Client.Get(ctx, nname, updatedNROObj)
				if err != nil {
					klog.ErrorS(err, "failed to get the NRO resource")
					return false, err
				}

				cond := status.FindCondition(updatedNROObj.Status.Conditions, status.ConditionAvailable)
				if cond == nil {
					klog.Infof("missing conditions in %v", updatedNROObj)
					return false, err
				}

				klog.Infof("condition for %s: %v", nname.Name, cond)
				return cond.Status == metav1.ConditionTrue, nil
			})
			if err != nil {
				logRTEPodsLogs(e2eclient.Client, e2eclient.K8sClient, context.TODO(), updatedNROObj, "NRO never reported available")
			}
			Expect(err).ToNot(HaveOccurred(), "NRO never reported available")

			By("checking the NRT CRD is deployed")
			_, err = crds.GetByName(e2eclient.Client, crds.CrdNRTName)
			Expect(err).ToNot(HaveOccurred())

			By("checking the NRO CRD is deployed")
			_, err = crds.GetByName(e2eclient.Client, crds.CrdNROName)
			Expect(err).ToNot(HaveOccurred())

			By("checking Daemonset is up&running")
			Eventually(func() bool {
				ds, err := getDaemonSetByOwnerReference(updatedNROObj.UID)
				if err != nil {
					klog.ErrorS(err, "unable to get Daemonset")
					return false
				}

				if ds.Status.NumberMisscheduled != 0 {
					klog.Infof(" Misscheduled: There are %d nodes that should not be running Daemon pod but are", ds.Status.NumberMisscheduled)
					return false
				}

				if ds.Status.NumberUnavailable != 0 {
					klog.Infof(" NumberUnavailable %d (should be 0)", ds.Status.NumberUnavailable)
					return false
				}

				if ds.Status.CurrentNumberScheduled != ds.Status.DesiredNumberScheduled {
					klog.Infof(" CurrentNumberScheduled %d (should be %d)", ds.Status.CurrentNumberScheduled, ds.Status.DesiredNumberScheduled)
					return false
				}

				if ds.Status.NumberReady != ds.Status.DesiredNumberScheduled {
					klog.Infof(" NumberReady %d (should be %d)", ds.Status.CurrentNumberScheduled, ds.Status.DesiredNumberScheduled)
					return false
				}
				return true
			}).WithTimeout(1*time.Minute).WithPolling(5*time.Second).Should(BeTrue(), "DaemonSet Status was not correct")

			By("checking DaemonSet pods are running with correct SELinux context")
			ds, err := getDaemonSetByOwnerReference(updatedNROObj.UID)
			Expect(err).ToNot(HaveOccurred())
			rteContainer, err := findContainerByName(*ds, containerNameRTE)
			Expect(err).ToNot(HaveOccurred())
			Expect(rteContainer.SecurityContext.SELinuxOptions.Type).To(Equal(selinux.RTEContextType), "container %s is running with wrong selinux context", rteContainer.Name)
		})
	})
})

var _ = Describe("[Install] durability", Serial, func() {
	var initialized bool

	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")
		}
		initialized = true
	})

	Context("with deploying NUMAResourcesOperator with wrong name", func() {
		It("should do nothing", func() {
			nroObj := objects.TestNRO(objects.NROWithMCPSelector(objects.EmptyMatchLabels()))
			nroObj.Name = "wrong-name"

			err := e2eclient.Client.Create(context.TODO(), nroObj)
			Expect(err).ToNot(HaveOccurred())

			By("checking that the condition Degraded=true")
			Eventually(func() bool {
				updatedNROObj := &nropv1.NUMAResourcesOperator{}
				err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroObj), updatedNROObj)
				if err != nil {
					klog.ErrorS(err, "failed to get the  NUMAResourcesOperator CR")
					return false
				}

				cond := status.FindCondition(updatedNROObj.Status.Conditions, status.ConditionDegraded)
				if cond == nil {
					klog.Infof("missing conditions in %v", updatedNROObj)
					return false
				}

				klog.Infof("condition: %v", cond)

				return cond.Status == metav1.ConditionTrue
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(BeTrue(), "NUMAResourcesOperator condition did not become degraded")

			deleteNROPSync(e2eclient.Client, nroObj)
		})
	})

	Context("with a running cluster with all the components and overall deployment", func() {
		var deployer deploy.Deployer
		var nroObj *nropv1.NUMAResourcesOperator

		BeforeEach(func() {
			deployer = deploy.NewForPlatform(configuration.Plat)
			nroObj = deployer.Deploy(context.TODO(), configuration.MachineConfigPoolUpdateTimeout)
		})

		AfterEach(func() {
			deployer.Teardown(context.TODO(), 5*time.Minute)
		})

		It("[test_id:47587] should restart RTE DaemonSet when image is updated in NUMAResourcesOperator", Label(label.Tier1), func() {
			By("getting up-to-date NRO object")
			nroKey := objects.NROObjectKey()

			immediate := true
			err := wait.PollUntilContextTimeout(context.Background(), 10*time.Second, 10*time.Minute, immediate, func(ctx context.Context) (bool, error) {
				err := e2eclient.Client.Get(ctx, nroKey, nroObj)
				if err != nil {
					return false, err
				}
				if len(nroObj.Status.DaemonSets) != 1 {
					klog.Infof("unsupported daemonsets (/MCP) count: %d", len(nroObj.Status.DaemonSets))
					return false, nil
				}
				return true, nil
			})
			if err != nil {
				logRTEPodsLogs(e2eclient.Client, e2eclient.K8sClient, context.TODO(), nroObj, "NRO not available")
			}
			Expect(err).NotTo(HaveOccurred(), "inconsistent NRO instance:\n%s", objects.ToYAML(nroObj))

			dsKey := nrowait.ObjectKey{
				Namespace: nroObj.Status.DaemonSets[0].Namespace,
				Name:      nroObj.Status.DaemonSets[0].Name,
			}

			By("waiting for DaemonSet to be ready")
			ds, err := nrowait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(context.TODO(), dsKey)
			Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

			By("Update RTE image in NRO")
			Eventually(func() error {
				err := e2eclient.Client.Get(context.TODO(), nroKey, nroObj)
				if err != nil {
					return err
				}
				nroObj.Spec.ExporterImage = e2eimages.RTETestImageCI
				return e2eclient.Client.Update(context.TODO(), nroObj)
			}).WithTimeout(3 * time.Minute).WithPolling(10 * time.Second).ShouldNot(HaveOccurred())

			By("waiting for the daemonset to be ready again")
			Eventually(func() bool {
				updatedDs := &appsv1.DaemonSet{}
				err := e2eclient.Client.Get(context.TODO(), dsKey.AsKey(), updatedDs)
				if err != nil {
					klog.ErrorS(err, "failed to get the daemonset", "key", dsKey.String())
					return false
				}

				if !nrowait.AreDaemonSetPodsReady(&updatedDs.Status) {
					klog.Infof("daemonset %s desired %d scheduled %d ready %d",
						dsKey.String(),
						updatedDs.Status.DesiredNumberScheduled,
						updatedDs.Status.CurrentNumberScheduled,
						updatedDs.Status.NumberReady)
					return false
				}

				klog.Infof("daemonset %s ready", dsKey.String())

				klog.Infof("daemonset Generation observed %v current %v", updatedDs.Status.ObservedGeneration, ds.Generation)
				isUpdated := updatedDs.Status.ObservedGeneration > ds.Generation
				if !isUpdated {
					return false
				}
				ds = updatedDs
				return true
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(BeTrue(), "failed to get up to date DaemonSet")

			rteContainer, err := findContainerByName(*ds, containerNameRTE)
			Expect(err).ToNot(HaveOccurred())

			Expect(rteContainer.Image).To(BeIdenticalTo(e2eimages.RTETestImageCI))
		})

		It("should be able to delete NUMAResourceOperator CR and redeploy without polluting cluster state", func() {
			nname := client.ObjectKeyFromObject(nroObj)
			Expect(nname.Name).NotTo(BeEmpty())

			err := e2eclient.Client.Get(context.TODO(), objects.NROObjectKey(), nroObj)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the DaemonSet to be created..")
			uid := nroObj.GetUID()
			ds := &appsv1.DaemonSet{}
			Eventually(func() error {
				var err error
				ds, err = getDaemonSetByOwnerReference(uid)
				return err
			}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

			deleteNROPSync(e2eclient.Client, nroObj)

			deploy.WaitForMCPUpdatedAfterNRODeleted(nroObj)

			By("checking there are no leftovers")
			// by taking the ns from the ds we're avoiding the need to figure out in advanced
			// at which ns we should look for the resources
			mf, err := rte.GetManifests(configuration.Plat, configuration.PlatVersion, ds.Namespace, true, inthelper.IsCustomPolicyEnabled(nroObj))
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				objs := mf.ToObjects()
				for _, obj := range objs {
					key := client.ObjectKeyFromObject(obj)
					if err := e2eclient.Client.Get(context.TODO(), key, obj); !errors.IsNotFound(err) {
						if err == nil {
							klog.Infof("obj %s still exists", key.String())
						} else {
							klog.ErrorS(err, "obj return with error", "key", key.String())
						}
						return false
					}
				}
				return true
			}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())

			By("redeploy with other parameters")
			nroObjRedep := objects.TestNRO(objects.NROWithMCPSelector(objects.EmptyMatchLabels()))
			nroObjRedep.Spec = *nroObj.Spec.DeepCopy()
			// TODO change to an image which is test dedicated
			nroObjRedep.Spec.ExporterImage = e2eimages.RTETestImageCI

			var mcps []*machineconfigv1.MachineConfigPool
			if inthelper.IsCustomPolicyEnabled(nroObj) {
				// need to get MCPs before the mutation
				mcps, err = nropmcp.GetListByNodeGroupsV1(context.TODO(), e2eclient.Client, nroObj.Spec.NodeGroups)
				Expect(err).NotTo(HaveOccurred())
			}

			err = e2eclient.Client.Create(context.TODO(), nroObjRedep)
			Expect(err).ToNot(HaveOccurred())

			if inthelper.IsCustomPolicyEnabled(nroObj) {
				Expect(deploy.WaitForMCPsCondition(e2eclient.Client, context.TODO(), machineconfigv1.MachineConfigPoolUpdated, mcps...)).To(Succeed())
			}

			Eventually(func() bool {
				updatedNroObj := &nropv1.NUMAResourcesOperator{}
				err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroObj), updatedNroObj)
				Expect(err).ToNot(HaveOccurred())

				ds, err := getDaemonSetByOwnerReference(updatedNroObj.GetUID())
				if err != nil {
					// TODO: multi-line value in structured log
					klog.ErrorS(err, "failed to get the RTE DaemonSet", "nroYAML", objects.ToYAML(updatedNroObj))
					return false
				}

				return ds.Spec.Template.Spec.Containers[0].Image == e2eimages.RTETestImageCI
			}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
		})
		It("should have the desired topology manager configuration under the NRT object", func() {
			rteConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				updatedConfigMaps := &corev1.ConfigMapList{}
				opts := client.ListOptions{LabelSelector: labels.SelectorFromSet(map[string]string{rteconfig.LabelOperatorName: nroObj.Name})}
				err := e2eclient.Client.List(context.TODO(), updatedConfigMaps, &opts)
				Expect(err).ToNot(HaveOccurred())

				if len(updatedConfigMaps.Items) != 1 {
					klog.Infof("expected exactly 1 RTE configmap, got: %d", len(updatedConfigMaps.Items))
					return false
				}
				rteConfigMap = &updatedConfigMaps.Items[0]
				return true
			}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
			klog.InfoS("found RTE configmap", "rteConfigMap", rteConfigMap)

			cfg, err := configuration.ValidateAndExtractRTEConfigData(rteConfigMap)
			Expect(err).ToNot(HaveOccurred())

			By("checking that NRT reflects the correct data from RTE configmap")
			Eventually(func() bool {
				updatedNrtObjs := &nrtv1alpha2.NodeResourceTopologyList{}
				err := e2eclient.Client.List(context.TODO(), updatedNrtObjs)
				Expect(err).ToNot(HaveOccurred())

				for i := range updatedNrtObjs.Items {
					nrt := &updatedNrtObjs.Items[i]
					// in this specific test deployment,
					// the same configuration should apply to all NRT objects
					matchingErr := configuration.CheckTopologyManagerConfigMatching(nrt, &cfg)
					if matchingErr != "" {
						klog.InfoS("NRT doesn't match topologyManager configuration", "name", nrt.Name, "problem", matchingErr)
						return false
					}
				}
				return true
			}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
		})
	})
})

func findContainerByName(daemonset appsv1.DaemonSet, containerName string) (*corev1.Container, error) {
	//shortcut
	containers := daemonset.Spec.Template.Spec.Containers

	if len(containers) == 0 {
		return nil, fmt.Errorf("there are no containers")
	}
	if containerName == "" {
		return &containers[0], nil
	}

	for idx := 0; idx < len(containers); idx++ {
		cnt := &containers[idx]
		if cnt.Name == containerName {
			return cnt, nil
		}
	}
	return nil, fmt.Errorf("container %q not found in %s/%s", containerName, daemonset.Namespace, daemonset.Name)
}

func deleteNROPSync(cli client.Client, nropObj *nropv1.NUMAResourcesOperator) {
	GinkgoHelper()
	var err error
	err = cli.Delete(context.TODO(), nropObj)
	Expect(err).ToNot(HaveOccurred())
	err = nrowait.With(cli).Interval(10*time.Second).Timeout(2*time.Minute).ForNUMAResourcesOperatorDeleted(context.TODO(), nropObj)
	Expect(err).ToNot(HaveOccurred(), "NROP %q failed to be deleted", nropObj.Name)
}

func getDaemonSetByOwnerReference(uid types.UID) (*appsv1.DaemonSet, error) {
	dsList := &appsv1.DaemonSetList{}

	if err := e2eclient.Client.List(context.TODO(), dsList); err != nil {
		return nil, fmt.Errorf("failed to get daemonset: %w", err)
	}

	for _, ds := range dsList.Items {
		for _, or := range ds.GetOwnerReferences() {
			if or.UID == uid {
				return &ds, nil
			}
		}
	}
	return nil, fmt.Errorf("failed to get daemonset with owner reference uid: %s", uid)
}

func logRTEPodsLogs(cli client.Client, k8sCli *kubernetes.Clientset, ctx context.Context, nroObj *nropv1.NUMAResourcesOperator, reason string) {
	dss, err := objects.GetDaemonSetsOwnedBy(cli, nroObj.ObjectMeta)
	if err != nil {
		klog.Infof("no DaemonSets for %s (%s)", nroObj.Name, nroObj.GetUID())
		return
	}

	klog.Infof("%s (%d DaemonSet)", reason, len(dss))

	for _, ds := range dss {
		klog.Infof("daemonset %s/%s desired %d scheduled %d ready %d", ds.Namespace, ds.Name, ds.Status.DesiredNumberScheduled, ds.Status.CurrentNumberScheduled, ds.Status.NumberReady)

		labSel, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
		if err != nil {
			klog.ErrorS(err, "cannot use DaemonSet label selector as selector")
			continue
		}

		var podList corev1.PodList
		err = cli.List(ctx, &podList, &client.ListOptions{
			Namespace:     ds.Namespace,
			LabelSelector: labSel,
		})
		if err != nil {
			klog.ErrorS(err, "cannot get Pods by DaemonSet", "namespace", ds.Namespace, "name", ds.Name)
			continue
		}

		for _, pod := range podList.Items {
			logs, err := objects.GetLogsForPod(k8sCli, pod.Namespace, pod.Name, containerNameRTE)
			if err != nil {
				klog.ErrorS(err, "cannot fetch logs", "dsNamespace", ds.Namespace, "dsName", ds.Name, "podNamespace", pod.Namespace, "podName", pod.Name)
				continue
			}
			klog.Infof("DaemonSet %s/%s -> Pod %s/%s -> logs:\n%s\n-----\n", ds.Namespace, ds.Name, pod.Namespace, pod.Name, logs)
		}
	}
}
