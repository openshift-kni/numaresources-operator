/*
Copyright 2020 The Kubernetes Authors.

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

/*
 * resource-topology-exporter specific tests
 */

package rte

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/k8stopologyawareschedwg/numaplacement"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	topologyclientset "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/generated/clientset/versioned"
	"github.com/k8stopologyawareschedwg/podfingerprint"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/k8sannotations"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/nrtupdater"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/fixture"
	e2enodes "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/nodes"
	e2enodetopology "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/nodetopology"
	e2epods "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/pods"
	e2ertepod "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/pods/rtepod"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/remoteexec"
	e2econsts "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/testconsts"
	e2etestenv "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/testenv"
)

const (
	updateIntervalExtraSafety = 10 * time.Second
)

var _ = ginkgo.Describe("[RTE][InfraConsuming] Resource topology exporter", func() {
	var (
		initialized         bool
		topologyUpdaterNode *corev1.Node
		workerNodes         []corev1.Node
	)

	f := fixture.New()

	ginkgo.BeforeEach(func() {
		var err error

		nsCleanup, err := f.CreateNamespace("rte")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		ginkgo.DeferCleanup(nsCleanup)
		if !initialized {
			workerNodes, err = e2enodes.GetWorkerNodes(f.K8SCli)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(workerNodes).ToNot(gomega.BeEmpty())

			// pick any worker node. The (implicit, TODO: make explicit) assumption is
			// the daemonset runs on CI on all the worker nodes.
			var hasLabel bool
			topologyUpdaterNode, hasLabel = e2enodes.PickTargetNode(workerNodes)
			gomega.Expect(topologyUpdaterNode).ToNot(gomega.BeNil())
			if !hasLabel {
				// during the e2e tests we expect changes on the node topology.
				// but in an environment with multiple worker nodes, we might be looking at the wrong node.
				// thus, we assign a unique label to the picked worker node
				// and making sure to deploy the pod on it during the test using nodeSelector
				err = e2enodes.LabelNode(f.K8SCli, topologyUpdaterNode, map[string]string{e2econsts.TestNodeLabel: ""})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			initialized = true
		}
	})

	ginkgo.Context("with cluster configured", func() {
		ginkgo.It("[NotificationFile] it should react to pod changes using the smart poller with notification file", func() {
			initialNodeTopo := e2enodetopology.GetNodeTopology(f.TopoCli, topologyUpdaterNode.Name)

			updateInterval, method, err := estimateUpdateInterval(*initialNodeTopo)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			baselineTimeout := 5*updateInterval + updateIntervalExtraSafety
			klog.Infof("%s update interval: %s (timeout %s)", method, updateInterval, baselineTimeout)

			ginkgo.By("waiting for a periodic update to find the optimal update window")
			var baselineNodeTopo *v1alpha2.NodeResourceTopology
			gomega.Eventually(func(g gomega.Gomega) {
				var err error
				baselineNodeTopo, err = f.TopoCli.TopologyV1alpha2().NodeResourceTopologies().Get(context.TODO(), topologyUpdaterNode.Name, metav1.GetOptions{})
				g.Expect(err).ToNot(gomega.HaveOccurred(), "failed to get the node topology resource")
				g.Expect(baselineNodeTopo.ObjectMeta.ResourceVersion).ToNot(gomega.Equal(initialNodeTopo.ObjectMeta.ResourceVersion), "resource %s not yet updated - resource version not bumped", topologyUpdaterNode.Name)
				klog.Infof("resource %s baseline update (resource version %v -> %v)", topologyUpdaterNode.Name, initialNodeTopo.ObjectMeta.ResourceVersion, baselineNodeTopo.ObjectMeta.ResourceVersion)
			}).WithTimeout(baselineTimeout).WithPolling(1*time.Second).Should(gomega.Succeed(), "didn't get baseline periodic update")

			ginkgo.By("triggering notification using the file")
			rtePod, err := e2epods.GetPodOnNode(f.K8SCli, topologyUpdaterNode.Name, e2etestenv.GetNamespaceName(), e2etestenv.RTELabelName)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			rteContainerName, err := e2ertepod.FindRTEContainerName(rtePod)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			rteNotifyFilePath, err := e2ertepod.FindNotificationFilePath(f.Ctx, f.K8SCli, rtePod)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			cmd := []string{"/bin/touch", rteNotifyFilePath}
			_, _, err = remoteexec.CommandOnPod(f.Ctx, f.K8SCli, rtePod, rteContainerName, cmd...)
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), "failed exec command %v on pod %q", cmd, client.ObjectKeyFromObject(rtePod).String())
			klog.Infof("notification triggered")

			ginkgo.By("waiting for reactive topology update")
			var finalNodeTopo *v1alpha2.NodeResourceTopology
			gomega.Eventually(func(g gomega.Gomega) {
				finalNodeTopo, err = f.TopoCli.TopologyV1alpha2().NodeResourceTopologies().Get(context.TODO(), topologyUpdaterNode.Name, metav1.GetOptions{})
				g.Expect(err).ToNot(gomega.HaveOccurred(), "failed to get the node topology resource")
				g.Expect(finalNodeTopo.ObjectMeta.ResourceVersion).ToNot(gomega.Equal(baselineNodeTopo.ObjectMeta.ResourceVersion), "resource %s not yet updated - resource version not bumped", topologyUpdaterNode.Name)

				klog.Infof("resource %s updated! - resource version bumped (old %v new %v)", topologyUpdaterNode.Name, baselineNodeTopo.ObjectMeta.ResourceVersion, finalNodeTopo.ObjectMeta.ResourceVersion)

				reason, ok := finalNodeTopo.Annotations[k8sannotations.RTEUpdate]
				g.Expect(ok).To(gomega.BeTrue(), "resource %s missing annotation!", topologyUpdaterNode.Name)
				g.Expect(reason).To(gomega.Equal(nrtupdater.RTEUpdateReactive), "resource %s reason %v expected %v", topologyUpdaterNode.Name, reason, nrtupdater.RTEUpdateReactive)
			}).WithTimeout(baselineTimeout).WithPolling(1*time.Second).Should(gomega.Succeed(), "didn't get updated node topology info")

			ginkgo.By("checking the topology was updated for the right reason")
			gomega.Expect(finalNodeTopo.Annotations).ToNot(gomega.BeNil(), "missing annotations entirely")
			reason := finalNodeTopo.Annotations[k8sannotations.RTEUpdate]
			gomega.Expect(reason).To(gomega.Equal(nrtupdater.RTEUpdateReactive), "update reason error: expected %q got %q", nrtupdater.RTEUpdateReactive, reason)
		})
	})

	ginkgo.Context("with pod fingerprinting enabled", func() {
		ginkgo.It("[PodFingerprint] it should report the computation method in the attributes", func() {
			nrt := e2enodetopology.GetNodeTopology(f.TopoCli, topologyUpdaterNode.Name)
			klog.Infof("Initial NRT: %q generation=%v resourceVersion=%v", nrt.Name, nrt.Generation, nrt.ResourceVersion)

			if _, ok := findAttribute(nrt.Attributes, podfingerprint.Attribute); !ok {
				ginkgo.Skip("pod fingerprinting attribute not found - assuming disabled")
			}
			meth, ok := findAttribute(nrt.Attributes, podfingerprint.AttributeMethod)
			gomega.Expect(ok).To(gomega.BeTrue(), "attribute %q missing, but PFP reported", podfingerprint.AttributeMethod)
			// note this is a subset of all the available methods declared in the podfingerprint package
			validMethods := []string{
				podfingerprint.MethodAll,
				podfingerprint.MethodWithExclusiveResources,
			}
			gomega.Expect(validMethods).Should(gomega.ContainElement(meth), "unsupported PFP computation method %q", meth)
		})

		ginkgo.It("[PodFingerprint] it should report stable value if the pods do not change", func() {
			prevNrt := e2enodetopology.GetNodeTopology(f.TopoCli, topologyUpdaterNode.Name)
			klog.Infof("Initial NRT: %q generation=%v resourceVersion=%v", prevNrt.Name, prevNrt.Generation, prevNrt.ResourceVersion)

			if _, ok := prevNrt.Annotations[podfingerprint.Annotation]; !ok {
				ginkgo.Skip("pod fingerprinting annotation not found - assuming disabled")
			}
			if _, ok := findAttribute(prevNrt.Attributes, podfingerprint.Attribute); !ok {
				ginkgo.Skip("pod fingerprinting attribute not found - assuming disabled")
			}

			dumpPods(f.K8SCli, topologyUpdaterNode.Name, "reference pods")

			updateInterval, method, err := estimateUpdateInterval(*prevNrt)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			klog.Infof("%s update interval: %s", method, updateInterval)

			// 3 timess is "long enough" - decided after quick tuning and try/error
			// if the object does not change, neither resourceVersion will. So we can just sleep.
			maxSteps := 3
			for step := 0; step < maxSteps; step++ {
				klog.Infof("waiting for %s: %d/%d", updateInterval, step+1, maxSteps)
				time.Sleep(updateInterval)
			}

			currNrt := e2enodetopology.GetNodeTopology(f.TopoCli, topologyUpdaterNode.Name)
			klog.Infof("Control NRT: %q generation=%v resourceVersion=%v", prevNrt.Name, prevNrt.Generation, prevNrt.ResourceVersion)

			// note we don't test no pods have been added/deleted. This is because the suite is supposed to own the cluster while it runs
			// IOW, if we don't create/delete pods explicitely, noone else is supposed to do
			pfpStable := expectPodFingerprint(*prevNrt, "==", *currNrt)
			if !pfpStable {
				dumpPods(f.K8SCli, topologyUpdaterNode.Name, "after PFP mismatch")
				// ignore errors and carry on. We don't want to fail the test because of missing debug info.
				dumpRTELogs(f.K8SCli, topologyUpdaterNode.Name)

			}
			gomega.Expect(pfpStable).To(gomega.BeTrue(), "PFP changed unexpectedly")
		})

		ginkgo.It("[release][PodFingerprint] it should report updated value if the set of running pods changes", func() {
			nodes, err := e2enodes.FilterNodesWithEnoughCores(workerNodes, "1000m")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			if len(nodes) < 1 {
				ginkgo.Skip("not enough allocatable cores for this test")
			}

			var currNrt *v1alpha2.NodeResourceTopology
			prevNrt := e2enodetopology.GetNodeTopology(f.TopoCli, topologyUpdaterNode.Name)

			if _, ok := prevNrt.Annotations[podfingerprint.Annotation]; !ok {
				ginkgo.Skip("pod fingerprinting not found - assuming disabled")
			}
			if _, ok := findAttribute(prevNrt.Attributes, podfingerprint.Attribute); !ok {
				ginkgo.Skip("pod fingerprinting attribute not found - assuming disabled")
			}

			dumpPods(f.K8SCli, topologyUpdaterNode.Name, "reference pods")

			sleeperPod := e2epods.MakeGuaranteedSleeperPod("1000m")
			pod, err := e2epods.CreateSync(f, sleeperPod)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			// (try to) delete the pod twice is no bother
			podNamespace, podName := pod.Namespace, pod.Name
			ginkgo.DeferCleanup(e2epods.DeletePodSyncByName, f, podNamespace, podName)

			updateTimeout := 5 * time.Minute
			currNrt = getUpdatedNRT(f.TopoCli, topologyUpdaterNode.Name, *prevNrt, updateTimeout)

			pfpChanged := expectPodFingerprint(*prevNrt, "!=", *currNrt)
			errMessage := "PFP did not change after pod creation"
			if !pfpChanged {
				dumpPods(f.K8SCli, topologyUpdaterNode.Name, errMessage)
			}
			gomega.Expect(pfpChanged).To(gomega.BeTrue(), errMessage)

			// since we need to delete the pod anyway, let's use this to run another check
			prevNrt = currNrt
			err = e2epods.DeletePodSyncByName(f, podNamespace, podName)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			currNrt = getUpdatedNRT(f.TopoCli, topologyUpdaterNode.Name, *prevNrt, updateTimeout)

			pfpChanged = expectPodFingerprint(*prevNrt, "!=", *currNrt)
			errMessage = "PFP did not change after pod deletion"
			if !pfpChanged {
				dumpPods(f.K8SCli, topologyUpdaterNode.Name, errMessage)
			}
			gomega.Expect(pfpChanged).To(gomega.BeTrue(), errMessage)
		})

		ginkgo.When("[release][numaplacement] testing numaplacement attributes", func() {
			// Node-level numaplacement.AttributeMetadata is published when pod fingerprinting is enabled; it holds
			// PackMetadata() from encoded container NUMA affinities (see resourcemonitor Scan + encodeContainerAffinities).
			// It is not a per-zone field; on single-NUMA nodes the encoder short-circuits and the value may stay empty
			// or unchanged when pods change.
			var (
				initialNRT        *v1alpha2.NodeResourceTopology
				initialMeta       string
				initialPayload    numaplacement.Payload
				initialContainers []numaplacement.ContainerID
				updateTimeout     time.Duration = 5 * time.Minute
			)

			ginkgo.BeforeEach(func() {
				initialNRT = e2enodetopology.GetNodeTopology(f.TopoCli, topologyUpdaterNode.Name)
				klog.Infof("Initial NRT: %q generation=%v resourceVersion=%v", initialNRT.Name, initialNRT.Generation, initialNRT.ResourceVersion)

				if _, ok := findAttribute(initialNRT.Attributes, podfingerprint.Attribute); !ok {
					ginkgo.Skip("pod fingerprinting attribute not found - assuming disabled")
				}
				tmPolicy, ok := findAttribute(initialNRT.Attributes, "topologyManagerPolicy")
				if !ok {
					ginkgo.Skip("topologyManagerPolicy attribute not found")
				}
				if tmPolicy != "single-numa-node" {
					ginkgo.Skip("topologyManagerPolicy is not single-numa-node - numaplacement containers encoding is not supported")
				}
				klog.Infof("topologyManagerPolicy is %q", tmPolicy)
				klog.Infof("initialNRT.Attributes: %v", initialNRT.Attributes)

				initialMeta, ok = findAttribute(initialNRT.Attributes, numaplacement.AttributeMetadata)
				if !ok {
					ginkgo.Skip("numaplacement metadata attribute not found")
				}

				ginkgo.By("checking the metadata is set with the expected data")
				gomega.Expect(initialMeta).ToNot(gomega.BeEmpty(), "numaplacement metadata is empty")
				gomega.Expect(initialMeta).To(gomega.HavePrefix(numaplacement.Prefix+numaplacement.Version), "numaplacement metadata is not prefixed properly")

				dumpPods(f.K8SCli, topologyUpdaterNode.Name, "reference pods")
				initialContainers = dumpContainers(f.K8SCli, topologyUpdaterNode.Name, false)

				initialPayload = numaplacement.Payload{}
				gomega.Expect(numaplacement.UnpackMetadataInto(&initialPayload, initialMeta)).To(gomega.Succeed())
				gomega.Expect(initialPayload.NUMANodes).To(gomega.Equal(len(initialNRT.Zones)), "numaplacement metadata must reflect the correct numer of NUMAs as found in the NRT")
				gomega.Expect(initialPayload.VectorEncoding).To(gomega.Equal(numaplacement.VectorEncodingLEB89), "numaplacement encoding must use LEB89 encoding")

			})

			ginkgo.Context("node-level metadata attribute", func() {
				ginkgo.It("should remain stable while workloads are unchanged", func() {
					maxSteps := 3
					updateInterval, method, err := estimateUpdateInterval(*initialNRT)
					gomega.Expect(err).ToNot(gomega.HaveOccurred())
					klog.Infof("%s update interval: %s", method, updateInterval)
					for step := 0; step < maxSteps; step++ {
						klog.Infof("waiting for %s: %d/%d", updateInterval, step+1, maxSteps)
						time.Sleep(updateInterval)
					}

					// note we don't test that no pods have been added/deleted. This is because the suite is supposed to own the cluster while it runs
					// IOW, if we don't create/delete pods explicitly, noone else is supposed to do
					currNrt := e2enodetopology.GetNodeTopology(f.TopoCli, topologyUpdaterNode.Name)
					klog.Infof("Current NRT: %q generation=%v resourceVersion=%v", currNrt.Name, currNrt.Generation, currNrt.ResourceVersion)

					metaAfter, ok := findAttribute(currNrt.Attributes, numaplacement.AttributeMetadata)
					gomega.Expect(ok).To(gomega.BeTrue(), "attribute %q missing after wait", numaplacement.AttributeMetadata)

					if initialMeta != metaAfter {
						infoUponFailure(f, topologyUpdaterNode.Name, initialContainers, "numaplacement metadata attribute changed unexpectedly")
					}

					gomega.Expect(metaAfter).To(gomega.Equal(initialMeta), "numaplacement metadata attribute changed unexpectedly")
					currContainers := dumpContainers(f.K8SCli, topologyUpdaterNode.Name, false)
					gomega.Expect(getContainersListCmpDiff(initialContainers, currContainers)).To(gomega.BeEmpty(), "containers list changed unexpectedly")
				})

				ginkgo.Context("multi-NUMA nodes", func() {
					type tableTest struct {
						expectedQoS                        corev1.PodQOSClass
						resourcesPerContainer              []corev1.ResourceRequirements
						eligibleContainersForNUMAPlacement int
					}
					ginkgo.BeforeEach(func() {
						if len(initialNRT.Zones) < 2 {
							ginkgo.Skip("single NUMA zone in NRT: affinity encoding does not vary with pod set (encoder short-circuit)")
						}
					})

					ginkgo.DescribeTable("numaplacement metadata on multi-NUMA nodes when pods are added and removed",
						func(test tableTest) {
							ginkgo.By("creating a sleeper pod")
							sleeperPod := e2epods.MakeSleeperPod(test.resourcesPerContainer)
							sleeperPod.Spec.NodeName = topologyUpdaterNode.Name
							pod, err := e2epods.CreateSync(f, sleeperPod)
							gomega.Expect(err).ToNot(gomega.HaveOccurred())
							gomega.Expect(pod.Status.QOSClass).To(gomega.Equal(test.expectedQoS))

							podNamespace, podName := pod.Namespace, pod.Name
							needsCleanup := true
							ginkgo.DeferCleanup(func() {
								if needsCleanup {
									gomega.Expect(e2epods.DeletePodSyncByName(f, podNamespace, podName)).To(gomega.Succeed())
								}
							})

							ginkgo.By("getting the updated NRT after pod creation")
							nrtPostPodCreation := getUpdatedNRT(f.TopoCli, topologyUpdaterNode.Name, *initialNRT, updateTimeout)
							metaPostPodCreation, ok := findAttribute(nrtPostPodCreation.Attributes, numaplacement.AttributeMetadata)
							gomega.Expect(ok).To(gomega.BeTrue(), "attribute %q missing after pod creation", numaplacement.AttributeMetadata)

							newPayload := numaplacement.Payload{}
							gomega.Expect(numaplacement.UnpackMetadataInto(&newPayload, metaPostPodCreation)).To(gomega.Succeed())

							ginkgo.By(fmt.Sprintf("checking the numaplacement metadata after running the test workload. expecting a change? %t", test.eligibleContainersForNUMAPlacement > 0))
							if test.eligibleContainersForNUMAPlacement > 0 {
								expectedContainersLen := initialPayload.Containers + test.eligibleContainersForNUMAPlacement
								failureMessage := "numaplacement info did not change after a workload with exclusive resources was scheduled"
								if metaPostPodCreation == initialMeta || newPayload.Containers != expectedContainersLen {
									infoUponFailure(f, topologyUpdaterNode.Name, initialContainers, failureMessage)
								}
								gomega.Expect(metaPostPodCreation).ToNot(gomega.Equal(initialMeta), failureMessage)
								gomega.Expect(newPayload.Containers).To(gomega.Equal(expectedContainersLen), failureMessage)
							} else {
								failureMessage := "numaplacement info changed unexpectedly after a workload with non-exclusive resources was scheduled"
								if metaPostPodCreation != initialMeta || newPayload.Containers != initialPayload.Containers {
									infoUponFailure(f, topologyUpdaterNode.Name, initialContainers, failureMessage)
								}
								gomega.Expect(metaPostPodCreation).To(gomega.Equal(initialMeta), failureMessage)
								gomega.Expect(newPayload.Containers).To(gomega.Equal(initialPayload.Containers), failureMessage)
							}

							ginkgo.By("verifying numaplacement attributes for NUMA zone")
							// we cannot tell which zone(s) will have non empty vectors but we can tell for sure that at least
							//  the busiest node should NOT have the vector
							var busiestZoneAttributes v1alpha2.AttributeList
							for _, zone := range nrtPostPodCreation.Zones {
								// to be safe from confusing indeces and names, extract the zone id and compare it to the busiest node id in the payload
								zoneId := strings.TrimPrefix(zone.Name, "node-")
								zoneIdInt, err := strconv.Atoi(strings.TrimSpace(zoneId))
								gomega.Expect(err).ToNot(gomega.HaveOccurred())
								klog.InfoS("zone", "name", zone.Name, "extractedId", zoneIdInt, "busiestNode", newPayload.BusiestNode)
								if zoneIdInt == newPayload.BusiestNode {
									busiestZoneAttributes = zone.Attributes
									break
								}
							}

							vecAttr, ok := findAttribute(busiestZoneAttributes, numaplacement.AttributeVector)
							gomega.Expect(ok).To(gomega.BeFalse(), "found vector attribute on the busiest zone: %q", vecAttr)

							// ASSUMPTION: no containers are eligible for NUMA placement before having this test running.
							// If this is proved to be incorrect by time, we either need to remove this check or improve
							// to consider the existing containers.
							// if there is only one container eligible for NUMA placement it would be on the busiest zone
							// which also should not have the vector
							if test.eligibleContainersForNUMAPlacement <= 1 {
								ginkgo.By("all zones should have empty vectors")
								for _, zone := range nrtPostPodCreation.Zones {
									vecAttr, ok := findAttribute(zone.Attributes, numaplacement.AttributeVector)
									gomega.Expect(ok).To(gomega.BeFalse(), "found vector attribute on zone %q: %q", zone.Name, vecAttr)
								}
							}

							ginkgo.By("deleting the pod and checking the numaplacement info is reset to the initial state")
							gomega.Expect(e2epods.DeletePodSyncByName(f, podNamespace, podName)).To(gomega.Succeed())
							needsCleanup = false

							afterDeleteNrt := getUpdatedNRT(f.TopoCli, topologyUpdaterNode.Name, *nrtPostPodCreation, updateTimeout)
							metaAfter, ok := findAttribute(afterDeleteNrt.Attributes, numaplacement.AttributeMetadata)
							gomega.Expect(ok).To(gomega.BeTrue())
							failureMessage := "mismatched metadata after pod removal"
							if metaAfter != initialMeta {
								infoUponFailure(f, topologyUpdaterNode.Name, initialContainers, failureMessage)
							}
							gomega.Expect(metaAfter).To(gomega.Equal(initialMeta), failureMessage)
						},
						ginkgo.Entry("guaranteed multi-container pod",
							tableTest{
								expectedQoS: corev1.PodQOSGuaranteed,
								resourcesPerContainer: []corev1.ResourceRequirements{
									{
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("1000m"),
											corev1.ResourceMemory: resource.MustParse("250Mi"),
										},
									},
									{
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("500m"),
											corev1.ResourceMemory: resource.MustParse("250Mi"),
										},
									},
								},
								eligibleContainersForNUMAPlacement: 2,
							}),
						ginkgo.Entry("burstable multi-container pod with no exclusive resources",
							tableTest{
								expectedQoS: corev1.PodQOSBurstable,
								resourcesPerContainer: []corev1.ResourceRequirements{
									{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("1000m"),
											corev1.ResourceMemory: resource.MustParse("250Mi"),
										},
									},
									{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("500m"),
											corev1.ResourceMemory: resource.MustParse("32Mi"),
										},
									},
								},
								eligibleContainersForNUMAPlacement: 0,
							}),
						ginkgo.Entry("burstable multi-container pod with exclusive resource on one container only",
							tableTest{
								expectedQoS: corev1.PodQOSBurstable,
								resourcesPerContainer: []corev1.ResourceRequirements{
									{
										Limits: corev1.ResourceList{
											corev1.ResourceCPU: resource.MustParse("1000m"),
											corev1.ResourceName(e2etestenv.GetDeviceName()): resource.MustParse("1"),
										},
									},
									{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("500m"),
											corev1.ResourceMemory: resource.MustParse("32Mi"),
										},
									},
								},
								eligibleContainersForNUMAPlacement: 1,
							},
						),
						ginkgo.Entry("best effort pod - nothing exclusive",
							tableTest{
								expectedQoS: corev1.PodQOSBestEffort,
								resourcesPerContainer: []corev1.ResourceRequirements{
									{
										Limits: corev1.ResourceList{},
									},
									{
										Limits: corev1.ResourceList{},
									},
								},
								eligibleContainersForNUMAPlacement: 0,
							}),
						ginkgo.Entry("best effort multi-container pod with exclusive resource on one container only",
							tableTest{
								expectedQoS: corev1.PodQOSBestEffort,
								resourcesPerContainer: []corev1.ResourceRequirements{
									{
										Limits: corev1.ResourceList{
											corev1.ResourceName(e2etestenv.GetDeviceName()): resource.MustParse("1"),
										},
									},
									{
										Limits: corev1.ResourceList{},
									},
								},
								eligibleContainersForNUMAPlacement: 1,
							},
						),
					)
				})
			})
		})
	})

	ginkgo.Context("with refresh-node-resources enabled", func() {
		ginkgo.It("[NodeRefresh] should be able to detect devices", func() {
			gomega.Eventually(func(g gomega.Gomega) {
				nrt := e2enodetopology.GetNodeTopology(f.TopoCli, topologyUpdaterNode.Name)
				devName := e2etestenv.GetDeviceName()
				found := false
				for _, zone := range nrt.Zones {
					for _, res := range zone.Resources {
						if res.Name == devName {
							found = true
						}
					}
				}
				g.Expect(found).To(gomega.BeTrue(), "device: %q was not found in NRT: %q", devName, topologyUpdaterNode.Name)
			}).WithTimeout(30 * time.Second).WithPolling(10 * time.Second).Should(gomega.Succeed())
		})

		ginkgo.It("[NodeRefresh] should log the refresh message", func() {
			rtePod, err := e2epods.GetPodOnNode(f.K8SCli, topologyUpdaterNode.Name, e2etestenv.GetNamespaceName(), e2etestenv.RTELabelName)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			rteContainerName, err := e2ertepod.FindRTEContainerName(rtePod)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func(g gomega.Gomega) {
				logs, err := e2epods.GetLogsForPod(f.K8SCli, rtePod.Namespace, rtePod.Name, rteContainerName)
				g.Expect(err).ToNot(gomega.HaveOccurred())

				g.Expect(logs).To(gomega.Or(
					gomega.ContainSubstring("tracking node resources"),
					gomega.ContainSubstring("update node resources"),
				), "container: %q in pod: %q doesn't contain the refresh log message", rteContainerName, rtePod.Name)
			}).WithTimeout(42 * time.Second).WithPolling(5 * time.Second).Should(gomega.Succeed())
		})
	})
})

func infoUponFailure(f *fixture.Fixture, nodeName string, initialContainers []numaplacement.ContainerID, message string) {
	dumpPods(f.K8SCli, nodeName, message)
	currContainers := dumpContainers(f.K8SCli, nodeName, true)
	klog.Infof("containers list diff: %s", getContainersListCmpDiff(initialContainers, currContainers))
	_ = dumpRTELogs(f.K8SCli, nodeName)
}

func getUpdatedNRT(topologyClient *topologyclientset.Clientset, nodeName string, prevNrt v1alpha2.NodeResourceTopology, timeout time.Duration) *v1alpha2.NodeResourceTopology {
	ginkgo.GinkgoHelper()
	effectiveTimeout := timeout + updateIntervalExtraSafety
	klog.Infof("waiting for NRT %q update: timeout %v (base %v + safety %v) prev resourceVersion=%v", nodeName, effectiveTimeout, timeout, updateIntervalExtraSafety, prevNrt.ObjectMeta.ResourceVersion)
	var err error
	var currNrt *v1alpha2.NodeResourceTopology
	count := 0
	stablePFP := ""
	gomega.Eventually(func(g gomega.Gomega) {
		currNrt, err = topologyClient.TopologyV1alpha2().NodeResourceTopologies().Get(context.TODO(), nodeName, metav1.GetOptions{})
		g.Expect(err).ToNot(gomega.HaveOccurred(), "failed to get the node topology resource")
		g.Expect(currNrt.ObjectMeta.ResourceVersion).ToNot(gomega.Equal(prevNrt.ObjectMeta.ResourceVersion), "resource %s not yet updated - resource version not bumped", nodeName)

		currentPFP, ok := currNrt.Annotations[podfingerprint.Annotation]
		g.Expect(ok).To(gomega.BeTrue(), "pod fingerprint annotation not found in NRT %q", nodeName)

		if stablePFP == "" {
			stablePFP = currentPFP
		} else if stablePFP != currentPFP {
			stablePFP = currentPFP
			count = 0
		}
		count++
		klog.InfoS("PFP stabilization", "PFP", currentPFP, "count", count)
		g.Expect(count).To(gomega.Equal(5), "PFP did not stabilize yet")
	}).WithTimeout(effectiveTimeout).WithPolling(10*time.Second).Should(gomega.Succeed(), "NRT did not settle")

	return currNrt
}

func dumpPods(cs clientset.Interface, nodeName, message string) {
	nodeSelector := fields.Set{
		"spec.nodeName": nodeName,
	}.AsSelector().String()

	pods, err := cs.CoreV1().Pods(e2etestenv.GetNamespaceName()).List(context.TODO(), metav1.ListOptions{FieldSelector: nodeSelector})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	klog.Infof("BEGIN pods running on %q: %s", nodeName, message)
	for _, pod := range pods.Items {
		klog.Infof("%s %s/%s status=%s (%s %s)", nodeName, pod.Namespace, pod.Name, pod.Status.Phase, pod.Status.Message, pod.Status.Reason)
	}
	klog.Infof("END pods running on %q: %s", nodeName, message)
}

func dumpContainers(cs clientset.Interface, nodeName string, printInfo bool) []numaplacement.ContainerID {
	nodeSelector := fields.Set{
		"spec.nodeName": nodeName,
	}.AsSelector().String()

	pods, err := cs.CoreV1().Pods(e2etestenv.GetNamespaceName()).List(context.TODO(), metav1.ListOptions{FieldSelector: nodeSelector})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	containers := []numaplacement.ContainerID{}
	containersSB := strings.Builder{}
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			cntID := numaplacement.ContainerID{
				Namespace:     pod.Namespace,
				PodName:       pod.Name,
				ContainerName: container.Name,
			}
			containers = append(containers, cntID)
			containersSB.WriteString(cntID.String() + "\n")
		}
	}

	if printInfo {
		klog.InfoS("Containers IDs running on node", "node", nodeName, "containers", containersSB.String())
	}
	return containers
}

func getContainersListCmpDiff(initialContainers, currentContainers []numaplacement.ContainerID) string {
	initialCopy := slices.Clone(initialContainers)
	currentCopy := slices.Clone(currentContainers)
	sort.Slice(initialCopy, func(i, j int) bool {
		return initialCopy[i].String() < initialCopy[j].String()
	})
	sort.Slice(currentCopy, func(i, j int) bool {
		return currentCopy[i].String() < currentCopy[j].String()
	})
	return cmp.Diff(initialCopy, currentCopy)
}
func expectPodFingerprint(nrt1 v1alpha2.NodeResourceTopology, mode string, nrt2 v1alpha2.NodeResourceTopology) bool {
	pfp1, ok1 := extractPFP(nrt1)
	if !ok1 {
		return false
	}

	pfp2, ok2 := extractPFP(nrt2)
	if !ok2 {
		return false
	}

	switch mode {
	case "==":
		return expectEqualPFPs(nrt1.Name, pfp1, nrt2.Name, pfp2)
	case "!=":
		return expectDifferentPFPs(nrt1.Name, pfp1, nrt2.Name, pfp2)
	default:
		klog.Infof("unsupported comparison mode %q", mode)
		return false
	}
}

func extractPFP(nrt v1alpha2.NodeResourceTopology) (string, bool) {
	pfpAnn, okAnn := nrt.Annotations[podfingerprint.Annotation]
	if !okAnn {
		klog.Infof("cannot find pod fingerprint annotation in NRT %q", nrt.Name)
		return "", false
	}
	pfpAttr, okAttr := findAttribute(nrt.Attributes, podfingerprint.Attribute)
	if !okAttr {
		klog.Infof("cannot find pod fingerprint attribute in NRT %q", nrt.Name)
		return "", false
	}
	if pfpAnn != pfpAttr {
		klog.Infof("PFP mismatch in %q  annotation=%q attribute=%q", nrt.Name, pfpAnn, pfpAttr)
		return "", false
	}
	return pfpAttr, true
}

func expectEqualPFPs(name1, pfp1, name2, pfp2 string) bool {
	if pfp1 != pfp2 {
		klog.Infof("fingerprint mismatch NRT %q PFP %q vs NRT %q PFP %q", name1, pfp1, name2, pfp2)
		return false
	}
	return true
}

func expectDifferentPFPs(name1, pfp1, name2, pfp2 string) bool {
	if pfp1 == pfp2 {
		klog.Infof("fingerprint equality NRT %q PFP %q vs NRT %q PFP %q", name2, pfp1, name2, pfp2)
		return false
	}
	return true
}

func estimateUpdateInterval(nrt v1alpha2.NodeResourceTopology) (time.Duration, string, error) {
	fallbackInterval, err := time.ParseDuration(e2etestenv.GetPollInterval())
	if err != nil {
		return fallbackInterval, "estimated", err
	}
	klog.Infof("Annotations for %q: %#v", nrt.Name, nrt.Annotations)
	updateIntervalAnn, ok := nrt.Annotations[k8sannotations.UpdateInterval]
	if !ok {
		// no annotation, we need to guess
		return fallbackInterval, "estimated", nil
	}
	updateInterval, err := time.ParseDuration(updateIntervalAnn)
	if err != nil || updateInterval <= 0 {
		klog.Warningf("annotation %q has unusable value %q (parsed=%v err=%v), falling back to %v", k8sannotations.UpdateInterval, updateIntervalAnn, updateInterval, err, fallbackInterval)
		return fallbackInterval, "estimated", err
	}
	return updateInterval, "computed", nil
}

func dumpRTELogs(cs clientset.Interface, nodeName string) error {
	rtePod, err := e2epods.GetPodOnNode(cs, nodeName, e2etestenv.GetNamespaceName(), e2etestenv.RTELabelName)
	if err != nil {
		return err
	}

	rteContainerName, err := e2ertepod.FindRTEContainerName(rtePod)
	if err != nil {
		return err
	}

	logs, err := e2epods.GetLogsForPod(cs, rtePod.Namespace, rtePod.Name, rteContainerName)
	if err != nil {
		return err
	}

	klog.Infof("RTE logs:\n%s", logs)
	return nil
}

func findAttribute(attrs v1alpha2.AttributeList, name string) (string, bool) {
	for _, attr := range attrs {
		if attr.Name == name {
			return attr.Value, true
		}
	}
	return "", false
}
