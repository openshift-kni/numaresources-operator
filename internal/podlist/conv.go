/*
 * Copyright 2023 Red Hat, Inc.
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

package podlist

import corev1 "k8s.io/api/core/v1"

// ToPods bridges packages podlist and wait.
// podlist package reasons in terms of corev1.PodList{}.Items: []corev1.Pod
// wait package reasons in terms of []*corev1.Pod (note the pointer)
func ToPods(pods []corev1.Pod) []*corev1.Pod {
	ret := make([]*corev1.Pod, 0, len(pods))
	for idx := range pods {
		ret = append(ret, &pods[idx])
	}
	return ret
}
