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

package nrosched

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	// scheduler
	ReasonScheduled        = "Scheduled"
	ReasonFailedScheduling = "FailedScheduling"
	// kubelet
	ReasonTopologyAffinityError = "TopologyAffinityError"

	// scheduler
	ErrorCannotAlignPod       = "cannot align pod"
	ErrorCannotAlignContainer = "cannot align container"
	// kubeket
	ErrorTopologyAffinityError = "Resources cannot be allocated with Topology locality"

	// component name
	kubeletName = "kubelet"
)

type eventChecker func(ev corev1.Event) bool

func checkPODEvents(k8sCli *kubernetes.Clientset, podNamespace, podName string, evCheck eventChecker) (bool, error) {
	By(fmt.Sprintf("checking events for pod %s/%s", podNamespace, podName))
	opts := metav1.ListOptions{
		FieldSelector: fields.SelectorFromSet(map[string]string{
			"involvedObject.name":      podName,
			"involvedObject.namespace": podNamespace,
			// TODO: use uid
		}).String(),
		TypeMeta: metav1.TypeMeta{Kind: "Pod"},
	}
	events, err := k8sCli.CoreV1().Events(podNamespace).List(context.TODO(), opts)
	if err != nil {
		klog.ErrorS(err, "cannot get events for pod %s/%s", podNamespace, podName)
		return false, err
	}
	if len(events.Items) == 0 {
		return false, fmt.Errorf("no event received for %s/%s", podNamespace, podName)
	}

	for _, item := range events.Items {
		evStr := eventToString(item)
		klog.Infof("checking event: %s/%s [%s]", podNamespace, podName, evStr)
		if evCheck(item) {
			klog.Infof("-> found relevant scheduling event for pod %s/%s: [%s]", podNamespace, podName, evStr)
			return true, nil
		}
	}
	klog.Warningf("Failed to find relevant scheduling event for pod %s/%s", podNamespace, podName)
	return false, nil
}

func CheckPODSchedulingFailed(k8sCli *kubernetes.Clientset, podNamespace, podName, schedulerName string) (bool, error) {
	isFailedScheduling := func(item corev1.Event) bool {
		return item.Reason == ReasonFailedScheduling && item.ReportingController == schedulerName
	}
	return checkPODEvents(k8sCli, podNamespace, podName, isFailedScheduling)
}

func CheckPODKubeletRejectWithTopologyAffinityError(k8sCli *kubernetes.Clientset, podNamespace, podName string) (bool, error) {
	isKubeletRejectForTopologyAffinityError := func(item corev1.Event) bool {
		if item.Reason != ReasonTopologyAffinityError {
			klog.Warningf("pod %s/%s reason %q expected %q", podNamespace, podName, item.Reason, ReasonTopologyAffinityError)
			return false
		}
		// kubernetes is quirky and the component naming is a bit of hard to grok
		if item.Source.Component != kubeletName {
			klog.Warningf("pod %s/%s controller %q expected %q", podNamespace, podName, item.Source.Component, kubeletName)
			return false
		}
		if !strings.Contains(item.Message, ErrorTopologyAffinityError) {
			klog.Warningf("pod %s/%s message %q expected %q", podNamespace, podName, item.Message, ErrorTopologyAffinityError)
			return false
		}
		return true
	}
	return checkPODEvents(k8sCli, podNamespace, podName, isKubeletRejectForTopologyAffinityError)
}

// This function assumes a TMPolicy of "single-numa-node"
func CheckPODSchedulingFailedForAlignment(k8sCli *kubernetes.Clientset, podNamespace, podName, schedulerName, scope string) (bool, error) {
	var alignmentErr string
	if scope == intnrt.Container {
		alignmentErr = ErrorCannotAlignContainer
	} else {
		alignmentErr = ErrorCannotAlignPod
	}

	return CheckPodSchedulingFailedWithMsg(k8sCli, podNamespace, podName, schedulerName, alignmentErr)
}

func CheckPodSchedulingFailedWithMsg(k8sCli *kubernetes.Clientset, podNamespace, podName, schedulerName, eventMsg string) (bool, error) {
	isFailedSchedulingWithMsg := func(item corev1.Event) bool {
		if item.Reason != ReasonFailedScheduling {
			klog.Warningf("pod %s/%s reason %q expected %q", podNamespace, podName, item.Reason, ReasonFailedScheduling)
			return false
		}
		if item.ReportingController != schedulerName {
			klog.Warningf("pod %s/%s controller %q expected %q", podNamespace, podName, item.ReportingController, schedulerName)
			return false
		}
		// workaround kubelet race/confusing behavior
		if !strings.Contains(item.Message, eventMsg) {
			klog.Warningf("pod %s/%s message %q expected %q", podNamespace, podName, item.Message, eventMsg)
			return false
		}
		return true
	}
	return checkPODEvents(k8sCli, podNamespace, podName, isFailedSchedulingWithMsg)
}

func CheckPODWasScheduledWith(k8sCli *kubernetes.Clientset, podNamespace, podName, schedulerName string) (bool, error) {
	isScheduledWith := func(item corev1.Event) bool {
		return item.Reason == ReasonScheduled && item.ReportingController == schedulerName
	}
	return checkPODEvents(k8sCli, podNamespace, podName, isScheduledWith)
}

// CheckNROSchedulerAvailable finds the NUMA-aware scheduler by name, verifies its
// status confirms a deployment and returns a pointer to it. The calling test will
// fail if any verification is unsuccessful.
func CheckNROSchedulerAvailable(ctx context.Context, cli client.Client, schedObjName string) *nropv1.NUMAResourcesScheduler {
	GinkgoHelper()

	nroSchedObj := &nropv1.NUMAResourcesScheduler{}
	Eventually(func() error {
		By(fmt.Sprintf("checking %q for the condition Available=true", schedObjName))
		err := cli.Get(ctx, objects.NROSchedObjectKey(), nroSchedObj)
		if err != nil {
			return fmt.Errorf("failed to get the scheduler resource: %v", err)
		}
		cond := status.FindCondition(nroSchedObj.Status.Conditions, status.ConditionAvailable)
		if cond == nil {
			return fmt.Errorf("missing conditions in %v", nroSchedObj)
		}
		klog.Infof("condition: %v", cond)
		if cond.Status != metav1.ConditionTrue {
			return fmt.Errorf("available condition not satisfied")
		}
		return nil
	}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "Scheduler condition did not become available")

	dpNName := nroSchedObj.Status.Deployment
	Expect(dpNName.Namespace).ToNot(BeEmpty(), "scheduler deployment missing: %q", dpNName.String())
	Expect(dpNName.Name).ToNot(BeEmpty(), "scheduler deployment missing: %q", dpNName.String())

	return nroSchedObj
}

func eventToString(ev corev1.Event) string {
	evSrc := eventSourceToString(ev.Source)
	evSrcSep := ""
	if evSrc != "" {
		evSrcSep = " "
	}
	return fmt.Sprintf("type=%q action=%q message=%q reason=%q reportedBy={%s/%s}",
		ev.Type, ev.Action, ev.Message, ev.Reason, ev.ReportingController, ev.ReportingInstance,
	) + evSrcSep + evSrc
}

func eventSourceToString(evs corev1.EventSource) string {
	if evs.Host != "" && evs.Component != "" {
		return "from={" + evs.Host + "/" + evs.Component + "}"
	}
	if evs.Host != "" {
		return "from={" + evs.Host + "}"
	}
	if evs.Component != "" {
		return "from={" + evs.Component + "}"
	}
	return ""
}
