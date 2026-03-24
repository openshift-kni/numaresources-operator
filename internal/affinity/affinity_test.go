/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package affinity

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetPodAntiAffinity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		labels  map[string]string
		want    *corev1.PodAntiAffinity
		wantErr error
	}{
		{
			name:    "nil labels",
			labels:  nil,
			wantErr: ErrNoPodTemplateLabels,
		},
		{
			name:    "empty labels",
			labels:  map[string]string{},
			wantErr: ErrNoPodTemplateLabels,
		},
		{
			name: "multiple labels",
			labels: map[string]string{
				"app":                          "secondary-scheduler",
				"pod-template-hash":            "abc123",
				"openshift.io/deployment.name": "secondary-scheduler",
			},
			want: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app":                          "secondary-scheduler",
								"pod-template-hash":            "abc123",
								"openshift.io/deployment.name": "secondary-scheduler",
							},
						},
						TopologyKey: "kubernetes.io/hostname",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := GetPodAntiAffinity(tt.labels)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error: want %v, got %v", tt.wantErr, err)
				}
				if got != nil {
					t.Fatalf("expected nil PodAntiAffinity on error, got %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatal("GetPodAntiAffinity returned nil PodAntiAffinity")
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("GetPodAntiAffinity mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
