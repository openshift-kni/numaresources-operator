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

package resourcelist

import (
	"fmt"
	"math"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func ToString(res corev1.ResourceList) string {
	idx := 0
	resNames := make([]string, len(res))
	for resName := range res {
		resNames[idx] = string(resName)
		idx++
	}
	sort.Strings(resNames)

	items := []string{}
	for _, resName := range resNames {
		resQty := res[corev1.ResourceName(resName)]
		items = append(items, fmt.Sprintf("%s=%s", resName, resQty.String()))
	}
	return strings.Join(items, ", ")
}

func FromReplicaSet(rs appsv1.ReplicaSet) corev1.ResourceList {
	rl := FromContainerLimits(rs.Spec.Template.Spec.Containers)
	replicas := rs.Spec.Replicas
	for resName, resQty := range rl {
		replicaResQty := resQty.DeepCopy()
		// index begins from 1 because we already have resources of one replica
		for i := 1; i < int(*replicas); i++ {
			resQty.Add(replicaResQty)
		}
		rl[resName] = resQty
	}
	return rl
}

func FromGuaranteedPod(pod corev1.Pod) corev1.ResourceList {
	return FromContainerLimits(pod.Spec.Containers)
}

func FromContainerLimits(containers []corev1.Container) corev1.ResourceList {
	res := make(corev1.ResourceList)
	for idx := 0; idx < len(containers); idx++ {
		cnt := &containers[idx] // shortcut
		for resName, resQty := range cnt.Resources.Limits {
			qty := res[resName]
			qty.Add(resQty)
			res[resName] = qty
		}
	}
	return res
}

func FromContainerRequests(containers []corev1.Container) corev1.ResourceList {
	res := make(corev1.ResourceList)
	for idx := 0; idx < len(containers); idx++ {
		cnt := &containers[idx] // shortcut
		for resName, resQty := range cnt.Resources.Requests {
			qty := res[resName]
			qty.Add(resQty)
			res[resName] = qty
		}
	}
	return res
}

func AddInPlace(res, resToAdd corev1.ResourceList) {
	for resName, resQty := range resToAdd {
		qty := res[resName]
		qty.Add(resQty)
		res[resName] = qty
	}
}

func SubInPlace(res, resToSub corev1.ResourceList) error {
	for resName, resQty := range resToSub {
		if resQty.Cmp(res[resName]) > 0 {
			return fmt.Errorf("cannot subtract resource %q because it is not found in the current resources", resName)
		}
		qty := res[resName]
		qty.Sub(resQty)
		res[resName] = qty
	}
	return nil
}

func Accumulate(ress []corev1.ResourceList, filterFunc func(corev1.ResourceName, resource.Quantity) bool) corev1.ResourceList {
	ret := corev1.ResourceList{}
	for _, rr := range ress {
		for resName, resQty := range rr {
			if !filterFunc(resName, resQty) {
				continue
			}
			qty := ret[resName]
			qty.Add(resQty)
			ret[resName] = qty
		}
	}
	return ret
}

func AllowAll(_ corev1.ResourceName, _ resource.Quantity) bool {
	return true
}

func FilterExclusive(resName corev1.ResourceName, resQty resource.Quantity) bool {
	if resName == corev1.ResourceEphemeralStorage || resName == corev1.ResourceStorage {
		return false
	}

	if resName == corev1.ResourceCPU {
		if resQty.MilliValue()%1000 > 0 {
			return false
		}
	}
	return true
}

func Equal(ra, rb corev1.ResourceList) bool {
	if len(ra) != len(rb) {
		return false
	}
	for key, valA := range ra {
		valB, ok := rb[key]
		if !ok {
			return false
		}
		if !valA.Equal(valB) {
			return false
		}
	}
	return true
}

func RoundUpCoreResources(cpu, mem resource.Quantity) (resource.Quantity, resource.Quantity) {
	retCpu := *resource.NewQuantity(roundUp(cpu.Value(), 2), resource.DecimalSI)
	retMem := mem.DeepCopy() // TODO: this is out of over caution
	// FIXME: this rounds to G (1000) not to Gi (1024) which works but is not what we intended
	retMem.RoundUp(resource.Giga)
	return retCpu, retMem
}

func roundUp(num, multiple int64) int64 {
	return ((num + multiple - 1) / multiple) * multiple
}

func Highest(rls ...corev1.ResourceList) corev1.ResourceList {
	if len(rls) == 0 {
		return make(corev1.ResourceList)
	}
	ret := rls[0].DeepCopy()
	if len(rls) == 1 {
		return ret
	}
	for _, rl := range rls[1:] {
		tmp := ret.DeepCopy()
		for resName, resQty := range ret {
			curQty := rl[resName]
			if curQty.Cmp(resQty) > 0 {
				tmp[resName] = curQty
			}
		}
		ret = tmp
	}
	return ret
}

func ScaleCoreResources(rl corev1.ResourceList, scaleNum, scaleDen int) corev1.ResourceList {
	ret := rl.DeepCopy()
	if cpuQty, ok := rl[corev1.ResourceCPU]; ok {
		newVal := scaleQuantity(cpuQty, scaleNum, scaleDen)
		ret[corev1.ResourceCPU] = *resource.NewQuantity(newVal, resource.DecimalSI)
	}
	if memQty, ok := rl[corev1.ResourceMemory]; ok {
		// rounding up is unnecessary but unharmful either, and makes testing and inspection easier
		newVal := scaleQuantity(memQty, scaleNum, scaleDen)
		newVal = roundUp(newVal, 1024)
		ret[corev1.ResourceMemory] = *resource.NewQuantity(newVal, resource.DecimalSI)
	}
	return ret
}

func scaleQuantity(qty resource.Quantity, scaleNum, scaleDen int) int64 {
	val, _ := qty.AsInt64() // TODO: handle !ok?
	// we use Ceil, not Round, to make sure to include the fractional amounts
	return int64(math.Ceil(float64(val*int64(scaleNum)) / float64(scaleDen)))
}
