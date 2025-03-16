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
	"crypto/sha256"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// hash package purpose is to compute a ConfigMap hash
// that will be attached as an annotation to workload resources (DaemonSet, Deployment, etc.)
//  to allow them to track ConfigMap changes
// more about this technique here: https://blog.questionable.services/article/kubernetes-deployments-configmap-change/

const ConfigMapAnnotation = "configmap.hash"

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
