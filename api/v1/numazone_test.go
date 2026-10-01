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

package v1

import (
	"encoding/json"
	"testing"

	"k8s.io/utils/ptr"
)

func TestNormalizeNumazoneConfig(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config *NumazoneConfig
		want   NumazoneMode
	}{
		{name: "omitted", want: NumazoneDisabled},
		{name: "empty", config: &NumazoneConfig{}, want: NumazoneDisabled},
		{name: "disabled", config: &NumazoneConfig{Mode: ptr.To(NumazoneDisabled)}, want: NumazoneDisabled},
		{name: "enabled", config: &NumazoneConfig{Mode: ptr.To(NumazoneEnabled)}, want: NumazoneEnabled},
		{name: "passthrough", config: &NumazoneConfig{Mode: ptr.To(NumazonePassthrough)}, want: NumazonePassthrough},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := NodeGroup{Numazone: tc.config}
			conf := group.NormalizeNumazoneConfig()
			if conf.Mode == nil || *conf.Mode != tc.want {
				t.Fatalf("normalized mode: got %v, want %s", conf.Mode, tc.want)
			}
			*conf.Mode = NumazoneMode("changed")
			if tc.config != nil && tc.config.Mode != nil && *tc.config.Mode != tc.want {
				t.Fatal("normalization changed the spec")
			}
			if tc.config != nil && tc.name == "empty" && tc.config.Mode != nil {
				t.Fatal("normalization defaulted the original spec")
			}
		})
	}
}

func TestNumazoneNodeGroupStatus(t *testing.T) {
	status := NodeGroupStatus{NumazoneDaemonSet: &NamespacedName{Namespace: "test", Name: "numazone-worker"}}
	cloned := status.DeepCopy()
	cloned.NumazoneDaemonSet.Name = "changed"
	if status.NumazoneDaemonSet.Name != "numazone-worker" {
		t.Fatal("deep copy shares the numazone reference")
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["numazoneDaemonSet"]; !ok {
		t.Fatal("missing numazone daemonset status")
	}
}
