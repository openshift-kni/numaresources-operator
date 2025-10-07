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
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

// PlatformProperties represents the platform capabilities we have to infer and which are not explicitly
// advertised from the platform using standard APIs.
type PlatformProperties struct {
	PodResourcesListFilterActivePods bool
}

type PlatformInfo struct {
	Platform   platform.Platform
	Version    platform.Version
	Properties PlatformProperties
}

func New(plat platform.Platform, ver platform.Version) PlatformInfo {
	info := PlatformInfo{
		Platform: plat,
		Version:  ver,
	}
	discoverProperties(&info)
	return info
}

func discoverProperties(info *PlatformInfo) {
	info.Properties.PodResourcesListFilterActivePods = isVersionEnoughForPodresourcesListFilterActivePods(info.Platform, info.Version)
}
