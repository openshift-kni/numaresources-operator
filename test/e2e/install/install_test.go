/*
Copyright 2021 The Kubernetes Authors.

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

package install

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/controllers"
	nropmcp "github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	"github.com/openshift-kni/numaresources-operator/test/utils/crds"
	e2eimages "github.com/openshift-kni/numaresources-operator/test/utils/images"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2epause "github.com/openshift-kni/numaresources-operator/test/utils/objects/pause"
	e2ewait "github.com/openshift-kni/numaresources-operator/test/utils/objects/wait"
)

const (
	containerNameRTE = "resource-topology-exporter"
)

var _ = Describe("[Install] continuousIntegration", func() {
	var initialized bool

	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")
		}
		initialized = true
	})

	Context("with a running cluster with all the components", func() {
		It("[test_id:47574][tier1] should perform overall deployment and verify the condition is reported as available", func() {
			deployedObj := overallDeployment()
			nname := client.ObjectKeyFromObject(deployedObj.nroObj)
			Expect(nname.Name).ToNot(BeEmpty())

			By("checking that the condition Available=true")
			updatedNROObj := &nropv1alpha1.NUMAResourcesOperator{}
			err := wait.PollImmediate(10*time.Second, 5*time.Minute, func() (bool, error) {
				err := e2eclient.Client.Get(context.TODO(), nname, updatedNROObj)
				if err != nil {
					klog.Warningf("failed to get the NRO resource: %v", err)
					return false, err
				}

				cond := status.FindCondition(updatedNROObj.Status.Conditions, status.ConditionAvailable)
				if cond == nil {
					klog.Warningf("missing conditions in %v", updatedNROObj)
					return false, err
				}

				klog.Infof("condition for %s: %v", nname.Name, cond)
				return cond.Status == metav1.ConditionTrue, nil
			})
			if err != nil {
				logRTEPodsLogs(e2eclient.Client, e2eclient.K8sClient, context.TODO(), updatedNROObj, "NRO never reported available")
			}
			Expect(err).NotTo(HaveOccurred(), "NRO never reported available")

			By("checking the NRT CRD is deployed")
			_, err = crds.GetByName(e2eclient.Client, crds.CrdNRTName)
			Expect(err).NotTo(HaveOccurred())

			By("checking the NRO CRD is deployed")
			_, err = crds.GetByName(e2eclient.Client, crds.CrdNROName)
			Expect(err).NotTo(HaveOccurred())

			By("checking Daemonset is up&running")
			Eventually(func() bool {
				ds, err := getDaemonSetByOwnerReference(updatedNROObj.UID)
				if err != nil {
					klog.Warningf("unable to get Daemonset  %v", err)
					return false
				}

				if ds.Status.NumberMisscheduled != 0 {
					klog.Warningf(" Misscheduled: There are %d nodes that should not be running Daemon pod but are", ds.Status.NumberMisscheduled)
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
		})
	})
})

var _ = Describe("[Install] durability", func() {
	var initialized bool

	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")
		}
		initialized = true
	})

	Context("with deploying NUMAResourcesOperator with wrong name", func() {
		It("should do nothing", func() {
			nroObj := objects.TestNRO(objects.EmptyMatchLabels())
			nroObj.Name = "wrong-name"

			err := e2eclient.Client.Create(context.TODO(), nroObj)
			Expect(err).ToNot(HaveOccurred())

			By("checking that the condition Degraded=true")
			Eventually(func() bool {
				updatedNROObj := &nropv1alpha1.NUMAResourcesOperator{}
				err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroObj), updatedNROObj)
				if err != nil {
					klog.Warningf("failed to get the  NUMAResourcesOperator CR: %v", err)
					return false
				}

				cond := status.FindCondition(updatedNROObj.Status.Conditions, status.ConditionDegraded)
				if cond == nil {
					klog.Warningf("missing conditions in %v", updatedNROObj)
					return false
				}

				klog.Infof("condition: %v", cond)

				return cond.Status == metav1.ConditionTrue
			}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "NUMAResourcesOperator condition did not become degraded")

			deleteNROPSync(e2eclient.Client, nroObj)
		})
	})

	Context("with a running cluster with all the components and overall deployment", func() {
		var deployedObj nroDeployment

		BeforeEach(func() {
			deployedObj = overallDeployment()
		})

		AfterEach(func() {
			teardownDeployment(deployedObj, 5*time.Minute)
		})

		It("[test_id:47587][tier1] should restart RTE DaemonSet when image is updated in NUMAResourcesOperator", func() {

			By("getting up-to-date NRO object")
			nroKey := client.ObjectKeyFromObject(deployedObj.nroObj)
			Expect(nroKey.Name).NotTo(BeEmpty())

			nroObj := &nropv1alpha1.NUMAResourcesOperator{}
			err := wait.PollImmediate(10*time.Second, 5*time.Minute, func() (bool, error) {
				err := e2eclient.Client.Get(context.TODO(), nroKey, nroObj)
				if err != nil {
					return false, err
				}
				if len(nroObj.Status.DaemonSets) != 1 {
					return false, fmt.Errorf("unsupported daemonsets (/MCP) count: %d", len(nroObj.Status.DaemonSets))
				}
				return true, nil
			})
			if err != nil {
				logRTEPodsLogs(e2eclient.Client, e2eclient.K8sClient, context.TODO(), nroObj, "NRO not available")
			}
			Expect(err).NotTo(HaveOccurred(), "inconsistent NRO instance:\n%s", objects.ToYAML(nroObj))

			dsKey := e2ewait.ObjectKey{
				Namespace: nroObj.Status.DaemonSets[0].Namespace,
				Name:      nroObj.Status.DaemonSets[0].Name,
			}

			By("waiting for DaemonSet to be ready")
			ds, err := e2ewait.ForDaemonSetReadyByKey(e2eclient.Client, dsKey, 10*time.Second, 3*time.Minute)
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
					klog.Warningf("failed to get the daemonset %s: %v", dsKey.String(), err)
					return false
				}

				if !e2ewait.AreDaemonSetPodsReady(&updatedDs.Status) {
					klog.Warningf("daemonset %s desired %d scheduled %d ready %d",
						dsKey.String(),
						updatedDs.Status.DesiredNumberScheduled,
						updatedDs.Status.CurrentNumberScheduled,
						updatedDs.Status.NumberReady)
					return false
				}

				klog.Infof("daemonset %s ready", dsKey.String())

				klog.Warningf("daemonset Generation observed %v current %v", updatedDs.Status.ObservedGeneration, ds.Generation)
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
			nname := client.ObjectKeyFromObject(deployedObj.nroObj)
			Expect(nname.Name).NotTo(BeEmpty())

			nroObj := &nropv1alpha1.NUMAResourcesOperator{}
			err := e2eclient.Client.Get(context.TODO(), nname, nroObj)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the DaemonSet to be created..")
			uid := nroObj.GetUID()
			ds := &appsv1.DaemonSet{}
			Eventually(func() error {
				var err error
				ds, err = getDaemonSetByOwnerReference(uid)
				return err
			}, 5*time.Minute, 10*time.Second).Should(BeNil())

			deleteNROPSync(e2eclient.Client, nroObj)

			By("checking there are no leftovers")
			// by taking the ns from the ds we're avoiding the need to figure out in advanced
			// at which ns we should look for the resources
			mf, err := rte.GetManifests(configuration.Plat, configuration.PlatVersion, ds.Namespace)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				objs := mf.ToObjects()
				for _, obj := range objs {
					key := client.ObjectKeyFromObject(obj)
					if err := e2eclient.Client.Get(context.TODO(), key, obj); !errors.IsNotFound(err) {
						if err == nil {
							klog.Warningf("obj %s still exists", key.String())
						} else {
							klog.Warningf("obj %s return with error: %v", key.String(), err)
						}
						return false
					}
				}
				return true
			}, 5*time.Minute, 10*time.Second).Should(BeTrue())

			By("redeploy with other parameters")
			nroObjRedep := objects.TestNRO(objects.EmptyMatchLabels())
			nroObjRedep.Spec = *deployedObj.nroObj.Spec.DeepCopy()
			// TODO change to an image which is test dedicated
			nroObjRedep.Spec.ExporterImage = e2eimages.RTETestImageCI

			err = e2eclient.Client.Create(context.TODO(), nroObjRedep)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				updatedNroObj := &nropv1alpha1.NUMAResourcesOperator{}
				err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroObj), updatedNroObj)
				Expect(err).ToNot(HaveOccurred())

				ds, err := getDaemonSetByOwnerReference(updatedNroObj.GetUID())
				if err != nil {
					klog.Warningf("failed to get the RTE DaemonSet: %v", err)
					return false
				}

				return ds.Spec.Template.Spec.Containers[0].Image == e2eimages.RTETestImageCI
			}, 5*time.Minute, 10*time.Second).Should(BeTrue())
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

type nroDeployment struct {
	mcpObj *machineconfigv1.MachineConfigPool
	kcObj  *machineconfigv1.KubeletConfig
	nroObj *nropv1alpha1.NUMAResourcesOperator
}

// overallDeployment returns a struct containing all the deployed objects,
// so it will be easier to introspect and delete them later.
func overallDeployment() nroDeployment {
	var matchLabels map[string]string
	var deployedObj nroDeployment

	if configuration.Plat == platform.Kubernetes {
		mcpObj := objects.TestMCP()
		By(fmt.Sprintf("creating the machine config pool object: %s", mcpObj.Name))
		err := e2eclient.Client.Create(context.TODO(), mcpObj)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		deployedObj.mcpObj = mcpObj
		matchLabels = map[string]string{"test": "test"}
	}

	if configuration.Plat == platform.OpenShift {
		// TODO: should this be configurable?
		matchLabels = objects.OpenshiftMatchLabels()
	}

	nroObj := objects.TestNRO(matchLabels)
	kcObj, err := objects.TestKC(matchLabels)
	ExpectWithOffset(1, err).To(Not(HaveOccurred()))

	unpause, err := e2epause.MachineConfigPoolsByNodeGroups(nroObj.Spec.NodeGroups)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	if _, ok := os.LookupEnv("E2E_NROP_INSTALL_SKIP_KC"); ok {
		By("using cluster kubeletconfig (if any)")
	} else {
		By(fmt.Sprintf("creating the KC object: %s", kcObj.Name))
		err = e2eclient.Client.Create(context.TODO(), kcObj)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		deployedObj.kcObj = kcObj
	}

	By(fmt.Sprintf("creating the NRO object: %s", nroObj.Name))
	err = e2eclient.Client.Create(context.TODO(), nroObj)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	deployedObj.nroObj = nroObj

	Eventually(
		unpause,
		configuration.MachineConfigPoolUpdateTimeout,
		configuration.MachineConfigPoolUpdateInterval,
	).ShouldNot(HaveOccurred())

	err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroObj), nroObj)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	if configuration.Plat == platform.OpenShift {
		Eventually(func() bool {
			updated, err := isMachineConfigPoolsUpdated(nroObj)
			if err != nil {
				klog.Errorf("failed to information about machine config pools: %w", err)
				return false
			}

			return updated
		}, configuration.MachineConfigPoolUpdateTimeout, configuration.MachineConfigPoolUpdateInterval).Should(BeTrue())
	}
	return deployedObj
}

// TODO: what if timeout < period?
func teardownDeployment(nrod nroDeployment, timeout time.Duration) {
	var wg sync.WaitGroup
	if nrod.mcpObj != nil {
		err := e2eclient.Client.Delete(context.TODO(), nrod.mcpObj)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())

		wg.Add(1)
		go func(mcpObj *machineconfigv1.MachineConfigPool) {
			defer GinkgoRecover()
			defer wg.Done()
			klog.Infof("waiting for MCP %q to be gone", mcpObj.Name)
			err := e2ewait.ForMachineConfigPoolDeleted(e2eclient.Client, mcpObj, 10*time.Second, timeout)
			ExpectWithOffset(1, err).ToNot(HaveOccurred(), "MCP %q failed to be deleted", mcpObj.Name)
		}(nrod.mcpObj)
	}

	var err error
	if nrod.kcObj != nil {
		err = e2eclient.Client.Delete(context.TODO(), nrod.kcObj)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
		wg.Add(1)
		go func(kcObj *machineconfigv1.KubeletConfig) {
			defer GinkgoRecover()
			defer wg.Done()
			klog.Infof("waiting for KC %q to be gone", kcObj.Name)
			err := e2ewait.ForKubeletConfigDeleted(e2eclient.Client, kcObj, 10*time.Second, timeout)
			ExpectWithOffset(1, err).ToNot(HaveOccurred(), "KC %q failed to be deleted", kcObj.Name)
		}(nrod.kcObj)
	}

	err = e2eclient.Client.Delete(context.TODO(), nrod.nroObj)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	wg.Add(1)
	go func(nropObj *nropv1alpha1.NUMAResourcesOperator) {
		defer GinkgoRecover()
		defer wg.Done()
		klog.Infof("waiting for NROP %q to be gone", nropObj.Name)
		err := e2ewait.ForNUMAResourcesOperatorDeleted(e2eclient.Client, nropObj, 10*time.Second, timeout)
		ExpectWithOffset(1, err).ToNot(HaveOccurred(), "NROP %q failed to be deleted", nropObj.Name)
	}(nrod.nroObj)

	wg.Wait()

	if configuration.Plat == platform.OpenShift {
		Eventually(func() bool {
			updated, err := isMachineConfigPoolsUpdatedAfterDeletion(nrod.nroObj)
			if err != nil {
				klog.Errorf("failed to retrieve information about machine config pools: %w", err)
				return false
			}
			return updated
		}, configuration.MachineConfigPoolUpdateTimeout, configuration.MachineConfigPoolUpdateInterval).Should(BeTrue())
	}
}

func deleteNROPSync(cli client.Client, nropObj *nropv1alpha1.NUMAResourcesOperator) {
	var err error
	err = cli.Delete(context.TODO(), nropObj)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	err = e2ewait.ForNUMAResourcesOperatorDeleted(cli, nropObj, 10*time.Second, 2*time.Minute)
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "NROP %q failed to be deleted", nropObj.Name)
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

// isMachineConfigPoolsUpdated checks if all related to NUMAResourceOperator CR machines config pools have updated status
func isMachineConfigPoolsUpdated(nro *nropv1alpha1.NUMAResourcesOperator) (bool, error) {
	mcps, err := nropmcp.GetListByNodeGroups(context.TODO(), e2eclient.Client, nro.Spec.NodeGroups)
	if err != nil {
		return false, err
	}

	for _, mcp := range mcps {
		if !controllers.IsMachineConfigPoolUpdated(nro.Name, mcp) {
			return false, nil
		}
	}

	return true, nil
}

// isMachineConfigPoolsUpdatedAfterDeletion checks if all related to NUMAResourceOperator CR machines config pools have updated status
// after MachineConfig deletion
func isMachineConfigPoolsUpdatedAfterDeletion(nro *nropv1alpha1.NUMAResourcesOperator) (bool, error) {
	mcps, err := nropmcp.GetListByNodeGroups(context.TODO(), e2eclient.Client, nro.Spec.NodeGroups)
	if err != nil {
		return false, err
	}

	for _, mcp := range mcps {
		if !controllers.IsMachineConfigPoolUpdatedAfterDeletion(nro.Name, mcp) {
			return false, nil
		}
	}

	return true, nil
}

func logRTEPodsLogs(cli client.Client, k8sCli *kubernetes.Clientset, ctx context.Context, nroObj *nropv1alpha1.NUMAResourcesOperator, reason string) {

	dss, err := objects.GetDaemonSetsOwnedBy(cli, nroObj.ObjectMeta)
	if err != nil {
		klog.Warningf("no DaemonSets for %s (%s)", nroObj.Name, nroObj.GetUID())
		return
	}

	klog.Infof("%s (%d DaemonSet)", reason, len(dss))

	for _, ds := range dss {
		klog.Infof("daemonset %s/%s desired %d scheduled %d ready %d", ds.Namespace, ds.Name, ds.Status.DesiredNumberScheduled, ds.Status.CurrentNumberScheduled, ds.Status.NumberReady)

		labSel, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
		if err != nil {
			klog.Warningf("cannot use DaemonSet label selector as selector: %v", err)
			continue
		}

		var podList corev1.PodList
		err = cli.List(ctx, &podList, &client.ListOptions{
			Namespace:     ds.Namespace,
			LabelSelector: labSel,
		})
		if err != nil {
			klog.Warningf("cannot get Pods by DaemonSet %s/%s: %v", ds.Namespace, ds.Name, err)
			continue
		}

		for _, pod := range podList.Items {
			logs, err := objects.GetLogsForPod(k8sCli, pod.Namespace, pod.Name, containerNameRTE)
			if err != nil {
				klog.Warningf("DaemonSet %s/%s -> Pod %s/%s -> error getting logs: %v", ds.Namespace, ds.Name, pod.Namespace, pod.Name, err)
				continue
			}
			klog.Infof("DaemonSet %s/%s -> Pod %s/%s -> logs:\n%s\n-----\n", ds.Namespace, ds.Name, pod.Namespace, pod.Name, logs)
		}
	}
}
