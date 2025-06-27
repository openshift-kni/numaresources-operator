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

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/podfingerprint"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

const (
	TracingDirectory = "/run/pfpstatus"
)

type Env struct {
	Ctx    context.Context
	Cli    client.Client
	K8sCli kubernetes.Interface
	Log    logr.Logger
}

func HasSynced(env *Env, nodeNames []string) (bool, map[string]sets.Set[string], error) {
	var err error
	var nroSched nropv1.NUMAResourcesScheduler
	nroKey := client.ObjectKey{Name: objectnames.DefaultNUMAResourcesSchedulerCrName}

	err = env.Cli.Get(context.TODO(), nroKey, &nroSched)
	if err != nil {
		return false, nil, err
	}

	cond := status.FindCondition(nroSched.Status.Conditions, status.ConditionAvailable)
	if cond == nil {
		return false, nil, fmt.Errorf("missing condition: available")
	}

	dp, err := podlist.With(env.Cli).DeploymentByOwnerReference(env.Ctx, nroSched.UID)
	if err != nil {
		return false, nil, err
	}

	if dp.Status.ReadyReplicas != *dp.Spec.Replicas {
		return false, nil, fmt.Errorf("scheduler dp not ready (%d/%d)", dp.Status.ReadyReplicas, *dp.Spec.Replicas)
	}

	unsynced := make(map[string]sets.Set[string])

	//nolint: ineffassign,staticcheck,wastedassign
	podList, err := podlist.With(env.Cli).ByDeployment(env.Ctx, *dp)
	for idx := range podList {
		pod := &podList[idx]

		notReady, err := ReplicaHasSynced(env, pod, nodeNames)
		mergeUnsynced(unsynced, notReady)
		if err != nil {
			return len(unsynced) == 0, unsynced, err
		}
	}

	return len(unsynced) == 0, unsynced, nil
}

func ReplicaHasSynced(env *Env, pod *corev1.Pod, nodeNames []string) (map[string]sets.Set[string], error) {
	unsynced := make(map[string]sets.Set[string])
	for _, nodeName := range nodeNames {
		ok, detectedPods, err := ReplicaHasSyncedForNode(env, pod, nodeName)
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

func ReplicaHasSyncedForNode(env *Env, pod *corev1.Pod, nodeName string) (bool, sets.Set[string], error) {
	detectedPods := sets.New[string]()
	stdout, _, err := remoteexec.CommandOnPod(env.Ctx, env.K8sCli, pod, "/bin/cat", filepath.Join(TracingDirectory, nodeNameToFileName(nodeName)+".json"))
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

func GetUpdaterFingerprintStatus(env *Env, podNamespace, podName, cntName string) (podfingerprint.Status, error) {
	var st podfingerprint.Status
	stdout, _, err := remoteexec.CommandOnPodByNames(env.Ctx, env.K8sCli, podNamespace, podName, cntName, "/bin/cat", filepath.Join(TracingDirectory, "dump.json"))
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(stdout, &st)
	return st, err
}

func mergeUnsynced(total, partial map[string]sets.Set[string]) {
	for nodeName, detectedPods := range partial {
		existing := total[nodeName]
		total[nodeName] = existing.Union(detectedPods)
	}
}

func nodeNameToFileName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}
