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
	"strings"

	"k8s.io/klog/v2"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

// This file processes the known OCP platform versions, so far nightly, konflux-nightly, CI, dev-preview, and RC

// supported version for the podresources List API fix.
// see: https://issues.redhat.com/browse/OCPBUGS-56785
// see: https://github.com/kubernetes/kubernetes/pull/132028
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

func decodeMinimumVersion(version platform.Version) string {
	v := version.String()
	// Nightly; example: 4.20.0-0.nightly-2025-08-04-154809
	if strings.Contains(v, ".nightly-") {
		return NightlySupportSince
	}
	// K5x nightly; example: 4.19.0-0.konflux-nightly-2025-08-05-060813
	if strings.Contains(v, ".konflux-nightly-") {
		return KonfluxNightlySupportSince
	}
	// CI; example: 4.19.0-0.ci-2025-08-05-050737
	if strings.Contains(v, ".ci-") {
		return CISupportSince
	}
	// DevPreview; example: 4.20.0-ec.5
	if strings.Contains(v, "-ec.") {
		return DevPreviewSupportSince
	}
	// Release Candidate: example: 4.20.0-rc.1
	if strings.Contains(v, "-rc.") {
		return ReleaseCandidateSupportSince
	}
	// Stable: example: 4.20.1
	if !strings.Contains(v, "-") {
		return StableSupportSince
	}
	return ""
}

func isVersionEnoughForPodresourcesListFilterActivePods(platf platform.Platform, version platform.Version) bool {
	if platf != platform.OpenShift && platf != platform.HyperShift {
		return false
	}

	minVer := decodeMinimumVersion(version)
	if minVer == "" {
		// should never happen, so we are loud about it
		klog.Infof("unrecognized version %q", version)
		return false
	}

	parsedVersion, err := platform.ParseVersion(minVer)
	if err != nil {
		// should never happen, so we are loud about it
		klog.Infof("failed to parse version %q: %v", minVer, err)
		return false
	}

	ok, err := version.AtLeast(parsedVersion)
	if err != nil {
		klog.Infof("failed to compare version %v with %v, err %v", parsedVersion, version, err)
		return false
	}

	return ok
}
