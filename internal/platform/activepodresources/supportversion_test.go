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

package activepodresources

import (
	"fmt"
	"testing"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

func TestGetLeastSupportedByBuildType(t *testing.T) {
	nightly, _ := platform.ParseVersion("4.20.0-0.nightly-2025-08-04-12")
	konflux, _ := platform.ParseVersion("4.20.0-0.konflux-nightly-2025-08-04-12")
	ci, _ := platform.ParseVersion("4.20.0-0.ci-2025-08-04-12")
	ec, _ := platform.ParseVersion("4.20.0-ec.2")
	rc, _ := platform.ParseVersion("4.20.0-rc.2")
	stable, _ := platform.ParseVersion("4.20.0")
	unrecognized, _ := platform.ParseVersion("4.20.0-unknown")

	tests := []struct {
		name                 string
		version              platform.Version
		leastSupportExpected string
		err                  error
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
			name:                 "rc",
			version:              rc,
			leastSupportExpected: ReleaseCandidateSupportSince,
		},
		{
			name:                 "ec",
			version:              ec,
			leastSupportExpected: DevPreviewSupportSince,
		},
		{
			name:                 "konflux",
			version:              konflux,
			leastSupportExpected: KonfluxNightlySupportSince,
		},
		{
			name:                 "unrecognized",
			version:              unrecognized,
			leastSupportExpected: "",
			err:                  fmt.Errorf("version form %s is unrecognized", unrecognized),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetLeastSupportedByBuildType(tt.version)
			if tt.err != nil {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				if err.Error() != tt.err.Error() {
					t.Fatalf("mismatching error string: error = %v, expected = %v", err, tt.err)
				}
				return
			}

			if tt.err == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.leastSupportExpected {
				t.Errorf("GetLeastSupportedByBuildType() got = %v, want %v", got, tt.leastSupportExpected)
			}
		})
	}
}

func TestIsVersionFixed(t *testing.T) {
	nightlyGreater, _ := platform.ParseVersion("4.20.0-0.nightly-2025-08-04-154810")
	k5xNightlyGreater, _ := platform.ParseVersion("4.20.0-0.konflux-nightly-2025-10-00-000000")
	ciGreater, _ := platform.ParseVersion("4.20.0-0.ci-2025-10-00-000000")
	stableGreater, _ := platform.ParseVersion("4.20.1")
	ecGreater, _ := platform.ParseVersion("4.20.0-ec.2")
	rcGreater, _ := platform.ParseVersion("4.20.0-rc.2")

	unsupportedNightly, _ := platform.ParseVersion("4.20.0-0.nightly-2024-08-04-150000")
	unsupportedStable, _ := platform.ParseVersion("4.19.0")

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
			name:    "konflux nightly - least supported",
			platf:   platform.OpenShift,
			version: KonfluxNightlySupportSince,
			want:    true,
		},
		{
			name:    "konflux nightly - greater than least supported",
			platf:   platform.HyperShift,
			version: k5xNightlyGreater,
			want:    true,
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
		{
			name:    "dev-preview - least supported",
			platf:   platform.OpenShift,
			version: DevPreviewSupportSince,
			want:    true,
		},
		{
			name:    "dev-preview - greater than least supported",
			platf:   platform.OpenShift,
			version: ecGreater,
			want:    true,
		},
		{
			name:    "release candidate - least supported",
			platf:   platform.OpenShift,
			version: ReleaseCandidateSupportSince,
			want:    true,
		},
		{
			name:    "release candidate - greater than least supported",
			platf:   platform.OpenShift,
			version: rcGreater,
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsVersionFixed(tt.platf, tt.version); got != tt.want {
				t.Errorf("IsVersionFixed() = %v, want %v", got, tt.want)
			}
		})
	}
}
