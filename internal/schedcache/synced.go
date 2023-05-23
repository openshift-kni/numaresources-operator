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

package schedcache

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/podfingerprint"
	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/status"

	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
)

const (
	TracingDirectory = "/run/pfpstatus"
)

func HasSynced(ctx context.Context, cli client.Client, k8sCli kubernetes.Interface, nodeNames []string) (bool, map[string]sets.String, error) {
	var err error
	var nroSched nropv1.NUMAResourcesScheduler
	nroKey := client.ObjectKey{Name: objectnames.DefaultNUMAResourcesSchedulerCrName}

	err = cli.Get(context.TODO(), nroKey, &nroSched)
	if err != nil {
		return false, nil, err
	}

	cond := status.FindCondition(nroSched.Status.Conditions, status.ConditionAvailable)
	if cond == nil {
		return false, nil, fmt.Errorf("missing condition: available")
	}

	dp, err := podlist.With(cli).DeploymentByOwnerReference(ctx, nroSched.UID)
	if err != nil {
		return false, nil, err
	}

	if dp.Status.ReadyReplicas != *dp.Spec.Replicas {
		return false, nil, fmt.Errorf("scheduler dp not ready (%d/%d)", dp.Status.ReadyReplicas, *dp.Spec.Replicas)
	}

	unsynced := make(map[string]sets.String)

	podList, err := podlist.With(cli).ByDeployment(ctx, *dp)
	for idx := range podList {
		pod := &podList[idx]

		notReady, err := ReplicaHasSynced(k8sCli, pod, nodeNames)
		mergeUnsynced(unsynced, notReady)
		if err != nil {
			return len(unsynced) == 0, unsynced, err
		}
	}

	return len(unsynced) == 0, unsynced, nil
}

func ReplicaHasSynced(k8sCli kubernetes.Interface, pod *corev1.Pod, nodeNames []string) (map[string]sets.String, error) {
	unsynced := make(map[string]sets.String)
	for _, nodeName := range nodeNames {
		ok, detectedPods, err := ReplicaHasSyncedForNode(k8sCli, pod, nodeName)
		if err != nil {
			return unsynced, err
		}
		if ok {
			continue
		}
		unsynced[nodeName] = detectedPods
	}

	return unsynced, nil
}

func ReplicaHasSyncedForNode(k8sCli kubernetes.Interface, pod *corev1.Pod, nodeName string) (bool, sets.String, error) {
	detectedPods := make(sets.String)
	stdout, _, err := remoteexec.CommandOnPod(k8sCli, pod, "/bin/cat", filepath.Join(TracingDirectory, nodeNameToFileName(nodeName)+".json"))
	if err != nil {
		return false, detectedPods, err
	}
	var status podfingerprint.Status
	err = json.Unmarshal(stdout, &status)
	if err != nil {
		return false, detectedPods, err
	}

	hasSync := (status.FingerprintComputed == status.FingerprintExpected)

	if hasSync {
		klog.Infof("synced cache on %q", status.NodeName)
	} else {
		klog.Warningf("unsynced cache on %q - fingerprint expected %q computed %q", status.NodeName, status.FingerprintExpected, status.FingerprintComputed)
		klog.Warningf("unsynced cache on %q - detected pods (%d)", status.NodeName, len(status.Pods))
		for idx, nn := range status.Pods {
			detected := nn.String()
			detectedPods.Insert(detected)
			klog.Warningf("- %3d/%3d %s", idx+1, len(status.Pods), detected)
		}
	}

	return hasSync, detectedPods, nil
}

func GetUpdaterFingerprintStatus(k8sCli kubernetes.Interface, podNamespace, podName, cntName string) (podfingerprint.Status, error) {
	var st podfingerprint.Status
	stdout, _, err := remoteexec.CommandOnPodByNames(k8sCli, podNamespace, podName, cntName, "/bin/cat", filepath.Join(TracingDirectory, "dump.json"))
	if err != nil {
		return st, err
	}
	err = json.Unmarshal([]byte(stdout), &st)
	return st, err
}

func mergeUnsynced(total, partial map[string]sets.String) {
	for nodeName, detectedPods := range partial {
		existing := total[nodeName]
		total[nodeName] = existing.Union(detectedPods)
	}
}

func nodeNameToFileName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}
