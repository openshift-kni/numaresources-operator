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
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	"github.com/openshift-kni/numaresources-operator/internal/devices"
)

var (
	/*
	 tests that involve reboot show a difference in the memory capacity on numa nodes, e.g memory capacity
	 is reallocated across numa nodes upon startup. This is a a kernel behavior which we have no control
	 on thus we want to work it around. The tests failed when comapring initial (pre-reboot) NRT with the
	 final (post reboot) NRT, and finds out a difference of 53760000 of memory (=52500Ki ~=52Mi).
	 For those case, we allow a dev of 52Mi when comparing the memory topologies.
	 Note: by the time this modification is done, although it is performed on reboot tests only, but we made
	 sure that there is no memory amount request equal or less than 52Mi thus it is safe to continue with this
	 without confusing with a real difference caused by unsettled NRTs.
	*/
	zoneLevelDev = resource.MustParse("54525952") // 52 Mi

	/*
	 nodeLevelDev is the allowed deviation for total node-level memory resources across all NUMA zones
	 in reboot tests. The total memory across all zones on a node typically remains stable, with observed deviations
	 less than 64 KB after reboots. This tolerance ensures we catch actual memory changes at the node level while allowing for minor
	 kernel-level variations in how memory is reported after reboot.
	*/
	nodeLevelDev = resource.MustParse("65536") // 64 KB
)

type memoryTotals struct {
	capacity    resource.Quantity
	available   resource.Quantity
	allocatable resource.Quantity
}

func EqualZones(zonesA, zonesB nrtv1alpha2.ZoneList, isRebootTest bool) (bool, error) {
	if len(zonesA) != len(zonesB) {
		return false, fmt.Errorf("unequal zone count")
	}

	zA := SortedZoneList(zonesA)
	zB := SortedZoneList(zonesB)

	/*
	 the need for this came mainly because of tests requiring reboot. The reason is that in such tests it was observed that there is a slight difference
	 in the memory resources set by the kernel on NUMA zones, and also the total memory resources on the node have been observed to be different after reboot.
	 This verification ensures that the total memory resources on the node are not deviating more than the allowed deviation that was observed after sampling the results.
	 For reboot tests we allow a deviation of 64 KB on node level. And for non-reboot tests we don't allow any deviation.
	*/
	klog.InfoS("comparing memory resources on node level", "isRebootTest", isRebootTest)
	_, err := compareMemoryNodeLevel(zonesA, zonesB, isRebootTest)
	if err != nil {
		return false, err
	}

	for idx := range zA {
		zoneA := &zA[idx]
		zoneB := &zB[idx]

		if zoneA.Name != zoneB.Name {
			return false, fmt.Errorf("mismatched zones initial=%q vs updated=%q", zoneA.Name, zoneB.Name)
		}

		ok, err := EqualResourceInfos(SortedResourceInfoList(zoneA.Resources), SortedResourceInfoList(zoneB.Resources), isRebootTest)
		if !ok || err != nil {
			return ok, err
		}
	}

	return true, nil
}

func compareMemoryNodeLevel(zonesA, zonesB nrtv1alpha2.ZoneList, isRebootTest bool) (bool, error) {
	totalCapacityA, totalAvailableA, totalAllocatableA := getTotalMemory(zonesA)
	totalCapacityB, totalAvailableB, totalAllocatableB := getTotalMemory(zonesB)

	a := memoryTotals{capacity: totalCapacityA, available: totalAvailableA, allocatable: totalAllocatableA}
	b := memoryTotals{capacity: totalCapacityB, available: totalAvailableB, allocatable: totalAllocatableB}

	var err error
	if isRebootTest {
		klog.InfoS("comparing memory resources on node level within allowed deviation", "tolerance", nodeLevelDev)
		capOK := QuantityAbsCmp(totalCapacityA, totalCapacityB, nodeLevelDev)
		availOK := QuantityAbsCmp(totalAvailableA, totalAvailableB, nodeLevelDev)
		allocOK := QuantityAbsCmp(totalAllocatableA, totalAllocatableB, nodeLevelDev)

		capDelta := totalCapacityB.DeepCopy()
		capDelta.Sub(totalCapacityA)
		availDelta := totalAvailableB.DeepCopy()
		availDelta.Sub(totalAvailableA)
		allocDelta := totalAllocatableB.DeepCopy()
		allocDelta.Sub(totalAllocatableA)

		zeroQty := resource.Quantity{}
		if capDelta.Cmp(zeroQty) != 0 || availDelta.Cmp(zeroQty) != 0 || allocDelta.Cmp(zeroQty) != 0 {
			klog.InfoS("node-level memory deviation after reboot",
				"capacityDelta", capDelta.String(),
				"availableDelta", availDelta.String(),
				"allocatableDelta", allocDelta.String(),
				"tolerance", nodeLevelDev.String(),
				"capacityOK", capOK,
				"availableOK", availOK,
				"allocatableOK", allocOK)
		}

		err = collectMemoryMismatchErrors(capOK, availOK, allocOK, a, b)
	} else {
		capOK := totalCapacityA.Cmp(totalCapacityB) == 0
		availOK := totalAvailableA.Cmp(totalAvailableB) == 0
		allocOK := totalAllocatableA.Cmp(totalAllocatableB) == 0

		err = collectMemoryMismatchErrors(capOK, availOK, allocOK, a, b)
	}

	return err == nil, err
}

func getTotalMemory(zoneList nrtv1alpha2.ZoneList) (resource.Quantity, resource.Quantity, resource.Quantity) {
	var totalCapacity, totalAvailable, totalAllocatable resource.Quantity
	for _, zone := range zoneList {
		res := zone.Resources
		for _, resource := range res {
			if resource.Name != string(corev1.ResourceMemory) {
				continue
			}
			totalCapacity.Add(resource.Capacity)
			totalAvailable.Add(resource.Available)
			totalAllocatable.Add(resource.Allocatable)
		}
	}
	return totalCapacity, totalAvailable, totalAllocatable
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
		return EqualMemoryWithDeviation(resInfoA, resInfoB)
	}

	// TODO we almost never use real devices but in case we do we still pass the names using same env variables as for
	// sample devices; need a way to distinguish between real vs sample devices used in the ci
	if isRebootTest && resourceIsDevice(resInfoA.Name) {
		// TODO: Feb 2025: NROP serial is using sample-device plugin which has a known issue that after operations like node reboot or taints update,
		// quantity of the device resources is messed up, so this is a workaround for reboot tests should be removed once
		// https://issues.redhat.com/browse/CNF-12824 is resolved. Note that this doesn't happen with real devices like sriov.
		return EqualDevicesWithDeviation(resInfoA, resInfoB)
	}

	if resInfoA.Name != resInfoB.Name {
		return false, fmt.Errorf("mismatched resource name initial=%q vs updated=%q", resInfoA.Name, resInfoB.Name)
	}
	if resInfoA.Capacity.Cmp(resInfoB.Capacity) != 0 {
		return false, fmt.Errorf("mismatched resource Capacity initial=%v vs updated=%v", ResourceInfoToString(resInfoA), ResourceInfoToString(resInfoB))
	}
	if resInfoA.Allocatable.Cmp(resInfoB.Allocatable) != 0 {
		return false, fmt.Errorf("mismatched resource Allocatable initial=%v vs updated=%v", ResourceInfoToString(resInfoA), ResourceInfoToString(resInfoB))
	}
	if resInfoA.Available.Cmp(resInfoB.Available) != 0 {
		return false, fmt.Errorf("mismatched resource Available initial=%v vs updated=%v", ResourceInfoToString(resInfoA), ResourceInfoToString(resInfoB))
	}
	return true, nil
}

func EqualDevicesWithDeviation(a, b nrtv1alpha2.ResourceInfo) (bool, error) {
	if a.Name != b.Name {
		return false, fmt.Errorf("mismatched resource name initial=%q vs updated=%q", a.Name, b.Name)
	}
	if a.Capacity.Cmp(b.Capacity) != 0 {
		klog.Warningf("mismatched resource Capacity initial=%v vs updated=%v", ResourceInfoToString(a), ResourceInfoToString(b))
	}
	if a.Allocatable.Cmp(b.Allocatable) != 0 {
		klog.Warningf("mismatched resource Allocatable initial=%v vs updated=%v", ResourceInfoToString(a), ResourceInfoToString(b))
		// we shouldn't tolerate different consumptions ratios though
		ac := a.Capacity
		bc := b.Capacity
		ac.Sub(a.Allocatable)
		bc.Sub(b.Allocatable)
		if ac.Cmp(bc) != 0 {
			return false, fmt.Errorf("mismatched resource consumption; expected %v==%v=true", ac.String(), bc.String())
		}
	}
	if a.Available.Cmp(b.Available) != 0 {
		klog.Warningf("mismatched resource Available initial=%v vs updated=%v", ResourceInfoToString(a), ResourceInfoToString(b))
		// we shouldn't tolerate different consumptions ratios though
		ac := a.Capacity
		bc := b.Capacity
		ac.Sub(a.Available)
		bc.Sub(b.Available)
		if ac.Cmp(bc) != 0 {
			return false, fmt.Errorf("mismatched resource consumption; expected %v==%v=true", ac.String(), bc.String())
		}
	}
	return true, nil
}

func resourceIsDevice(resName string) bool {
	var name string
	var ok bool

	name, ok = os.LookupEnv(devices.DevType1EnvVar)
	if ok && resName == name {
		return true
	}
	name, ok = os.LookupEnv(devices.DevType2EnvVar)
	if ok && resName == name {
		return true
	}
	name, ok = os.LookupEnv(devices.DevType3EnvVar)
	if ok && resName == name {
		return true
	}
	return false
}

func EqualMemoryWithDeviation(resInfoA, resInfoB nrtv1alpha2.ResourceInfo) (bool, error) {
	if resInfoA.Name != resInfoB.Name {
		return false, fmt.Errorf("mismatched resource name initial=%q vs updated=%q", resInfoA.Name, resInfoB.Name)
	}
	if !QuantityAbsCmp(resInfoA.Capacity, resInfoB.Capacity, zoneLevelDev) {
		return false, fmt.Errorf("resource %q: mismatched resource Capacity initial=%v vs updated=%v", resInfoA.Name, resInfoA.Capacity, resInfoB.Capacity)
	}
	if !QuantityAbsCmp(resInfoA.Allocatable, resInfoB.Allocatable, zoneLevelDev) {
		return false, fmt.Errorf("resource %q: mismatched resource Allocatable initial=%v vs updated=%v", resInfoA.Name, resInfoA.Allocatable, resInfoB.Allocatable)
	}
	if !QuantityAbsCmp(resInfoA.Available, resInfoB.Available, zoneLevelDev) {
		return false, fmt.Errorf("resource %q: mismatched resource Available initial=%v vs updated=%v", resInfoA.Name, resInfoA.Available, resInfoB.Available)
	}
	return true, nil
}

func QuantityAbsCmp(a, b, dev resource.Quantity) bool {
	a.Sub(b)
	z, _ := resource.ParseQuantity("0")
	z.Sub(dev)
	return dev.Cmp(a) == 1 && z.Cmp(a) <= 0
}

func collectMemoryMismatchErrors(capOK, availOK, allocOK bool, a, b memoryTotals) error {
	var errMsgs []string
	if !capOK {
		errMsgs = append(errMsgs, fmt.Sprintf("capacity: initial=%v updated=%v", a.capacity, b.capacity))
	}
	if !availOK {
		errMsgs = append(errMsgs, fmt.Sprintf("available: initial=%v updated=%v", a.available, b.available))
	}
	if !allocOK {
		errMsgs = append(errMsgs, fmt.Sprintf("allocatable: initial=%v updated=%v", a.allocatable, b.allocatable))
	}
	if len(errMsgs) > 0 {
		return fmt.Errorf("mismatched total memory: %s", strings.Join(errMsgs, "; "))
	}
	return nil
}
