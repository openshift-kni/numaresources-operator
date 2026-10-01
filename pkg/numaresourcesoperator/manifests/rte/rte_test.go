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
 * Copyright 2021 Red Hat, Inc.
 */

package rte

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/manifests"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/options"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/platform"
)

func renderingCases() []struct {
	name string
	opts options.Render
} {
	return []struct {
		name string
		opts options.Render
	}{
		{"kubernetes", options.Render{Platform: platform.Kubernetes, Namespace: "test"}},
		{"openshift-legacy", options.Render{Platform: platform.OpenShift, PlatformVersion: "v4.17", Namespace: "test", CustomSELinuxPolicy: true, EnableCRIHooks: true}},
		{"openshift-current", options.Render{Platform: platform.OpenShift, PlatformVersion: "v4.18", Namespace: "test", CustomSELinuxPolicy: true}},
		{"openshift-builtin", options.Render{Platform: platform.OpenShift, PlatformVersion: "v4.18", Namespace: "test"}},
		{"hypershift", options.Render{Platform: platform.HyperShift, PlatformVersion: "v4.18", Namespace: "test", CustomSELinuxPolicy: true}},
	}
}

func renderingOptions() options.UpdaterDaemon {
	return options.UpdaterDaemon{
		Name: "test-rte", Namespace: "rendered", ConfigData: "topologyManagerPolicy: single-numa-node\n",
		MachineConfigPoolSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "worker"}},
		DaemonSet: options.DaemonSet{
			Verbose: 2, PullIfNotPresent: true, PFPEnable: true, NotificationEnable: true,
			NodeSelector:   &metav1.LabelSelector{MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""}},
			UpdateInterval: 10 * time.Second, SCCVersion: options.SCCV1,
		},
	}
}

func serializeObjects(t *testing.T, objects []client.Object) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, obj := range objects {
		buf.WriteString("---\n")
		if err := manifests.SerializeObject(obj, &buf); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func TestRenderManifests(t *testing.T) {
	t.Setenv("TAS_IMAGES_USE_SHA", "")
	t.Setenv("TAS_RESOURCE_EXPORTER_IMAGE", "test/rte:copy")
	for _, tc := range renderingCases() {
		t.Run(tc.name, func(t *testing.T) {
			mf, err := NewWithOptions(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(mf, mf.Clone()) {
				t.Fatal("clone differs from the original")
			}
			original := serializeObjects(t, mf.ToObjects())
			rendered, err := mf.Render(renderingOptions())
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", tc.name+".yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if got := serializeObjects(t, rendered.ToObjects()); !bytes.Equal(got, want) {
				t.Fatalf("rendered manifests differ from the expected fixture:\n%s", got)
			}
			rendered.DaemonSet.Spec.Template.Spec.Containers[0].Args[0] = "changed"
			if rendered.MachineConfig != nil {
				rendered.MachineConfig.Spec.Config.Raw[0] = ' '
			}
			if !bytes.Equal(original, serializeObjects(t, mf.ToObjects())) {
				t.Fatal("rendering or modifying a clone changed the original manifests")
			}
		})
	}
}
