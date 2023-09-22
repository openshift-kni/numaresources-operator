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
	"strings"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func EqualZones(zonesA, zonesB nrtv1alpha2.ZoneList, isRebootTest bool) (bool, error) {
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

		ok, err := EqualResourceInfos(SortedResourceInfoList(zoneA.Resources), SortedResourceInfoList(zoneB.Resources), isRebootTest)
		if !ok || err != nil {
			return ok, err
		}
	}

	return true, nil
}

func EqualResourceInfos(resInfosA, resInfosB nrtv1alpha2.ResourceInfoList, isRebootTest bool) (bool, error) {
	if len(resInfosA) != len(resInfosB) {
		return false, fmt.Errorf("unequal resourceinfo count")
	}

	for idx := range resInfosA {
		resInfoA := resInfosA[idx]
		resInfoB := resInfosB[idx]

		ok, err := EqualResourceInfo(resInfoA, resInfoB, isRebootTest)
		if !ok || err != nil {
			return ok, err
		}
	}

	return true, nil
}

func EqualResourceInfo(resInfoA, resInfoB nrtv1alpha2.ResourceInfo, isRebootTest bool) (bool, error) {
	if isRebootTest && strings.Compare(resInfoA.Name, string(corev1.ResourceMemory)) == 0 {
		return EqualResourceInfoWithDeviation(resInfoA, resInfoB)
	}

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

func EqualResourceInfoWithDeviation(resInfoA, resInfoB nrtv1alpha2.ResourceInfo) (bool, error) {
	/*
		tests that involve reboot show a difference in the memory capacity on numa nodes, e.g memory capacity
		 is reallocated across numa nodes upon startup. This is a a kernel behavior which we have no control
		 on thus we want to work it around. The tests failed when comapring initial (pre-reboot) NRT with the
		 final (post reboot) NRT, and finds out a difference of 53760000 of memory (=52500Ki ~=52Mi).
		 For those case, we allow a deviation of 52Mi when comparing the memory topologies.
		 Note: by the time this modification is done, although it is performed on reboot tests only, but we made
		 sure that there is no memory amount request equal or less than 52Mi thus it is safe to continue with this
		  without confusing with a real difference caused by unsettled NRTs.
	*/
	dev, _ := resource.ParseQuantity("54525952") //52 Mi

	if resInfoA.Name != resInfoB.Name {
		return false, fmt.Errorf("mismatched resource name %q vs %q", resInfoA.Name, resInfoB.Name)
	}
	if !QuantityAbsCmp(resInfoA.Capacity, resInfoB.Capacity, dev) {
		return false, fmt.Errorf("resource %q: mismatched resource Capacity %v vs %v", resInfoA.Name, resInfoA.Capacity, resInfoB.Capacity)
	}
	if !QuantityAbsCmp(resInfoA.Allocatable, resInfoB.Allocatable, dev) {
		return false, fmt.Errorf("resource %q: mismatched resource Allocatable %v vs %v", resInfoA.Name, resInfoA.Allocatable, resInfoB.Allocatable)
	}
	if !QuantityAbsCmp(resInfoA.Available, resInfoB.Available, dev) {
		return false, fmt.Errorf("resource %q: mismatched resource Available %v vs %v", resInfoA.Name, resInfoA.Available, resInfoB.Available)
	}
	return true, nil
}

func QuantityAbsCmp(a, b, dev resource.Quantity) bool {
	a.Sub(b)
	z, _ := resource.ParseQuantity("0")
	z.Sub(dev)
	return dev.Cmp(a) == 1 && z.Cmp(a) <= 0
}
