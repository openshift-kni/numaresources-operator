/*
 * Copyright 2025 Red Hat, Inc.
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

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestToPods(t *testing.T) {
	sourcePods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				UID: types.UID("aaa-000"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				UID: types.UID("aaa-001"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				UID: types.UID("aaa-002"),
			},
		},
	}
	pods := ToPods(sourcePods)

	for idx, pod := range pods {
		if pod != &sourcePods[idx] {
			t.Errorf("pod mismatch: idx=%v pod=%s", idx, string(pod.UID))
		}
	}
}
