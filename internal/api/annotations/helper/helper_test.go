/*
Copyright 2025.

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

package helper

import (
	"testing"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	anns "github.com/openshift-kni/numaresources-operator/internal/api/annotations"
)

func TestIsCustomPolicyEnabled(t *testing.T) {
	testcases := []struct {
		description string
		nodeGroups  []nropv1.NodeGroup
		expected    bool
	}{
		{
			description: "empty maps - single",
			nodeGroups: []nropv1.NodeGroup{
				{
					Annotations: make(map[string]string),
				},
			},
			expected: false,
		},
		{
			description: "empty maps - multi",
			nodeGroups: []nropv1.NodeGroup{
				{
					Annotations: make(map[string]string),
				},
				{
					Annotations: make(map[string]string),
				},
				{
					Annotations: make(map[string]string),
				},
			},
			expected: false,
		},
		{
			description: "annotation set but not to anything except \"custom\" value means the default - single",
			nodeGroups: []nropv1.NodeGroup{
				{
					Annotations: map[string]string{
						anns.SELinuxPolicyConfigAnnotation: "true",
					},
				},
			},
			expected: false,
		},
		{
			description: "annotation set but not to anything except \"custom\" value means the default - multi",
			nodeGroups: []nropv1.NodeGroup{
				{
					Annotations: map[string]string{
						anns.SELinuxPolicyConfigAnnotation: "true",
					},
				},
				{
					Annotations: make(map[string]string),
				},
				{
					Annotations: make(map[string]string),
				},
			},
			expected: false,
		},
		{
			description: "enabled custom policy - single",
			nodeGroups: []nropv1.NodeGroup{
				{
					Annotations: map[string]string{
						anns.SELinuxPolicyConfigAnnotation: "custom",
					},
				},
			},
			expected: true,
		},
		{
			description: "enabled custom policy - multi",
			nodeGroups: []nropv1.NodeGroup{
				{
					Annotations: make(map[string]string),
				},
				{
					Annotations: map[string]string{
						anns.SELinuxPolicyConfigAnnotation: "custom",
					},
				},
				{
					Annotations: make(map[string]string),
				},
			},
			expected: true,
		},
		{
			description: "enabled custom policy - multi + conflict",
			nodeGroups: []nropv1.NodeGroup{
				{
					Annotations: map[string]string{
						anns.SELinuxPolicyConfigAnnotation: "enabled",
					},
				},
				{
					Annotations: map[string]string{
						anns.SELinuxPolicyConfigAnnotation: "custom",
					},
				},
				{
					Annotations: make(map[string]string),
				},
			},
			expected: true,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.description, func(t *testing.T) {
			nropObj := nropv1.NUMAResourcesOperator{}
			nropObj.Spec.NodeGroups = tc.nodeGroups
			if got := IsCustomPolicyEnabled(&nropObj); got != tc.expected {
				t.Errorf("expected %v got %v", tc.expected, got)
			}
		})
	}
}
