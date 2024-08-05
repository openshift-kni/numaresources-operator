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

type Images struct {
	SchedulerPluginScheduler  string
	SchedulerPluginController string
	ResourceTopologyExporter  string
	NodeFeatureDiscovery      string
}

func Defaults(useSHA bool) Images {
	if useSHA {
		return Images{
			SchedulerPluginScheduler:  SchedulerPluginSchedulerDefaultImageSHA,
			SchedulerPluginController: SchedulerPluginControllerDefaultImageSHA,
			ResourceTopologyExporter:  ResourceTopologyExporterDefaultImageSHA,
			NodeFeatureDiscovery:      NodeFeatureDiscoveryDefaultImageSHA,
		}
	}
	return Images{
		SchedulerPluginScheduler:  SchedulerPluginSchedulerDefaultImageTag,
		SchedulerPluginController: SchedulerPluginControllerDefaultImageTag,
		ResourceTopologyExporter:  ResourceTopologyExporterDefaultImageTag,
		NodeFeatureDiscovery:      NodeFeatureDiscoveryDefaultImageTag,
	}
}

func Get() Images {
	_, ok := os.LookupEnv("TAS_IMAGES_USE_SHA")
	return GetWithFunc(ok, os.LookupEnv)
}

func GetWithFunc(useSHA bool, getImage func(string) (string, bool)) Images {
	images := Defaults(useSHA)
	if schedImage, ok := getImage("TAS_SCHEDULER_PLUGIN_IMAGE"); ok {
		images.SchedulerPluginScheduler = schedImage
	}
	if schedCtrlImage, ok := getImage("TAS_SCHEDULER_PLUGIN_CONTROLLER_IMAGE"); ok {
		images.SchedulerPluginController = schedCtrlImage
	}
	if rteImage, ok := getImage("TAS_RESOURCE_EXPORTER_IMAGE"); ok {
		images.ResourceTopologyExporter = rteImage
	}
	if nfdImage, ok := getImage("TAS_NODE_FEATURE_DISCOVERY_IMAGE"); ok {
		images.NodeFeatureDiscovery = nfdImage
	}
	return images
}
