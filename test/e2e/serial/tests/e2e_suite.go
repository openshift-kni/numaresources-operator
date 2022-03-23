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

package tests

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
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"

	numacellmanifests "github.com/openshift-kni/numaresources-operator/test/deviceplugin/pkg/numacell/manifests"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	"github.com/openshift-kni/numaresources-operator/test/utils/images"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2ewait "github.com/openshift-kni/numaresources-operator/test/utils/objects/wait"
)

const (
	multiNUMALabel    = "numa.hardware.openshift-kni.io/cell-count"
	nropTestCIImage   = "quay.io/openshift-kni/resource-topology-exporter:test-ci"
	schedulerTestName = "test-topology-scheduler"
)

var (
	nroOperObj    *nropv1alpha1.NUMAResourcesOperator
	nroSchedObj   *nropv1alpha1.NUMAResourcesScheduler
	schedulerName string
)

// This suite holds the e2e tests which span across components,
// e.g. involve both the behaviour of RTE and the scheduler.
// These tests are almost always disruptive, meaning they significantly
// alter the cluster state and need a very specific cluster state (which
// is each test responsability to setup and cleanup).
// Hence we call this suite serial, implying each test should run alone
// and indisturbed on the cluster. No concurrency at all is possible,
// each test "owns" the cluster - but again, must leave no leftovers.

// do not use these outside this *file*
var __fxt *e2efixture.Fixture
var __nrtList nrtv1alpha1.NodeResourceTopologyList

func BeforeSuiteHelper() {
	var err error

	__fxt, err = e2efixture.Setup("e2e-test-infra")
	Expect(err).ToNot(HaveOccurred(), "unable to setup infra test fixture")

	err = __fxt.Client.List(context.TODO(), &__nrtList)
	Expect(err).ToNot(HaveOccurred())

	nroSchedObj = &nropv1alpha1.NUMAResourcesScheduler{}
	err = __fxt.Client.Get(context.TODO(), client.ObjectKey{Name: nrosched.NROSchedObjectName}, nroSchedObj)
	Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nrosched.NROSchedObjectName)

	nroOperObj = &nropv1alpha1.NUMAResourcesOperator{}
	err = __fxt.Client.Get(context.TODO(), client.ObjectKey{Name: objects.NROName()}, nroOperObj)
	Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", objects.NROName())

	Expect(nroOperObj.Spec.NodeGroups).ToNot(BeEmpty(), "cannot autodetect the TAS node groups from the cluster")

	schedulerName = nroSchedObj.Status.SchedulerName
	Expect(schedulerName).ToNot(BeEmpty(), "cannot autodetect the TAS scheduler name from the cluster")
	klog.Infof("scheduler name: %q", schedulerName)

	setupInfra(__fxt, nroOperObj.Spec.NodeGroups, 3*time.Minute)

	labelNodes(__fxt.Client, __nrtList)
}

func AfterSuiteHelper() {
	if _, ok := os.LookupEnv("E2E_INFRA_NO_TEARDOWN"); ok {
		return
	}

	unlabelNodes(__fxt.Client, __nrtList)

	// numacell daemonset automatically cleaned up when we remove the namespace
	err := e2efixture.Teardown(__fxt)
	Expect(err).NotTo(HaveOccurred())
}

func setupInfra(fxt *e2efixture.Fixture, nodeGroups []nropv1alpha1.NodeGroup, timeout time.Duration) {
	klog.Infof("e2e infra setup begin")

	mcps, err := machineconfigpools.GetNodeGroupsMCPs(context.TODO(), fxt.Client, nodeGroups)
	Expect(err).ToNot(HaveOccurred())

	klog.Infof("setting e2e infra for %d MCPs", len(mcps))

	sa := numacellmanifests.ServiceAccount(fxt.Namespace.Name, numacellmanifests.Prefix)
	err = fxt.Client.Create(context.TODO(), sa)
	Expect(err).ToNot(HaveOccurred(), "cannot create the numacell serviceaccount %q in the namespace %q", sa.Name, sa.Namespace)

	ro := numacellmanifests.Role(fxt.Namespace.Name, numacellmanifests.Prefix)
	err = fxt.Client.Create(context.TODO(), ro)
	Expect(err).ToNot(HaveOccurred(), "cannot create the numacell role %q in the namespace %q", sa.Name, sa.Namespace)

	rb := numacellmanifests.RoleBinding(fxt.Namespace.Name, numacellmanifests.Prefix)
	err = fxt.Client.Create(context.TODO(), rb)
	Expect(err).ToNot(HaveOccurred(), "cannot create the numacell rolebinding %q in the namespace %q", sa.Name, sa.Namespace)

	var dss []*appsv1.DaemonSet
	for _, mcp := range mcps {
		if mcp.Spec.NodeSelector == nil {
			klog.Warningf("the machine config pool %q does not have node selector", mcp.Name)
			continue
		}

		dsName := objectnames.GetComponentName(numacellmanifests.Prefix, mcp.Name)
		klog.Infof("setting e2e infra for %q: daemonset %q", mcp.Name, dsName)

		pullSpec := getNUMACellDevicePluginPullSpec()
		ds := numacellmanifests.DaemonSet(mcp.Spec.NodeSelector.MatchLabels, fxt.Namespace.Name, dsName, sa.Name, pullSpec)
		err = fxt.Client.Create(context.TODO(), ds)
		Expect(err).ToNot(HaveOccurred(), "cannot create the numacell daemonset %q in the namespace %q", ds.Name, ds.Namespace)

		dss = append(dss, ds)
	}

	klog.Infof("daemonsets created (%d)", len(dss))

	var wg sync.WaitGroup
	for _, ds := range dss {
		wg.Add(1)
		go func(ds *appsv1.DaemonSet) {
			defer GinkgoRecover()
			defer wg.Done()

			klog.Infof("waiting for daemonset %q to be ready", ds.Name)

			// TODO: what if timeout < period?
			ds, err := e2ewait.ForDaemonSetReady(fxt.Client, ds, 10*time.Second, timeout)
			Expect(err).ToNot(HaveOccurred(), "DaemonSet %q failed to go running", ds.Name)
		}(ds)
	}
	wg.Wait()

	klog.Infof("e2e infra setup completed")
}

func getNUMACellDevicePluginPullSpec() string {
	if pullSpec, ok := os.LookupEnv("E2E_NUMACELL_DEVICE_PLUGIN_URL"); ok {
		return pullSpec
	}
	return images.NUMACellDevicePluginTestImageCI
}

func labelNodes(cli client.Client, nrtList nrtv1alpha1.NodeResourceTopologyList) {
	for _, nrt := range nrtList.Items {
		node := corev1.Node{}
		err := cli.Get(context.TODO(), client.ObjectKey{Name: nrt.Name}, &node)
		Expect(err).ToNot(HaveOccurred())
		labelValue := fmt.Sprintf("%d", len(nrt.Zones))
		node.Labels[multiNUMALabel] = labelValue

		klog.Infof("labeling node %q with %s: %s", nrt.Name, multiNUMALabel, labelValue)
		err = cli.Update(context.TODO(), &node)
		Expect(err).ToNot(HaveOccurred())
	}
}

func unlabelNodes(cli client.Client, nrtList nrtv1alpha1.NodeResourceTopologyList) {
	for _, nrt := range nrtList.Items {
		node := corev1.Node{}
		err := cli.Get(context.TODO(), client.ObjectKey{Name: nrt.Name}, &node)
		Expect(err).ToNot(HaveOccurred())

		klog.Infof("unlabeling node %q removing label %s", nrt.Name, multiNUMALabel)
		delete(node.Labels, multiNUMALabel)
		err = cli.Update(context.TODO(), &node)
		Expect(err).ToNot(HaveOccurred())
	}
}
