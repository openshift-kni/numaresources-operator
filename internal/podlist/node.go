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

package podlist

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (fnd Finder) OnNode(ctx context.Context, nodeName string) ([]corev1.Pod, error) {
	sel, err := fields.ParseSelector("spec.nodeName=" + nodeName)
	if err != nil {
		return nil, err
	}

	podList := &corev1.PodList{}
	err = fnd.List(ctx, podList, &client.ListOptions{FieldSelector: sel})
	return podList.Items, err
}
