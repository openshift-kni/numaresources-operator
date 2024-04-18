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

package images

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type Data struct {
	User    string // dynamic, learned from the user
	Self    string // dynamic, learned from the cluster
	Builtin string // static, determined at built time
}

// Discovered returns the image Spec learned from the environment (dynamic)
func (da Data) Discovered() string {
	if da.User != "" {
		return da.User
	}
	return da.Self
}

// Preferred returns the image spec to use, preferring the dynamic
func (da Data) Preferred() string {
	if dyn := da.Discovered(); dyn != "" {
		return dyn
	}
	return da.Builtin
}

func Discover(ctx context.Context, userImageSpec string) (Data, corev1.PullPolicy) {
	imageSpec, pullPolicy, err := GetCurrentImage(ctx)
	if err != nil {
		// intentionally continue
		klog.InfoS("unable to find current image, using hardcoded", "error", err)
	}
	data := Data{
		User:    userImageSpec,
		Self:    imageSpec,
		Builtin: SpecPath(),
	}
	klog.InfoS("discovered RTE image", "user", data.User, "self", data.Self, "builtin", data.Builtin, "preferred", data.Preferred())
	return data, pullPolicy
}
