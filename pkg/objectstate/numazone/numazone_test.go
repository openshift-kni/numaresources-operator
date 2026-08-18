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

package numazone

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
)

func TestDesiredManifests(t *testing.T) {
	nodeSelector := map[string]string{"node-role.kubernetes.io/worker": ""}
	mf := DesiredManifests("test-ns", "test-numazone", "quay.io/test:latest", corev1.PullIfNotPresent, nodeSelector)

	if mf.ServiceAccount == nil {
		t.Fatal("ServiceAccount should not be nil")
	}
	if mf.ServiceAccount.Name != "test-numazone-sa" {
		t.Errorf("ServiceAccount name: got %q, want %q", mf.ServiceAccount.Name, "test-numazone-sa")
	}
	if mf.ServiceAccount.Namespace != "test-ns" {
		t.Errorf("ServiceAccount namespace: got %q, want %q", mf.ServiceAccount.Namespace, "test-ns")
	}

	if mf.Role == nil {
		t.Fatal("Role should not be nil")
	}
	if mf.Role.Name != "test-numazone-ro" {
		t.Errorf("Role name: got %q, want %q", mf.Role.Name, "test-numazone-ro")
	}

	if mf.RoleBinding == nil {
		t.Fatal("RoleBinding should not be nil")
	}
	if mf.RoleBinding.Name != "test-numazone-rb" {
		t.Errorf("RoleBinding name: got %q, want %q", mf.RoleBinding.Name, "test-numazone-rb")
	}

	if mf.DaemonSet == nil {
		t.Fatal("DaemonSet should not be nil")
	}
	if mf.DaemonSet.Name != "test-numazone-ds" {
		t.Errorf("DaemonSet name: got %q, want %q", mf.DaemonSet.Name, "test-numazone-ds")
	}

	container := mf.DaemonSet.Spec.Template.Spec.Containers[0]
	if container.Image != "quay.io/test:latest" {
		t.Errorf("container image: got %q, want %q", container.Image, "quay.io/test:latest")
	}
	if container.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Errorf("pull policy: got %v, want %v", container.ImagePullPolicy, corev1.PullIfNotPresent)
	}

	foundDevices := false
	for i, arg := range container.Args {
		if arg == "--devices" && i+1 < len(container.Args) {
			if container.Args[i+1] != "15" {
				t.Errorf("devices arg: got %q, want %q", container.Args[i+1], "15")
			}
			foundDevices = true
			break
		}
	}
	if !foundDevices {
		t.Errorf("--devices flag not found in args: %v", container.Args)
	}

	dsNodeSelector := mf.DaemonSet.Spec.Template.Spec.NodeSelector
	if dsNodeSelector == nil {
		t.Fatal("DaemonSet nodeSelector should not be nil")
	}
	if dsNodeSelector["node-role.kubernetes.io/worker"] != "" {
		t.Errorf("DaemonSet nodeSelector: got %v, want worker label", dsNodeSelector)
	}
}

func TestComponentNameFor(t *testing.T) {
	got := ComponentNameFor("numaresourcesoperator", "worker")
	want := "numaresourcesoperator-worker-numazone-dp"
	if got != want {
		t.Errorf("ComponentNameFor: got %q, want %q", got, want)
	}
}

func TestIsEnabled(t *testing.T) {
	enabled := nropv1.NUMAAwareDevicePluginEnabled
	disabled := nropv1.NUMAAwareDevicePluginDisabled

	tests := []struct {
		name   string
		config *nropv1.NodeGroupConfig
		want   bool
	}{
		{
			name:   "nil config",
			config: nil,
			want:   false,
		},
		{
			name:   "nil field",
			config: &nropv1.NodeGroupConfig{},
			want:   false,
		},
		{
			name: "disabled",
			config: &nropv1.NodeGroupConfig{
				NUMAAwareDevicePlugin: &disabled,
			},
			want: false,
		},
		{
			name: "enabled",
			config: &nropv1.NodeGroupConfig{
				NUMAAwareDevicePlugin: &enabled,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsEnabled(tt.config)
			if got != tt.want {
				t.Errorf("IsEnabled: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestState(t *testing.T) {
	desired := DesiredManifests("test-ns", "test-numazone", "quay.io/test:latest", corev1.PullAlways, nil)
	em := ExistingManifests{}

	states := em.State(desired)
	if len(states) != 4 {
		t.Fatalf("State: got %d objects, want 4", len(states))
	}

	for i, s := range states {
		if s.Desired == nil {
			t.Errorf("State[%d]: Desired should not be nil", i)
		}
		if s.Compare == nil {
			t.Errorf("State[%d]: Compare should not be nil", i)
		}
		if s.Merge == nil {
			t.Errorf("State[%d]: Merge should not be nil", i)
		}
	}
}

func TestDeletionState(t *testing.T) {
	t.Run("empty returns nil", func(t *testing.T) {
		em := ExistingManifests{}
		states := em.DeletionState()
		if len(states) != 0 {
			t.Errorf("DeletionState: got %d, want 0", len(states))
		}
	})

	t.Run("returns states with nil Desired for existing objects", func(t *testing.T) {
		em := ExistingManifests{
			existing: Manifests{
				ServiceAccount: &corev1.ServiceAccount{},
			},
		}
		states := em.DeletionState()
		if len(states) != 1 {
			t.Fatalf("DeletionState: got %d, want 1", len(states))
		}
		if states[0].Existing == nil {
			t.Error("DeletionState[0]: Existing should not be nil")
		}
		if states[0].IsCreateOrUpdate() {
			t.Error("DeletionState[0]: should not be create-or-update (Desired must be nil)")
		}
	})
}
