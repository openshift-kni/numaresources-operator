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

package detect

import (
	"context"

	"github.com/go-logr/logr"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

func FindPlatform(ctx context.Context, userSupplied platform.Platform) (PlatformInfo, string, error) {
	env := platform.Environment{
		Environment: deployer.Environment{
			Ctx: ctx,
			Log: logr.Discard(),
		},
	}
	if err := env.EnsureClient(); err != nil {
		return PlatformInfo{}, DetectedFailure, err
	}
	return FindPlatformFromEnv(&env, userSupplied)
}

func FindVersion(ctx context.Context, plat platform.Platform, userSupplied platform.Version) (VersionInfo, string, error) {
	env := platform.Environment{
		Environment: deployer.Environment{
			Ctx: ctx,
			Log: logr.Discard(),
		},
	}
	if err := env.EnsureClient(); err != nil {
		return VersionInfo{}, DetectedFailure, err
	}
	return FindVersionFromEnv(&env, plat, userSupplied)
}

func FindPlatformFromEnv(env *platform.Environment, userSupplied platform.Platform) (PlatformInfo, string, error) {
	do := PlatformInfo{
		AutoDetected: platform.Unknown,
		UserSupplied: userSupplied,
		Discovered:   platform.Unknown,
	}
	if err := env.EnsureClient(); err != nil {
		return do, DetectedFailure, err
	}

	if do.UserSupplied != platform.Unknown {
		do.Discovered = do.UserSupplied
		return do, DetectedFromUser, nil
	}

	dp, err := PlatformFromEnv(env)
	if err != nil {
		return do, DetectedFailure, err
	}

	do.AutoDetected = dp
	do.Discovered = do.AutoDetected
	return do, DetectedFromCluster, nil
}

func FindVersionFromEnv(env *platform.Environment, plat platform.Platform, userSupplied platform.Version) (VersionInfo, string, error) {
	do := VersionInfo{
		AutoDetected: platform.MissingVersion,
		UserSupplied: userSupplied,
		Discovered:   platform.MissingVersion,
	}
	if err := env.EnsureClient(); err != nil {
		return do, DetectedFailure, err
	}

	if do.UserSupplied != platform.MissingVersion {
		do.Discovered = do.UserSupplied
		return do, DetectedFromUser, nil
	}

	dv, err := VersionFromEnv(env, plat)
	if err != nil {
		return do, DetectedFailure, err
	}

	do.AutoDetected = dv
	do.Discovered = do.AutoDetected
	return do, DetectedFromCluster, nil
}
