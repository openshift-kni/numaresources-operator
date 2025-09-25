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
	// (e.i CI) that produce the build after the specific nightly build date will certainly
	// have the fix, thus we need to track them separately.
	// releases that are already have their first build GA do not have dev-preview or release-candidate builds
	StableSupportSince  = "4.17.40"
	NightlySupportSince = "4.17.0-0.nightly-2025-09-09-150403" // *** according to OCP verification:wq
	CISupportSince      = "4.17.0-0.ci-2025-09-10-053550"      // *** the earliest that was found available at time of writing this
)

func decodeMinimumVersion(version platform.Version) string {
	v := version.String()
	if strings.Contains(v, ".nightly-") {
		return NightlySupportSince
	}
	if strings.Contains(v, ".ci-") {
		return CISupportSince
	}
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
