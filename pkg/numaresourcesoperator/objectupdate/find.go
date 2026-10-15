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
 * Copyright 2023 Red Hat, Inc.
 */

package objectupdate

import (
	corev1 "k8s.io/api/core/v1"
)

func FindContainerByName(conts []corev1.Container, name string) *corev1.Container {
	for idx := range conts {
		cont := &conts[idx]
		if cont.Name == name {
			return cont
		}
	}
	return nil
}

func FindContainerEnvVarByName(envs []corev1.EnvVar, name string) *corev1.EnvVar {
	for idx := range envs {
		env := &envs[idx]
		if env.Name == name {
			return env
		}
	}
	return nil
}

func FindContainerPortByName(ports []corev1.ContainerPort, name string) *corev1.ContainerPort {
	for idx := range ports {
		cp := &ports[idx]
		if cp.Name == name {
			return cp
		}
	}
	return nil
}
