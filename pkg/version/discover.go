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
 * Copyright 2022 Red Hat, Inc.
 */

package version

import (
	"context"

	"k8s.io/klog/v2"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform/detect"
)

// DiscoverResult is the result of the cluster discovery process of platform and version.
// It stores the platform, the short version (minimized) and the long version (full).
// the short version is the version without the build metadata, and is the one for
// general use. For more specific use cases concerning to specific Kubernetes versions,
// the long version should be used.
type DiscoverResult struct {
	Platform     platform.Platform
	ShortVersion platform.Version
	LongVersion  platform.Version
}

func newDefaultClusterResult() DiscoverResult {
	return DiscoverResult{
		Platform:     platform.Unknown,
		ShortVersion: platform.MissingVersion,
		LongVersion:  platform.MissingVersion,
	}
}

func DiscoverCluster(ctx context.Context, platformName, platformVersion string) (DiscoverResult, error) {
	result := newDefaultClusterResult()
	// if it is unknown, it's fine
	userPlatform, _ := platform.ParsePlatform(platformName)
	userPlatformVersion, _ := platform.ParseVersion(platformVersion)

	plat, reason, err := detect.FindPlatform(ctx, userPlatform)
	klog.InfoS("platform detection", "kind", plat.Discovered, "reason", reason)
	clusterPlatform := plat.Discovered
	if clusterPlatform == platform.Unknown {
		klog.ErrorS(err, "cannot autodetect the platform, and no platform given")
		return newDefaultClusterResult(), err
	}

	result.Platform = clusterPlatform
	platVersion, source, err := detect.FindVersion(ctx, clusterPlatform, userPlatformVersion)
	klog.InfoS("platform detection", "version", platVersion.Discovered, "reason", source)
	result.LongVersion = platVersion.Discovered
	result.ShortVersion = Minimize(platVersion.Discovered)
	if result.LongVersion == platform.MissingVersion {
		klog.ErrorS(err, "cannot autodetect the platform version, and no platform given")
		return result, err
	}

	klog.InfoS("detected cluster", "platform", result.Platform, "shortVersion", result.ShortVersion, "longVersion", result.LongVersion)

	return result, nil
}
