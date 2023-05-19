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
	"bytes"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	"sigs.k8s.io/yaml"

	schedscheme "sigs.k8s.io/scheduler-plugins/apis/config/scheme"
	schedapiv1beta2 "sigs.k8s.io/scheduler-plugins/apis/config/v1beta2"

	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
)

// all this pain because in the internal unversioned types cacheResyncPeriodSeconds is NOT a pointer type :\
func CleanSchedulerConfig(data []byte) []byte {
	var r unstructured.Unstructured
	if err := yaml.Unmarshal(data, &r.Object); err != nil {
		klog.ErrorS(err, "cannot unmarshal scheduler config, passing through")
		return data
	}

	profiles, ok, err := unstructured.NestedSlice(r.Object, "profiles")
	if !ok || err != nil {
		klog.ErrorS(err, "failed to process unstructured data", "profiles", ok)
		return data
	}
	for _, prof := range profiles {
		profile, ok := prof.(map[string]interface{})
		if !ok {
			klog.V(1).InfoS("unexpected profile data")
			return data
		}

		pluginConfigs, ok, err := unstructured.NestedSlice(profile, "pluginConfig")
		if !ok || err != nil {
			klog.ErrorS(err, "failed to process unstructured data", "pluginConfig", ok)
			return data
		}
		for _, plConf := range pluginConfigs {
			pluginConf, ok := plConf.(map[string]interface{})
			if !ok {
				klog.V(1).InfoS("unexpected profile coonfig data")
				return data
			}

			name, ok, err := unstructured.NestedString(pluginConf, "name")
			if !ok || err != nil {
				klog.ErrorS(err, "failed to process unstructured data", "name", ok)
				return data
			}
			if name != schedstate.SchedulerPluginName {
				continue
			}
			args, ok, err := unstructured.NestedMap(pluginConf, "args")
			if !ok || err != nil {
				klog.ErrorS(err, "failed to process unstructured data", "args", ok)
				return data
			}

			// TODO
			resyncPeriod, ok, err := unstructured.NestedFloat64(args, "cacheResyncPeriodSeconds")
			if !ok || err != nil {
				klog.ErrorS(err, "failed to process unstructured data", "cacheResyncPeriodSeconds", ok)
				return data
			}

			if resyncPeriod > 0 {
				return data // nothing to do!
			}

			delete(args, "cacheResyncPeriodSeconds")

			if err := unstructured.SetNestedMap(pluginConf, args, "args"); err != nil {
				klog.ErrorS(err, "failed to override unstructured data", "data", "args")
				return data
			}
		}

		if err := unstructured.SetNestedSlice(profile, pluginConfigs, "pluginConfig"); err != nil {
			klog.ErrorS(err, "failed to override unstructured data", "data", "pluginConfig")
			return data
		}
	}

	if err := unstructured.SetNestedSlice(r.Object, profiles, "profiles"); err != nil {
		klog.ErrorS(err, "failed to override unstructured data", "data", "profiles")
		return data
	}

	newData, err := encodeUnstructuredSchedulerConfigToData(r)
	if err != nil {
		klog.ErrorS(err, "cannot re-encode scheduler config, passing through")
		return data // silent passthrough
	}
	return newData
}

func encodeUnstructuredSchedulerConfigToData(r unstructured.Unstructured) ([]byte, error) {
	yamlInfo, ok := runtime.SerializerInfoForMediaType(schedscheme.Codecs.SupportedMediaTypes(), runtime.ContentTypeYAML)
	if !ok {
		return nil, fmt.Errorf("unable to locate encoder -- %q is not a supported media type", runtime.ContentTypeYAML)
	}

	encoder := schedscheme.Codecs.EncoderForVersion(yamlInfo.Serializer, schedapiv1beta2.SchemeGroupVersion)

	var buf bytes.Buffer
	err := encoder.Encode(&r, &buf)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
