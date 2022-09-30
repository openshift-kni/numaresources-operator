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
	"bytes"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"

	schedconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	"sigs.k8s.io/scheduler-plugins/apis/config/scheme"
	"sigs.k8s.io/scheduler-plugins/apis/config/v1beta2"
)

func DecodeSchedulerConfigFromData(data []byte) (*schedconfig.KubeSchedulerConfiguration, error) {
	decoder := scheme.Codecs.UniversalDecoder()
	obj, gvk, err := decoder.Decode(data, nil, nil)

	if err != nil {
		return nil, err
	}

	schedCfg, ok := obj.(*schedconfig.KubeSchedulerConfiguration)
	if !ok {
		return nil, fmt.Errorf("decoded unsupported type: %T gvk=%s", obj, gvk)
	}
	return schedCfg, nil
}

func EncodeSchedulerConfigToData(schedCfg *schedconfig.KubeSchedulerConfiguration) ([]byte, error) {
	yamlInfo, ok := runtime.SerializerInfoForMediaType(scheme.Codecs.SupportedMediaTypes(), runtime.ContentTypeYAML)
	if !ok {
		return nil, fmt.Errorf("unable to locate encoder -- %q is not a supported media type", runtime.ContentTypeYAML)
	}

	encoder := scheme.Codecs.EncoderForVersion(yamlInfo.Serializer, v1beta2.SchemeGroupVersion)

	var buf bytes.Buffer
	err := encoder.Encode(schedCfg, &buf)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
