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

package sched

import (
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	schedconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	pluginconfig "sigs.k8s.io/scheduler-plugins/apis/config"

	"github.com/k8stopologyawareschedwg/deployer/pkg/images"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
)

const (
	SchedulerConfigFileName = "scheduler-config.yaml" // TODO duplicate from yaml
	schedulerPluginName     = "NodeResourceTopologyMatch"
)

func SchedulerDeployment(dp *appsv1.Deployment, pullIfNotPresent bool) {
	dp.Spec.Template.Spec.Containers[0].Image = images.SchedulerPluginSchedulerImage
	dp.Spec.Template.Spec.Containers[0].ImagePullPolicy = pullPolicy(pullIfNotPresent)
}

func ControllerDeployment(dp *appsv1.Deployment, pullIfNotPresent bool) {
	dp.Spec.Template.Spec.Containers[0].Image = images.SchedulerPluginControllerImage
	dp.Spec.Template.Spec.Containers[0].ImagePullPolicy = pullPolicy(pullIfNotPresent)
}

func SchedulerConfig(cm *corev1.ConfigMap, schedulerName string, cacheResyncPeriod time.Duration) error {
	if cm.Data == nil {
		return fmt.Errorf("no data found in ConfigMap: %s/%s", cm.Namespace, cm.Name)
	}

	data, ok := cm.Data[SchedulerConfigFileName]
	if !ok {
		return fmt.Errorf("no data key named: %s found in ConfigMap: %s/%s", SchedulerConfigFileName, cm.Namespace, cm.Name)
	}

	newData, err := RenderConfig(data, schedulerName, cacheResyncPeriod)
	if err != nil {
		return err
	}

	cm.Data[SchedulerConfigFileName] = string(newData)
	return nil
}

func RenderConfig(data, schedulerName string, cacheResyncPeriod time.Duration) (string, error) {
	schedCfg, err := manifests.DecodeSchedulerConfigFromData([]byte(data))
	if err != nil {
		return data, err
	}

	schedProf, pluginConf := findKubeSchedulerProfileByName(schedCfg, schedulerPluginName)
	if schedProf == nil || pluginConf == nil {
		return data, fmt.Errorf("no profile or plugin configuration found for %q", schedulerPluginName)
	}

	if schedulerName != "" {
		schedProf.SchedulerName = schedulerName
	}

	confObj := pluginConf.Args.DeepCopyObject()
	cfg, ok := confObj.(*pluginconfig.NodeResourceTopologyMatchArgs)
	if !ok {
		return data, fmt.Errorf("unsupported plugin config type: %T", confObj)
	}

	period := int64(cacheResyncPeriod.Seconds())
	cfg.CacheResyncPeriodSeconds = period

	pluginConf.Args = cfg

	newData, err := manifests.EncodeSchedulerConfigToData(schedCfg)
	return string(newData), err
}

func findKubeSchedulerProfileByName(sc *schedconfig.KubeSchedulerConfiguration, name string) (*schedconfig.KubeSchedulerProfile, *schedconfig.PluginConfig) {
	for i := range sc.Profiles {
		// if we have a configuration for the NodeResourceTopologyMatch
		// this is a valid profile
		for j := range sc.Profiles[i].PluginConfig {
			if sc.Profiles[i].PluginConfig[j].Name == name {
				return &sc.Profiles[i], &sc.Profiles[i].PluginConfig[j]
			}
		}
	}

	return nil, nil
}

func pullPolicy(pullIfNotPresent bool) corev1.PullPolicy {
	if pullIfNotPresent {
		return corev1.PullIfNotPresent
	}
	return corev1.PullAlways
}
