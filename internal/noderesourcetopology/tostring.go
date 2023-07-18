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
	"sort"
	"strings"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2/helper/attribute"
)

func ResourceInfoToString(resInfo nrtv1alpha2.ResourceInfo) string {
	return fmt.Sprintf("%s=%s/%s/%s", resInfo.Name, resInfo.Capacity.String(), resInfo.Allocatable.String(), resInfo.Available.String())
}

func ResourceInfoListToString(resInfoList []nrtv1alpha2.ResourceInfo) string {
	items := []string{}
	resInfos := CloneResourceInfoList(resInfoList)
	sort.Slice(resInfos, func(i, j int) bool { return resInfos[i].Name < resInfos[j].Name })
	for _, resInfo := range resInfos {
		items = append(items, ResourceInfoToString(resInfo))
	}
	return strings.Join(items, ",")
}

func ZoneToString(zone nrtv1alpha2.Zone) string {
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

func ToString(nrt nrtv1alpha2.NodeResourceTopology) string {
	var b strings.Builder
	name := nrt.Name
	if name == "" {
		name = "<MISSING>"
	}
	pol := "N/A"
	tmPolicy, ok := attribute.Get(nrt.Attributes, TopologyManagerPolicyAttribute)
	if ok {
		pol = tmPolicy.Value
	}
	scope := "N/A"
	tmScope, ok := attribute.Get(nrt.Attributes, TopologyManagerScopeAttribute)
	if ok {
		scope = tmScope.Value
	}
	fmt.Fprintf(&b, "%s policy=%s, scope=%s\n", name, pol, scope)
	for idx := range nrt.Zones {
		fmt.Fprintf(&b, "- zone: %s\n", ZoneToString(nrt.Zones[idx]))
	}
	return b.String()
}

func ListToString(nrts []nrtv1alpha2.NodeResourceTopology, tag string) string {
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

func CloneResourceInfoList(ril []nrtv1alpha2.ResourceInfo) []nrtv1alpha2.ResourceInfo {
	ret := make([]nrtv1alpha2.ResourceInfo, 0, len(ril))
	for _, ri := range ril {
		ret = append(ret, *ri.DeepCopy())
	}
	return ret
}
