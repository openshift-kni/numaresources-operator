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

package images

import "os"

func init() {
	_, ok := os.LookupEnv("TAS_IMAGES_USE_SHA")
	SetDefaults(ok)
	Setup(os.LookupEnv)
}

func SetDefaults(useSHA bool) {
	if useSHA {
		SchedulerPluginSchedulerImage = SchedulerPluginSchedulerDefaultImageSHA
		SchedulerPluginControllerImage = SchedulerPluginControllerDefaultImageSHA
		ResourceTopologyExporterImage = ResourceTopologyExporterDefaultImageSHA
		NodeFeatureDiscoveryImage = NodeFeatureDiscoveryDefaultImageSHA
	} else {
		SchedulerPluginSchedulerImage = SchedulerPluginSchedulerDefaultImageTag
		SchedulerPluginControllerImage = SchedulerPluginControllerDefaultImageTag
		ResourceTopologyExporterImage = ResourceTopologyExporterDefaultImageTag
		NodeFeatureDiscoveryImage = NodeFeatureDiscoveryDefaultImageTag
	}
}

func Setup(getImage func(string) (string, bool)) {
	if schedImage, ok := getImage("TAS_SCHEDULER_PLUGIN_IMAGE"); ok {
		SchedulerPluginSchedulerImage = schedImage
	}
	if schedCtrlImage, ok := getImage("TAS_SCHEDULER_PLUGIN_CONTROLLER_IMAGE"); ok {
		SchedulerPluginControllerImage = schedCtrlImage
	}
	if rteImage, ok := getImage("TAS_RESOURCE_EXPORTER_IMAGE"); ok {
		ResourceTopologyExporterImage = rteImage
	}
	if nfdImage, ok := getImage("TAS_NODE_FEATURE_DISCOVERY_IMAGE"); ok {
		NodeFeatureDiscoveryImage = nfdImage
	}
}

var (
	SchedulerPluginSchedulerImage  = SchedulerPluginSchedulerDefaultImageTag
	SchedulerPluginControllerImage = SchedulerPluginControllerDefaultImageTag
	ResourceTopologyExporterImage  = ResourceTopologyExporterDefaultImageTag
	NodeFeatureDiscoveryImage      = NodeFeatureDiscoveryDefaultImageTag
)
