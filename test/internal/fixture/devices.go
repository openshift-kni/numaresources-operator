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

	"github.com/openshift-kni/numaresources-operator/internal/devices"
)

var (
	devType1 = ""
	devType2 = ""
	devType3 = ""
)

func init() {
	var ok bool
	devType1, ok = os.LookupEnv(devices.DevType1EnvVar)
	if !ok {
		klog.Errorf("%q environment variable is not set", devices.DevType1EnvVar)
	}
	devType2, ok = os.LookupEnv(devices.DevType2EnvVar)
	if !ok {
		klog.Errorf("%q environment variable is not set", devices.DevType2EnvVar)
	}
	devType3, ok = os.LookupEnv(devices.DevType3EnvVar)
	if !ok {
		klog.Errorf("%q environment variable is not set", devices.DevType3EnvVar)
	}
}

func GetDeviceType1Name() string {
	return devType1
}

func GetDeviceType2Name() string {
	return devType2
}

func GetDeviceType3Name() string {
	return devType3
}
