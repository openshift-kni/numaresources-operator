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

func DiscoverCluster(ctx context.Context, platformName, platformVersion string) (platform.Platform, platform.Version, error) {
	// if it is unknown, it's fine
	userPlatform, _ := platform.ParsePlatform(platformName)
	userPlatformVersion, _ := platform.ParseVersion(platformVersion)

	plat, reason, err := detect.FindPlatform(ctx, userPlatform)
	klog.InfoS("platform detection", "kind", plat.Discovered, "reason", reason)
	clusterPlatform := plat.Discovered
	if clusterPlatform == platform.Unknown {
		klog.ErrorS(err, "cannot autodetect the platform, and no platform given")
		return clusterPlatform, "", err
	}

	platVersion, source, err := detect.FindVersion(ctx, clusterPlatform, userPlatformVersion)
	klog.InfoS("platform detection", "version", platVersion.Discovered, "reason", source)
	clusterPlatformVersion := Minimize(platVersion.Discovered)
	if clusterPlatformVersion == platform.MissingVersion {
		klog.ErrorS(err, "cannot autodetect the platform version, and no platform given")
		return clusterPlatform, clusterPlatformVersion, err
	}

	klog.InfoS("detected cluster", "platform", clusterPlatform, "version", clusterPlatformVersion)

	return clusterPlatform, clusterPlatformVersion, nil
}
