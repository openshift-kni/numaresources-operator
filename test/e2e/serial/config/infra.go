/*
 * Copyright 2022 Red Hat, Inc.
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

package config

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/nodegroups"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	numacellapi "github.com/openshift-kni/numaresources-operator/test/deviceplugin/pkg/numacell/api"
	numacellmanifests "github.com/openshift-kni/numaresources-operator/test/deviceplugin/pkg/numacell/manifests"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/images"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func SetupInfra(fxt *e2efixture.Fixture, nroOperObj *nropv1.NUMAResourcesOperator, nrtList nrtv1alpha2.NodeResourceTopologyList) {
	setupNUMACell(fxt, nroOperObj.Spec.NodeGroups, nrtList, 3*time.Minute)
	LabelNodes(fxt.Client, nrtList)
}

func TeardownInfra(fxt *e2efixture.Fixture, nrtList nrtv1alpha2.NodeResourceTopologyList) {
	UnlabelNodes(fxt.Client, nrtList)
}

func setupNUMACell(fxt *e2efixture.Fixture, nodeGroups []nropv1.NodeGroup, nrtList nrtv1alpha2.NodeResourceTopologyList, timeout time.Duration) {
	klog.Infof("e2e infra setup begin")

	Expect(nodeGroups).ToNot(BeEmpty(), "cannot autodetect the TAS node groups from the cluster")

	poolNames, err := nodegroups.GetPoolNamesFrom(context.TODO(), fxt.Client, nodeGroups)
	Expect(err).ToNot(HaveOccurred())
	klog.Infof("setting e2e infra for %d pools", len(poolNames))

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
	for _, poolName := range poolNames {
		dsName := objectnames.GetComponentName(numacellmanifests.Prefix, poolName)
		klog.Infof("setting e2e infra for %q: daemonset %q", poolName, dsName)

		pullSpec := GetNUMACellDevicePluginPullSpec()
		labels, err := nodegroups.NodeSelectorFromPoolName(context.TODO(), fxt.Client, poolName)
		Expect(err).ToNot(HaveOccurred())
		ds := numacellmanifests.DaemonSet(labels, fxt.Namespace.Name, dsName, sa.Name, pullSpec)
		err = fxt.Client.Create(context.TODO(), ds)
		Expect(err).ToNot(HaveOccurred(), "cannot create the numacell daemonset %q in the namespace %q", ds.Name, ds.Namespace)

		dss = append(dss, ds)
	}

	klog.Infof("daemonsets created (%d)", len(dss))

	waitAllDSReady(fxt, dss, timeout)
	klog.Infof("daemonsets ready (%d)", len(dss))

	waitResourcesAvailable(fxt, nrtList, timeout)
	klog.Infof("resources available (%d)", len(nrtList.Items))

	klog.Infof("e2e infra setup completed")
}

func waitAllDSReady(fxt *e2efixture.Fixture, dss []*appsv1.DaemonSet, timeout time.Duration) {
	var wg sync.WaitGroup
	for _, ds := range dss {
		wg.Add(1)
		go func(ds *appsv1.DaemonSet) {
			defer GinkgoRecover()
			defer wg.Done()

			klog.Infof("waiting for daemonset %q to be ready", ds.Name)

			// TODO: what if timeout < period?
			ds, err := wait.With(fxt.Client).Interval(10*time.Second).Timeout(timeout).ForDaemonSetReady(context.TODO(), ds)
			Expect(err).ToNot(HaveOccurred(), "DaemonSet %q failed to go running", ds.Name)
		}(ds)
	}
	wg.Wait()
}

func waitResourcesAvailable(fxt *e2efixture.Fixture, nrtList nrtv1alpha2.NodeResourceTopologyList, timeout time.Duration) {
	var wg sync.WaitGroup
	for _, nrt := range nrtList.Items {
		wg.Add(1)
		go func(nrtName string) {
			defer GinkgoRecover()
			defer wg.Done()

			klog.Infof("waiting for numacell resources to be reported on NRT %q", nrtName)

			_, err := wait.With(fxt.Client).Interval(11*time.Second).Timeout(timeout).ForNodeResourceTopologyToHave(context.TODO(), nrtName, func(resInfo nrtv1alpha2.ResourceInfo) bool {
				// TODO: check available qty > 0?
				return numacellapi.IsResourceName(resInfo.Name)
			})
			Expect(err).ToNot(HaveOccurred(), "NRT %q failed to expose numacell resources", nrtName)
		}(nrt.Name)
	}
	wg.Wait()
}

func GetNUMACellDevicePluginPullSpec() string {
	pullSpec := getNUMACellDevicePluginPullSpec()
	klog.Infof("using NUMACell image: %q", pullSpec)
	return pullSpec
}

func getNUMACellDevicePluginPullSpec() string {
	if pullSpec, ok := os.LookupEnv("E2E_NROP_URL_NUMACELL_DEVICE_PLUGIN"); ok {
		return pullSpec
	}
	// backward compatibility
	if pullSpec, ok := os.LookupEnv("E2E_NUMACELL_DEVICE_PLUGIN_URL"); ok {
		return pullSpec
	}
	return images.NUMACellDevicePluginTestImageCI
}

func LabelNodes(cli client.Client, nrtList nrtv1alpha2.NodeResourceTopologyList) {
	var wg sync.WaitGroup
	for idx := range nrtList.Items {
		nrt := &nrtList.Items[idx]

		wg.Add(1)
		go func(nodeName string) {
			defer wg.Done()
			labelNodeByName(cli, nodeName, fmt.Sprintf("%d", len(nrt.Zones)))
		}(nrt.Name)
	}
	wg.Wait()
}

func UnlabelNodes(cli client.Client, nrtList nrtv1alpha2.NodeResourceTopologyList) {
	var wg sync.WaitGroup
	for _, nrt := range nrtList.Items {
		wg.Add(1)
		go func(nodeName string) {
			defer wg.Done()
			unlabelNodeByName(cli, nodeName)
		}(nrt.Name)
	}
	wg.Wait()
}

func labelNodeByName(cli client.Client, nodeName, labelValue string) {
	var err error
	// see https://pkg.go.dev/github.com/onsi/gomega#Eventually category 3
	Eventually(func(g Gomega) {
		node := corev1.Node{}
		err := cli.Get(context.TODO(), client.ObjectKey{Name: nodeName}, &node)
		g.Expect(err).ToNot(HaveOccurred())
		node.Labels[MultiNUMALabel] = labelValue

		klog.Infof("labeling node %q with %s: %s", nodeName, MultiNUMALabel, labelValue)
		// TODO: this should be retried
		err = cli.Update(context.TODO(), &node)
		g.Expect(err).ToNot(HaveOccurred())
	}).WithTimeout(3*time.Minute).WithPolling(30*time.Second).Should(Succeed(), "failed to label node %q: %v", nodeName, err)
}

func unlabelNodeByName(cli client.Client, nodeName string) {
	var err error
	// see https://pkg.go.dev/github.com/onsi/gomega#Eventually category 3
	Eventually(func(g Gomega) {
		node := corev1.Node{}
		err = cli.Get(context.TODO(), client.ObjectKey{Name: nodeName}, &node)
		g.Expect(err).ToNot(HaveOccurred())

		klog.Infof("unlabeling node %q removing label %s", nodeName, MultiNUMALabel)
		delete(node.Labels, MultiNUMALabel)
		err = cli.Update(context.TODO(), &node)
		g.Expect(err).ToNot(HaveOccurred())
	}).WithTimeout(3*time.Minute).WithPolling(30*time.Second).Should(Succeed(), "failed to unlabel node %q: %v", nodeName, err)
}
