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
	"strings"

	"k8s.io/klog/v2"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

// This file processes the known OCP platform versions, so far nightly, konflux-nightly, CI, dev-preview, and RC

const (
	// first OCP build to have the fix is always the nightly build. Yet that doesn't necessarily mean that builds of other lanes
	// (e.i CI, dev-preview) that produce the build after the specific nightly build date will certainly
	// have the fix, thus we need to track them separately
	StableSupportSince           = "4.20.0"
	NightlySupportSince          = "4.20.0-0.nightly-2025-08-04-154809"
	KonfluxNightlySupportSince   = "4.20.0-0.konflux-nightly-0000-00-00-000000"
	CISupportSince               = "4.20.0-0.ci-0000-00-00-000000"
	DevPreviewSupportSince       = "4.20.0-ec.0"
	ReleaseCandidateSupportSince = "4.20.0-rc.0"
)

// isStable checks if the version matches the stable build form and returns least supported stable version
func isStable(v string) (bool, string) {
	// example: 4.20.1
	if !strings.Contains(v, "-") {
		return true, StableSupportSince
	}
	return false, ""
}

// isReleaseCandidate checks if the version matches the RC build form and returns least supported RC version
func isReleaseCandidate(v string) (bool, string) {
	// example: 4.20.0-rc.1
	if strings.Contains(v, "-rc.") {
		return true, ReleaseCandidateSupportSince
	}
	return false, ""
}

// isNightly checks if the version matches the nightly build form and returns least supported nightly version
func isNightly(v string) (bool, string) {
	// example: 4.20.0-0.nightly-2025-08-04-154809
	if strings.Contains(v, ".nightly-") {
		return true, NightlySupportSince
	}
	return false, ""
}

// isKonfluxNightly checks if the version matches the konflux-nightly build form and returns least supported konflux-nightly version
func isKonfluxNightly(v string) (bool, string) {
	// example: 4.19.0-0.konflux-nightly-2025-08-05-060813
	if strings.Contains(v, ".konflux-nightly-") {
		return true, KonfluxNightlySupportSince
	}
	return false, ""
}

// isCI checks if the version matches the CI build form and returns the least supported CI version
func isCI(v string) (bool, string) {
	// example: 4.19.0-0.ci-2025-08-05-050737
	if strings.Contains(v, ".ci-") {
		return true, CISupportSince
	}
	return false, ""
}

// isDevPreview checks if the version matches the dev-preview build form and returns the least supported dev-preview version
func isDevPreview(v string) (bool, string) {
	// example: 4.20.0-ec.5
	if strings.Contains(v, "-ec.") {
		return true, DevPreviewSupportSince
	}
	return false, ""
}

func GetLeastSupportedByBuildType(version platform.Version) (string, error) {
	v := version.String()
	if ok, leastSupported := isStable(v); ok {
		return leastSupported, nil
	}
	if ok, leastSupported := isNightly(v); ok {
		return leastSupported, nil
	}

	if ok, leastSupported := isKonfluxNightly(v); ok {
		return leastSupported, nil
	}

	if ok, leastSupported := isCI(v); ok {
		return leastSupported, nil
	}

	if ok, leastSupported := isDevPreview(v); ok {
		return leastSupported, nil
	}

	if ok, leastSupported := isReleaseCandidate(v); ok {
		return leastSupported, nil
	}

	return "", fmt.Errorf("version form %s is unrecognized", v)
}

func IsVersionFixed(platf platform.Platform, version platform.Version) bool {
	if platf != platform.OpenShift && platf != platform.HyperShift {
		return false
	}

	leastSupported, err := GetLeastSupportedByBuildType(version)
	if err != nil {
		klog.Infof("failed to get least supported version, err %v", err)
		return false
	}

	parsedVersion, _ := platform.ParseVersion(leastSupported)
	ok, err := version.AtLeast(parsedVersion)
	if err != nil {
		klog.Infof("failed to compare version %v with %v, err %v", parsedVersion, version, err)
		return false
	}

	return ok
}
