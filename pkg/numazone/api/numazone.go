/*
Copyright 2020.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	NUMAZoneDevicePath        = "/dev/null"
	NUMAZoneResourceName      = "numazone"
	NUMAZoneResourceNamespace = "kni.node"

	NUMAZoneEnvironVarName = "KNI_NODE_ZONE_ID"

	NUMAZoneDefaultDeviceCount = 15
)

func MakeResourceName(numazoneid int) corev1.ResourceName {
	return corev1.ResourceName(fmt.Sprintf("%s/%s", NUMAZoneResourceNamespace, MakeDeviceID(numazoneid)))
}

func MakeDeviceID(numazoneid int) string {
	return fmt.Sprintf("%s%02d", NUMAZoneResourceName, numazoneid)
}

func IsResourceName(resName string) bool {
	tmpl := fmt.Sprintf("%s/%s", NUMAZoneResourceNamespace, NUMAZoneResourceName)
	return strings.HasPrefix(resName, tmpl)
}
