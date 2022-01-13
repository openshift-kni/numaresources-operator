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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/apps/v1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	"github.com/openshift-kni/numaresources-operator/test/utils/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

const (
	crdName      = "noderesourcetopologies.topology.node.k8s.io"
	newTestImage = "quay.io/openshift-kni/numaresources-operator:v0.1.0"
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
		It("[test_id:47574] should perform overall deployment and verify the condition is reported as available", func() {
			deployedObj := overallDeployment()
			var nname client.ObjectKey
			for _, obj := range deployedObj {
				if nroObj, ok := obj.(*nropv1alpha1.NUMAResourcesOperator); ok {
					nname = client.ObjectKeyFromObject(nroObj)
				}
			}
			Expect(nname.Name).ToNot(BeEmpty())

			By("checking that the condition Available=true")
			Eventually(func() bool {
				updatedNROObj := &nropv1alpha1.NUMAResourcesOperator{}
				err := e2eclient.Client.Get(context.TODO(), nname, updatedNROObj)
				if err != nil {
					klog.Warningf("failed to get the RTE resource: %v", err)
					return false
				}

				cond := status.FindCondition(updatedNROObj.Status.Conditions, status.ConditionAvailable)
				if cond == nil {
					klog.Warningf("missing conditions in %v", updatedNROObj)
					return false
				}

				klog.Infof("condition: %v", cond)

				return cond.Status == metav1.ConditionTrue
			}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "RTE condition did not become available")

			By("checking the NRT CRD is deployed")
			crd := &apiextensionv1.CustomResourceDefinition{}
			key := client.ObjectKey{
				Name: crdName,
			}
			err := e2eclient.Client.Get(context.TODO(), key, crd)
			Expect(err).NotTo(HaveOccurred())
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
			nroObj := objects.TestNRO(nil)
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

			err = e2eclient.Client.Delete(context.TODO(), nroObj)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("with a running cluster with all the components and overall deployment", func() {
		var deployedObj []client.Object

		BeforeEach(func() {
			deployedObj = overallDeployment()
		})

		AfterEach(func() {
			for _, obj := range deployedObj {
				err := e2eclient.Client.Delete(context.TODO(), obj)
				if err != nil {
					Expect(errors.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("unexpected error: %v", err))
				}
			}
		})

		It("should be able to delete NUMAResourceOperator CR and redeploy without polluting cluster state", func() {
			var nname client.ObjectKey
			for _, obj := range deployedObj {
				if nroObj, ok := obj.(*nropv1alpha1.NUMAResourcesOperator); ok {
					nname = client.ObjectKeyFromObject(nroObj)
				}
			}
			Expect(nname.Name).ToNot(BeEmpty())

			nroObj := &nropv1alpha1.NUMAResourcesOperator{}
			err := e2eclient.Client.Get(context.TODO(), nname, nroObj)
			Expect(err).ToNot(HaveOccurred())

			uid := nroObj.GetUID()
			ds, err := getDaemonSetByOwnerReference(uid)
			Expect(err).ToNot(HaveOccurred())

			err = e2eclient.Client.Delete(context.TODO(), nroObj)
			Expect(err).ToNot(HaveOccurred())

			By("checking there are no leftovers")
			// by taking the ns from the ds we're avoiding the need to figure out in advanced
			// at which ns we should look for the resources
			mf, err := rte.GetManifests(configuration.Platform, ds.Namespace)
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
			// TODO change to an image which is test dedicated
			nroObj.Spec.ExporterImage = newTestImage
			// resourceVersion should not be set on objects to be created
			nroObj.ResourceVersion = ""

			err = e2eclient.Client.Create(context.TODO(), nroObj)
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

				return ds.Spec.Template.Spec.Containers[0].Image == newTestImage
			}, 5*time.Minute, 10*time.Second).Should(BeTrue())
		})
	})
})

// overallDeployment returns a slice of an objects created by it,
// so it will be easier to introspect and delete them later.
func overallDeployment() []client.Object {
	var matchLabels map[string]string
	var deployedObj []client.Object

	if configuration.Platform == platform.Kubernetes {
		mcpObj := objects.TestMCP()
		By(fmt.Sprintf("creating the machine config pool object: %s", mcpObj.Name))
		err := e2eclient.Client.Create(context.TODO(), mcpObj)
		Expect(err).NotTo(HaveOccurred())
		deployedObj = append(deployedObj, mcpObj)
		matchLabels = map[string]string{"test": "test"}
	}

	if configuration.Platform == platform.OpenShift {
		// TODO: should this be configurable?
		matchLabels = map[string]string{"pools.operator.machineconfiguration.openshift.io/worker": ""}
	}

	nroObj := objects.TestNRO(matchLabels)
	kcObj, err := objects.TestKC(matchLabels)
	Expect(err).To(Not(HaveOccurred()))

	unpause, err := machineconfigpools.PauseMCPs(nroObj.Spec.NodeGroups)
	Expect(err).NotTo(HaveOccurred())

	By(fmt.Sprintf("creating the KC object: %s", kcObj.Name))
	err = e2eclient.Client.Create(context.TODO(), kcObj)
	Expect(err).NotTo(HaveOccurred())
	deployedObj = append(deployedObj, kcObj)

	By(fmt.Sprintf("creating the NRO object: %s", nroObj.Name))
	err = e2eclient.Client.Create(context.TODO(), nroObj)
	Expect(err).NotTo(HaveOccurred())
	deployedObj = append(deployedObj, nroObj)

	err = unpause()
	Expect(err).NotTo(HaveOccurred())

	err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroObj), nroObj)
	Expect(err).NotTo(HaveOccurred())

	if configuration.Platform == platform.OpenShift {
		Eventually(func() bool {
			updated, err := machineconfigpools.IsMachineConfigPoolsUpdated(nroObj)
			if err != nil {
				klog.Errorf("failed to information about machine config pools: %w", err)
				return false
			}

			return updated
		}, configuration.MachineConfigPoolUpdateTimeout, configuration.MachineConfigPoolUpdateInterval).Should(BeTrue())
	}
	return deployedObj
}

func getDaemonSetByOwnerReference(uid types.UID) (*v1.DaemonSet, error) {
	dsList := &v1.DaemonSetList{}

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
