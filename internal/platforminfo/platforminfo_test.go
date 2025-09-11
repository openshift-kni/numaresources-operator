/*
 * Copyright 2025 Red Hat, Inc.
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

package platforminfo

import (
	"reflect"
	"testing"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

func TestNewWithDiscover(t *testing.T) {
	testcases := []struct {
		description string
		plat        platform.Platform
		ver         platform.Version
		expected    PlatformProperties
	}{
		{
			description: "empty",
		},
		{
			description: "last major unfixed",
			plat:        platform.OpenShift,
			ver:         mustParseVersion("4.19.0"),
			expected: PlatformProperties{
				PodResourcesListFilterActivePods: false,
			},
		},
		{
			description: "first major fixed",
			plat:        platform.OpenShift,
			ver:         mustParseVersion("4.21.0"), // at time of writing
			expected: PlatformProperties{
				PodResourcesListFilterActivePods: true,
			},
		},
		{
			description: "next Z-stream fixed, must never regress", // at time of writing
			plat:        platform.OpenShift,
			ver:         mustParseVersion("4.21.1"),
			expected: PlatformProperties{
				PodResourcesListFilterActivePods: true,
			},
		},
		{
			description: "next major fixed, must never regress", // at time of writing
			plat:        platform.OpenShift,
			ver:         mustParseVersion("4.22.0"),
			expected: PlatformProperties{
				PodResourcesListFilterActivePods: true,
			},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.description, func(t *testing.T) {
			got := New(tc.plat, tc.ver)
			if !reflect.DeepEqual(got.Properties, tc.expected) {
				t.Errorf("expected %#v got %#v", tc.expected, got.Properties)
			}
		})
	}
}

func mustParseVersion(ver string) platform.Version {
	v, err := platform.ParseVersion(ver)
	if err != nil {
		panic(err)
	}
	return v
}
