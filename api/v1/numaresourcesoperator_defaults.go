/*
 * Copyright 2023 Red Hat, Inc.
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

package v1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func DefaultNodeGroupConfig() NodeGroupConfig {
	ngc := NodeGroupConfig{}
	ngc.SetDefaults()
	return ngc
}

func (ngc *NodeGroupConfig) SetDefaults() {
	if ngc.PodsFingerprinting == nil {
		ngc.PodsFingerprinting = defaultPodsFingerprinting()
	}
	if ngc.InfoRefreshPeriod == nil {
		ngc.InfoRefreshPeriod = defaultInfoRefreshPeriod()
	}
	if ngc.InfoRefreshMode == nil {
		ngc.InfoRefreshMode = defaultInfoRefreshMode()
	}
	if ngc.InfoRefreshPause == nil {
		ngc.InfoRefreshPause = defaultInfoRefreshPause()
	}
	if ngc.NumaPlacement == nil {
		ngc.NumaPlacement = defaultNumaPlacement()
	}
}

func defaultPodsFingerprinting() *PodsFingerprintingMode {
	return ptr.To(PodsFingerprintingEnabledExclusiveResources)
}

func defaultInfoRefreshMode() *InfoRefreshMode {
	return ptr.To(InfoRefreshPeriodic)
}

func defaultInfoRefreshPeriod() *metav1.Duration {
	return ptr.To(metav1.Duration{
		Duration: 10 * time.Second,
	})
}

func defaultInfoRefreshPause() *InfoRefreshPauseMode {
	return ptr.To(InfoRefreshPauseDisabled)
}

func defaultNumaPlacement() *NumaPlacementMode {
	return ptr.To(NumaPlacementEnabled)
}
