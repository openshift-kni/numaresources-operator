/*
 * Copyright 2022 Red Hat, Inc.
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

package fixture

import (
	"os"

	"k8s.io/klog/v2"
)

const (
	devType1EnvVar = "E2E_NROP_DEVICE_TYPE_1"
	devType2EnvVar = "E2E_NROP_DEVICE_TYPE_2"
	devType3EnvVar = "E2E_NROP_DEVICE_TYPE_3"
)

func GetDeviceType1Name() string {
	if devType1, ok := os.LookupEnv(devType1EnvVar); ok {
		return devType1
	}
	klog.Errorf("%q environment variable is not set", devType1EnvVar)
	return ""
}

func GetDeviceType2Name() string {
	if devType2, ok := os.LookupEnv(devType2EnvVar); ok {
		return devType2
	}
	klog.Errorf("%q environment variable is not set", devType2EnvVar)
	return ""
}

func GetDeviceType3Name() string {
	if devType3, ok := os.LookupEnv(devType3EnvVar); ok {
		return devType3
	}
	klog.Errorf("%q environment variable is not set", devType3EnvVar)
	return ""
}
