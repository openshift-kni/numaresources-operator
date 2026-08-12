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
	numazoneapi "github.com/openshift-kni/numaresources-operator/pkg/numazone/api"
	numazonemanifests "github.com/openshift-kni/numaresources-operator/pkg/numazone/manifests"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/images"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func SetupInfra(fxt *e2efixture.Fixture, nroOperObj *nropv1.NUMAResourcesOperator, nrtList nrtv1alpha2.NodeResourceTopologyList) {
	setupNUMAZone(fxt, nroOperObj, nrtList, 3*time.Minute)
	LabelNodes(fxt.Client, nrtList)
}

func TeardownInfra(fxt *e2efixture.Fixture, nrtList nrtv1alpha2.NodeResourceTopologyList) {
	UnlabelNodes(fxt.Client, nrtList)
}

func setupNUMAZone(fxt *e2efixture.Fixture, nroOperObj *nropv1.NUMAResourcesOperator, nrtList nrtv1alpha2.NodeResourceTopologyList, timeout time.Duration) {
	klog.InfoS("e2e infra setup begin")

	Expect(nroOperObj).ToNot(BeNil(), "NUMAResourcesOperator object is required for e2e infra setup")
	Expect(nroOperObj.Spec.NodeGroups).ToNot(BeEmpty(), "cannot autodetect the TAS node groups from the cluster")

	poolNames, err := nodegroups.GetPoolNamesFrom(context.TODO(), fxt.Client, nroOperObj.Spec.NodeGroups)
	Expect(err).ToNot(HaveOccurred())
	klog.InfoS("setting e2e infra for pools", "poolCount", len(poolNames))

	sa := numazonemanifests.ServiceAccount(fxt.Namespace.Name, numazonemanifests.Prefix)
	err = fxt.Client.Create(context.TODO(), sa)
	Expect(err).ToNot(HaveOccurred(), "cannot create the NUMA-aware device plugin serviceaccount %q in the namespace %q", sa.Name, sa.Namespace)

	ro := numazonemanifests.Role(fxt.Namespace.Name, numazonemanifests.Prefix)
	err = fxt.Client.Create(context.TODO(), ro)
	Expect(err).ToNot(HaveOccurred(), "cannot create the NUMA-aware device plugin role %q in the namespace %q", sa.Name, sa.Namespace)

	rb := numazonemanifests.RoleBinding(fxt.Namespace.Name, numazonemanifests.Prefix)
	err = fxt.Client.Create(context.TODO(), rb)
	Expect(err).ToNot(HaveOccurred(), "cannot create the NUMA-aware device plugin rolebinding %q in the namespace %q", sa.Name, sa.Namespace)

	pullSpec := GetNUMAAwareDevicePluginPullSpec(context.TODO(), fxt.Client, nroOperObj)

	var dss []*appsv1.DaemonSet
	for _, poolName := range poolNames {
		dsName := objectnames.GetComponentName(numazonemanifests.Prefix, poolName)
		klog.InfoS("setting e2e infra for pool", "poolName", poolName, "daemonsetName", dsName)

		labels, err := nodegroups.NodeSelectorFromPoolName(context.TODO(), fxt.Client, poolName)
		Expect(err).ToNot(HaveOccurred())
		ds := numazonemanifests.DaemonSet(labels, fxt.Namespace.Name, dsName, sa.Name, pullSpec)
		err = fxt.Client.Create(context.TODO(), ds)
		Expect(err).ToNot(HaveOccurred(), "cannot create the NUMA-aware device plugin daemonset %q in the namespace %q", ds.Name, ds.Namespace)

		dss = append(dss, ds)
	}

	klog.InfoS("daemonsets created", "count", len(dss))

	waitAllDSReady(fxt, dss, timeout)
	klog.InfoS("daemonsets ready", "count", len(dss))

	waitResourcesAvailable(fxt, nrtList, timeout)
	klog.InfoS("resources available", "count", len(nrtList.Items))

	klog.InfoS("e2e infra setup completed")
}

func waitAllDSReady(fxt *e2efixture.Fixture, dss []*appsv1.DaemonSet, timeout time.Duration) {
	var wg sync.WaitGroup
	for _, ds := range dss {
		wg.Add(1)
		go func(ds *appsv1.DaemonSet) {
			defer GinkgoRecover()
			defer wg.Done()

			klog.InfoS("waiting for daemonset to be ready", "daemonsetName", ds.Name)

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

			klog.InfoS("waiting for numazone resources to be reported on NRT", "nrtName", nrtName)

			_, err := wait.With(fxt.Client).Interval(11*time.Second).Timeout(timeout).ForNodeResourceTopologyToHave(context.TODO(), nrtName, func(resInfo nrtv1alpha2.ResourceInfo) bool {
				// TODO: check available qty > 0?
				return numazoneapi.IsResourceName(resInfo.Name)
			})
			Expect(err).ToNot(HaveOccurred(), "NRT %q failed to expose numazone resources", nrtName)
		}(nrt.Name)
	}
	wg.Wait()
}

func GetNUMAAwareDevicePluginPullSpec(ctx context.Context, cli client.Client, nroOperObj *nropv1.NUMAResourcesOperator) string {
	pullSpec := getNUMAAwareDevicePluginPullSpec(ctx, cli, nroOperObj)
	klog.InfoS("using NUMA-aware device plugin", "image", pullSpec)
	return pullSpec
}

func getNUMAAwareDevicePluginPullSpec(ctx context.Context, cli client.Client, nroOperObj *nropv1.NUMAResourcesOperator) string {
	if pullSpec, ok := os.LookupEnv("E2E_NROP_URL_NUMAZONE_DEVICE_PLUGIN"); ok {
		return pullSpec
	}
	if pullSpec, ok := os.LookupEnv("E2E_NUMAZONE_DEVICE_PLUGIN_URL"); ok {
		return pullSpec
	}
	// backward compatibility with pre-rename env names
	if pullSpec, ok := os.LookupEnv("E2E_NROP_URL_NUMACELL_DEVICE_PLUGIN"); ok {
		return pullSpec
	}
	if pullSpec, ok := os.LookupEnv("E2E_NUMACELL_DEVICE_PLUGIN_URL"); ok {
		return pullSpec
	}

	// Prefer the operator-managed RTE image. In productization, numazone is
	// shipped in the same multi-entrypoint operator image as RTE.
	if pullSpec, err := devicePluginImageFromRTEDaemonSet(ctx, cli, nroOperObj); err == nil {
		return pullSpec
	} else {
		klog.InfoS("unable to discover device plugin image from RTE DaemonSet, using fallback", "error", err)
	}
	return images.NUMAAwareDevicePluginTestImageCI
}

func devicePluginImageFromRTEDaemonSet(ctx context.Context, cli client.Client, nroOperObj *nropv1.NUMAResourcesOperator) (string, error) {
	if nroOperObj == nil {
		return "", fmt.Errorf("NUMAResourcesOperator object is nil")
	}
	if len(nroOperObj.Status.DaemonSets) == 0 {
		return "", fmt.Errorf("NUMAResourcesOperator %q has no RTE DaemonSets in status", nroOperObj.Name)
	}

	dss, err := objects.GetDaemonSetsByNamespacedName(cli, ctx, nroOperObj.Status.DaemonSets...)
	if err != nil {
		return "", err
	}
	if len(dss) == 0 {
		return "", fmt.Errorf("no RTE DaemonSets found for NUMAResourcesOperator %q", nroOperObj.Name)
	}
	if len(dss[0].Spec.Template.Spec.Containers) == 0 {
		return "", fmt.Errorf("RTE DaemonSet %s/%s has no containers", dss[0].Namespace, dss[0].Name)
	}
	image := dss[0].Spec.Template.Spec.Containers[0].Image
	if image == "" {
		return "", fmt.Errorf("RTE DaemonSet %s/%s has empty container image", dss[0].Namespace, dss[0].Name)
	}
	return image, nil
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

		klog.InfoS("adding labels", "nodeName", nodeName, "label", MultiNUMALabel, "value", labelValue)
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

		klog.InfoS("removing labels", "nodeName", nodeName, "label", MultiNUMALabel)
		delete(node.Labels, MultiNUMALabel)
		err = cli.Update(context.TODO(), &node)
		g.Expect(err).ToNot(HaveOccurred())
	}).WithTimeout(3*time.Minute).WithPolling(30*time.Second).Should(Succeed(), "failed to unlabel node %q: %v", nodeName, err)
}
