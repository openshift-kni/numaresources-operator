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

package numazone

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/openshift-kni/numaresources-operator/pkg/numazoneresource"
)

func TestEnforcesAdmission(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"default", nil, true},
		{"enforcing", []string{"--mode=enforcing"}, true},
		{"passthrough", []string{"--mode=passthrough"}, false},
		{"passthrough separate arg", []string{"-mode", "passthrough"}, false},
		{"sync disabled", []string{"--admission-sync=false"}, false},
		{"sync disabled separate arg", []string{"-admission-sync", "false"}, false},
		{"last flag wins", []string{"--mode=passthrough", "--mode=enforcing"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ds := &appsv1.DaemonSet{Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: numazoneresource.ContainerName, Args: tc.args}}},
			}}}
			if got := enforcesAdmission(ds.Spec.Template.Spec.Containers); got != tc.want {
				t.Fatalf("enforcesAdmission=%v, want %v", got, tc.want)
			}
		})
	}
}
