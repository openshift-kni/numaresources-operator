/*
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
 *
 * Copyright 2022 Red Hat, Inc.
 */

package hash

import (
	"context"
	"crypto/sha256"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// hash package purpose is to compute a ConfigMap hash
// that will be attached as an annotation to workload resources (DaemonSet, Deployment, etc.)
// in order to allow them to track ConfigMap changes
// more about this technique here: https://blog.questionable.services/article/kubernetes-deployments-configmap-change/

const ConfigMapAnnotation = "configmap.hash"

func ComputeCurrentConfigMap(ctx context.Context, cli client.Client, cm *corev1.ConfigMap) (string, error) {
	updatedConfigMap := &corev1.ConfigMap{}
	key := client.ObjectKeyFromObject(cm)

	if err := cli.Get(ctx, key, updatedConfigMap); err != nil {
		// ConfigMap not created yet, use the data from the manifests
		if apierrors.IsNotFound(err) {
			updatedConfigMap = cm
		} else {
			return "", fmt.Errorf("could not calculate ConfigMap %q hash: %w", key.String(), err)
		}
	}
	cmHash := ConfigMapData(updatedConfigMap)
	klog.InfoS("configmap hash calculated", "hash", cmHash)
	return cmHash, nil
}

func ConfigMapData(cm *corev1.ConfigMap) string {
	var dataAsString string

	if cm.Data != nil {
		dataAsString = fmt.Sprintf("%v", cm.Data)
	}
	if cm.BinaryData != nil {
		dataAsString += fmt.Sprintf("%v", cm.BinaryData)
	}
	return fmt.Sprintf("SHA256:%x", sha256.Sum256([]byte(dataAsString)))
}
