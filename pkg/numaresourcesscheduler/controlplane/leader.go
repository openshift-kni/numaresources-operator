/*
 * Copyright 2026 Red Hat, Inc.
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

package controlplane

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	nrs "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler"
)

// GetLeaderPod returns the namespace/name of the scheduler pod currently holding the leader
// election lease. Typically used for the scheduler pods, but meant to work with any pod using
// the standard kubernetes leader election.
// The kube-scheduler framework sets the lease HolderIdentity to
// "<hostname>_<uuid>", where hostname is the pod name inside a Kubernetes pod.
func GetLeaderPod(ctx context.Context, k8sCli kubernetes.Interface, namespace string) (types.NamespacedName, error) {
	lease, err := k8sCli.CoordinationV1().Leases(namespace).Get(ctx, nrs.LeaderElectionResourceName, metav1.GetOptions{})
	if err != nil {
		return types.NamespacedName{}, fmt.Errorf("failed to get lease %s/%s: %w", namespace, nrs.LeaderElectionResourceName, err)
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		return types.NamespacedName{}, fmt.Errorf("lease %s/%s has no holder", namespace, nrs.LeaderElectionResourceName)
	}
	holderIdentity := *lease.Spec.HolderIdentity
	// HolderIdentity format is "<pod-name>_<uuid>", split at the last "_" to extract the pod name
	idx := strings.LastIndex(holderIdentity, "_")
	if idx == -1 {
		return types.NamespacedName{}, fmt.Errorf("unexpected holder identity format: %q", holderIdentity)
	}
	podName := holderIdentity[:idx]
	return types.NamespacedName{Namespace: namespace, Name: podName}, nil
}
