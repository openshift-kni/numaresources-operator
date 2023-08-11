/*
 * Copyright 2023 Red Hat, Inc.
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

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
)

func EqualZones(zonesA, zonesB nrtv1alpha2.ZoneList) (bool, error) {
	if len(zonesA) != len(zonesB) {
		return false, fmt.Errorf("unequal zone count")
	}

	zA := SortedZoneList(zonesA)
	zB := SortedZoneList(zonesB)

	for idx := range zA {
		zoneA := &zA[idx]
		zoneB := &zB[idx]

		if zoneA.Name != zoneB.Name {
			return false, fmt.Errorf("mismatched zones %q vs %q", zoneA.Name, zoneB.Name)
		}

		ok, err := EqualResourceInfos(SortedResourceInfoList(zoneA.Resources), SortedResourceInfoList(zoneB.Resources))
		if !ok || err != nil {
			return ok, err
		}
	}

	return true, nil
}

func EqualResourceInfos(resInfosA, resInfosB nrtv1alpha2.ResourceInfoList) (bool, error) {
	if len(resInfosA) != len(resInfosB) {
		return false, fmt.Errorf("unequal resourceinfo count")
	}

	for idx := range resInfosA {
		resInfoA := resInfosA[idx]
		resInfoB := resInfosB[idx]

		ok, err := EqualResourceInfo(resInfoA, resInfoB)
		if !ok || err != nil {
			return ok, err
		}
	}

	return true, nil
}

func EqualResourceInfo(resInfoA, resInfoB nrtv1alpha2.ResourceInfo) (bool, error) {
	if resInfoA.Name != resInfoB.Name {
		return false, fmt.Errorf("mismatched resource name %q vs %q", resInfoA.Name, resInfoB.Name)
	}
	if resInfoA.Capacity.Cmp(resInfoB.Capacity) != 0 {
		return false, fmt.Errorf("resource %q: mismatched resource Capacity %v vs %v", resInfoA.Name, resInfoA.Capacity, resInfoB.Capacity)
	}
	if resInfoA.Allocatable.Cmp(resInfoB.Allocatable) != 0 {
		return false, fmt.Errorf("resource %q: mismatched resource Allocatable %v vs %v", resInfoA.Name, resInfoA.Allocatable, resInfoB.Allocatable)
	}
	if resInfoA.Available.Cmp(resInfoB.Available) != 0 {
		return false, fmt.Errorf("resource %q: mismatched resource Available %v vs %v", resInfoA.Name, resInfoA.Available, resInfoB.Available)
	}
	return true, nil
}
