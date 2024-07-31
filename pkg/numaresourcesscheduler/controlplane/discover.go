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
 * Copyright 2021 Red Hat, Inc.
 */

package controlplane

import (
	"context"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform/detect"
	"k8s.io/klog/v2"
)

func Defaults() detect.ControlPlaneInfo {
	return detect.ControlPlaneInfo{
		NodeCount: 1, // TODO
	}
}

func Discover(ctx context.Context) detect.ControlPlaneInfo {
	info, err := detect.ControlPlane(ctx)
	if err != nil {
		klog.InfoS("cannot autodetect control plane and scheduler replica count", "err", err)
		return Defaults()
	}

	klog.InfoS("autodetected control plane nodes", "suggestedSchedulerReplicas", info.NodeCount)
	return info
}
