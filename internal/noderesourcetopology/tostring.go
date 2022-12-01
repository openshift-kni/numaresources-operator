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

package noderesourcetopology

import (
	"fmt"
	"strings"

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"
)

func ResourceInfoToString(resInfo nrtv1alpha1.ResourceInfo) string {
	return fmt.Sprintf("%s=%s/%s/%s", resInfo.Name, resInfo.Capacity.String(), resInfo.Allocatable.String(), resInfo.Available.String())
}

func ResourceInfoListToString(resInfoList []nrtv1alpha1.ResourceInfo) string {
	items := []string{}
	for _, resInfo := range resInfoList {
		items = append(items, ResourceInfoToString(resInfo))
	}
	return strings.Join(items, ",")
}

func ZoneToString(zone nrtv1alpha1.Zone) string {
	name := zone.Name
	if name == "" {
		name = "<MISSING>"
	}
	zType := zone.Type
	if zType == "" {
		zType = "N/A"
	}
	resList := ResourceInfoListToString(zone.Resources)
	if resList == "" {
		resList = "N/A"
	}
	return fmt.Sprintf("%s [%s]: %s", name, zType, resList)
}

func ToString(nrt nrtv1alpha1.NodeResourceTopology) string {
	var b strings.Builder
	name := nrt.Name
	if name == "" {
		name = "<MISSING>"
	}
	pol := "N/A"
	if len(nrt.TopologyPolicies) > 0 {
		pol = nrt.TopologyPolicies[0]
	}
	fmt.Fprintf(&b, "%s policy=%s\n", name, pol)
	for idx := range nrt.Zones {
		fmt.Fprintf(&b, "- zone: %s\n", ZoneToString(nrt.Zones[idx]))
	}
	return b.String()
}

func ListToString(nrts []nrtv1alpha1.NodeResourceTopology, tag string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "NRT BEGIN dump")
	if len(tag) != 0 {
		fmt.Fprintf(&b, " %s", tag)
	}
	fmt.Fprintf(&b, "\n")
	for idx := range nrts {
		fmt.Fprintf(&b, ToString(nrts[idx]))
	}
	fmt.Fprintf(&b, "NRT END dump\n")
	return b.String()
}
