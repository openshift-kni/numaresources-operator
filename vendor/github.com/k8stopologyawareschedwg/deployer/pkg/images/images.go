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
	Setup(os.LookupEnv)
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
	SchedulerPluginControllerImage = SchedulerPluginSchedulerDefaultImageTag
	ResourceTopologyExporterImage  = ResourceTopologyExporterDefaultImageTag
	NodeFeatureDiscoveryImage      = NodeFeatureDiscoveryDefaultImageTag
)
