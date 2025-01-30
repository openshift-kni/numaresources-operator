/*
 * Copyright 2024 Red Hat, Inc.
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

package deploy

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
)

func FindNUMAResourcesOperatorPod(ctx context.Context, cli client.Client, nrop *nropv1.NUMAResourcesOperator) (*corev1.Pod, error) {
	if len(nrop.Status.NodeGroups) < 1 {
		return nil, errors.New("node groups not reported, nothing to do")
	}
	// nrop places all daemonsets in the same namespace on which it resides, so any group is fine
	namespace := nrop.Status.NodeGroups[0].DaemonSet.Namespace // shortcut
	klog.InfoS("NROP pod", "namespace", namespace)

	sel, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app": "numaresources-operator",
		},
	})
	if err != nil {
		return nil, err
	}
	klog.InfoS("NROP pod", "selector", sel.String())

	podList := corev1.PodList{}
	err = cli.List(ctx, &podList, &client.ListOptions{Namespace: namespace, LabelSelector: sel})
	if err != nil {
		return nil, err
	}

	if len(podList.Items) != 1 {
		return nil, fmt.Errorf("unexpected number of pods found: %d", len(podList.Items))
	}

	return &podList.Items[0], nil
}
