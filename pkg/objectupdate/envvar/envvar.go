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
 * Copyright 2025 Red Hat, Inc.
 */

package envvar

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	PFPStatusDump = "PFP_STATUS_DUMP"

	PFPStatusDirDefault = "/run/pfpstatus"
)

func SetForContainer(cnt *corev1.Container, name, value string) {
	if env := FindByName(cnt.Env, name); env != nil {
		klog.V(2).InfoS("overriding existing environment variable", "name", name, "oldValue", env.Value, "newValue", value)
		env.Value = value
		return
	}

	cnt.Env = append(cnt.Env, corev1.EnvVar{
		Name:  name,
		Value: value,
	})
}

func DeleteFromContainer(cnt *corev1.Container, name string) {
	var envs []corev1.EnvVar
	for _, env := range cnt.Env {
		if env.Name == name {
			continue
		}
		envs = append(envs, env)
	}
	cnt.Env = envs
}

func FindByName(envs []corev1.EnvVar, name string) *corev1.EnvVar {
	for idx := range envs {
		env := &envs[idx]
		if env.Name == name {
			return env
		}
	}
	return nil
}
