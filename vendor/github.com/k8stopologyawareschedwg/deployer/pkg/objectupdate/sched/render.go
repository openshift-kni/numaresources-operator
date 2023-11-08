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
 * Copyright 2023 Red Hat, Inc.
 */

package sched

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"

	"sigs.k8s.io/yaml"

	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
)

func SchedulerConfig(cm *corev1.ConfigMap, schedulerName string, params *manifests.ConfigParams) error {
	if cm.Data == nil {
		return fmt.Errorf("no data found in ConfigMap: %s/%s", cm.Namespace, cm.Name)
	}

	data, ok := cm.Data[manifests.SchedulerConfigFileName]
	if !ok {
		return fmt.Errorf("no data key named: %s found in ConfigMap: %s/%s", manifests.SchedulerConfigFileName, cm.Namespace, cm.Name)
	}

	newData, _, err := RenderConfig([]byte(data), schedulerName, params)
	if err != nil {
		return err
	}

	cm.Data[manifests.SchedulerConfigFileName] = string(newData)
	return nil
}

func RenderConfig(data []byte, schedulerName string, params *manifests.ConfigParams) ([]byte, bool, error) {
	if schedulerName == "" || params == nil {
		klog.V(2).InfoS("missing parameters, passing through", "schedulerName", schedulerName, "params", toJSON(params))
		return data, false, nil
	}

	var r unstructured.Unstructured
	if err := yaml.Unmarshal(data, &r.Object); err != nil {
		klog.ErrorS(err, "cannot unmarshal scheduler config, passing through")
		return data, false, err
	}

	updated := false

	profiles, ok, err := unstructured.NestedSlice(r.Object, "profiles")
	if !ok || err != nil {
		klog.ErrorS(err, "failed to process unstructured data", "profiles", ok)
		return data, false, err
	}
	for _, prof := range profiles {
		profile, ok := prof.(map[string]interface{})
		if !ok {
			klog.V(1).InfoS("unexpected profile data")
			return data, false, nil
		}

		profileName, ok, err := unstructured.NestedString(profile, "schedulerName")
		if !ok || err != nil {
			klog.ErrorS(err, "failed to get profile name", "profileName", ok)
			return data, false, err
		}

		if profileName != schedulerName {
			continue
		}

		if params.ProfileName != "" {
			unstructured.SetNestedField(profile, params.ProfileName, "schedulerName")
			updated = true
		}

		pluginConfigs, ok, err := unstructured.NestedSlice(profile, "pluginConfig")
		if !ok || err != nil {
			klog.ErrorS(err, "failed to process unstructured data", "pluginConfig", ok)
			return data, false, err
		}
		for _, plConf := range pluginConfigs {
			pluginConf, ok := plConf.(map[string]interface{})
			if !ok {
				klog.V(1).InfoS("unexpected profile coonfig data")
				return data, false, nil
			}

			name, ok, err := unstructured.NestedString(pluginConf, "name")
			if !ok || err != nil {
				klog.ErrorS(err, "failed to process unstructured data", "name", ok)
				return data, false, err
			}
			if name != manifests.SchedulerPluginName {
				continue
			}
			args, ok, err := unstructured.NestedMap(pluginConf, "args")
			if !ok || err != nil {
				klog.ErrorS(err, "failed to process unstructured data", "args", ok)
				return data, false, err
			}

			argsUpdated, err := updateArgs(args, params)
			if err != nil {
				klog.ErrorS(err, "failed to update unstructured data", "args", args, "params", params)
				return data, false, err
			}
			if argsUpdated {
				updated = true
			}

			if err := unstructured.SetNestedMap(pluginConf, args, "args"); err != nil {
				klog.ErrorS(err, "failed to override unstructured data", "data", "args")
				return data, false, err
			}
		}

		if err := unstructured.SetNestedSlice(profile, pluginConfigs, "pluginConfig"); err != nil {
			klog.ErrorS(err, "failed to override unstructured data", "data", "pluginConfig")
			return data, false, err
		}
	}

	if err := unstructured.SetNestedSlice(r.Object, profiles, "profiles"); err != nil {
		klog.ErrorS(err, "failed to override unstructured data", "data", "profiles")
		return data, false, err
	}

	newData, err := yaml.Marshal(&r.Object)
	if err != nil {
		klog.ErrorS(err, "cannot re-encode scheduler config, passing through")
		return data, false, nil
	}
	return newData, updated, nil
}

func updateArgs(args map[string]interface{}, params *manifests.ConfigParams) (bool, error) {
	var updated int
	var err error

	if params.Cache != nil {
		if params.Cache.ResyncPeriodSeconds != nil {
			resyncPeriod := *params.Cache.ResyncPeriodSeconds // shortcut
			err = unstructured.SetNestedField(args, resyncPeriod, "cacheResyncPeriodSeconds")
			if err != nil {
				return updated > 0, err
			}
			updated++
		}
	}
	return updated > 0, ensureBackwardCompatibility(args)
}

func ensureBackwardCompatibility(args map[string]interface{}) error {
	resyncPeriod, ok, err := unstructured.NestedInt64(args, "cacheResyncPeriodSeconds")
	if !ok {
		// nothing to do
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot process field cacheResyncPeriodSeconds: %w", err)
	}
	if resyncPeriod > 0 {
		// nothing to do
	} else {
		// remove for backward compatibility
		delete(args, "cacheResyncPeriodSeconds")
	}
	return nil
}

func toJSON(v any) string {
	if v == nil {
		return "<nil>"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<err=%v>", err)
	}
	return string(data)
}
