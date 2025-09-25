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
	"testing"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

func TestDecodeMinimumVersion(t *testing.T) {
	nightly, _ := platform.ParseVersion("4.17.0-0.nightly-2025-08-04-12")
	ci, _ := platform.ParseVersion("4.17.0-0.ci-2025-08-04-12")
	stable, _ := platform.ParseVersion("4.17.0")
	unrecognized, _ := platform.ParseVersion("4.17.0-unknown")

	tests := []struct {
		name                 string
		version              platform.Version
		leastSupportExpected string
		shouldBeUnrecognized bool
	}{
		{
			name:                 "nightly",
			version:              nightly,
			leastSupportExpected: NightlySupportSince,
		},
		{
			name:                 "stable",
			version:              stable,
			leastSupportExpected: StableSupportSince,
		},
		{
			name:                 "ci",
			version:              ci,
			leastSupportExpected: CISupportSince,
		},
		{
			name:                 "unrecognized",
			version:              unrecognized,
			leastSupportExpected: "",
			shouldBeUnrecognized: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeMinimumVersion(tt.version)
			if tt.shouldBeUnrecognized {
				if got != "" {
					t.Fatalf("expected unrecognized, but got %q", tt.version)
				}
				return
			}

			if !tt.shouldBeUnrecognized && got == "" {
				t.Fatalf("unexpected unrecognized version: got %v from %v", got, tt.version)
			}

			if got != tt.leastSupportExpected {
				t.Errorf("DecodeMinimumVersion() got = %v, want %v", got, tt.leastSupportExpected)
			}
		})
	}
}

func TestIsVersionEnoughForPodresourcesListFilterActivePods(t *testing.T) {
	nightlyGreater, _ := platform.ParseVersion("4.17.0-0.nightly-2025-09-29-154810")
	ciGreater, _ := platform.ParseVersion("4.17.0-0.ci-2025-10-00-000000")
	stableGreater, _ := platform.ParseVersion("4.17.41")

	unsupportedNightly, _ := platform.ParseVersion("4.17.0-0.nightly-2025-08-04-150000")
	unsupportedStable, _ := platform.ParseVersion("4.17.0")

	tests := []struct {
		name    string
		platf   platform.Platform
		version platform.Version
		want    bool
	}{
		{
			name: "empty",
			want: false,
		},
		{
			name:    "nightly - least supported",
			platf:   platform.OpenShift,
			version: NightlySupportSince,
			want:    true,
		},
		{
			name:    "nightly - greater than least supported",
			platf:   platform.OpenShift,
			version: nightlyGreater,
			want:    true,
		},
		{
			name:    "nightly - unsupported",
			platf:   platform.OpenShift,
			version: unsupportedNightly,
			want:    false,
		},
		{
			name:    "stable - least supported",
			platf:   platform.OpenShift,
			version: StableSupportSince,
			want:    true,
		},
		{
			name:    "stable - greater than least supported",
			platf:   platform.OpenShift,
			version: stableGreater,
			want:    true,
		},
		{
			name:    "stable - unsupported",
			platf:   platform.OpenShift,
			version: unsupportedStable,
			want:    false,
		},
		{
			name:    "CI - least supported",
			platf:   platform.OpenShift,
			version: CISupportSince,
			want:    true,
		},
		{
			name:    "CI - greater than least supported",
			platf:   platform.OpenShift,
			version: ciGreater,
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isVersionEnoughForPodresourcesListFilterActivePods(tt.platf, tt.version); got != tt.want {
				t.Errorf("isVersionEnoughForPodresourcesListFilterActivePods() = %v, want %v", got, tt.want)
			}
		})
	}
}
