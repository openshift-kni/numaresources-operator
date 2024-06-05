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

package noderesourcetopologies

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2/helper/attribute"
	nrtv1alpha2attr "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2/helper/attribute"
	"github.com/k8stopologyawareschedwg/podfingerprint"

	e2enrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
)

// ErrNotEnoughResources means a NUMA zone or a node has not enough resouces to reserve
var ErrNotEnoughResources = errors.New("nrt: Not enough resources")

func GetZoneIDFromName(zoneName string) (int, error) {
	for _, prefix := range []string{
		"node-",
	} {
		if !strings.HasPrefix(zoneName, prefix) {
			continue
		}
		return strconv.Atoi(zoneName[len(prefix):])
	}
	return strconv.Atoi(zoneName)
}

func GetUpdated(cli client.Client, initialNrtList nrtv1alpha2.NodeResourceTopologyList, timeout time.Duration) (nrtv1alpha2.NodeResourceTopologyList, error) {
	var updatedNrtList nrtv1alpha2.NodeResourceTopologyList
	klog.Infof("Waiting up to %v to get %d NRT objects updated", timeout, len(initialNrtList.Items))
	immediate := false
	err := wait.PollUntilContextTimeout(context.Background(), 5*time.Second, timeout, immediate, func(ctx context.Context) (bool, error) {
		err := cli.List(ctx, &updatedNrtList)
		if err != nil {
			klog.Errorf("cannot get the NRT List: %v", err)
			return false, err
		}
		for idx := range initialNrtList.Items {
			initialNrt := &initialNrtList.Items[idx]
			updatedNrt, err := FindFromList(updatedNrtList.Items, initialNrt.Name)
			if err != nil {
				klog.Errorf("missing NRT for %s: %v", initialNrt.Name, err)
				return false, err
			}
			if isEqualNRT(*initialNrt, *updatedNrt) {
				klog.Warningf("NRT for %s not yet updated", initialNrt.Name)
				return false, err
			}
		}
		klog.Infof("Detected changes on all %d NRT objects", len(initialNrtList.Items))
		return true, nil
	})
	return updatedNrtList, err
}

func GetUpdatedForNode(cli client.Client, ctx context.Context, ref nrtv1alpha2.NodeResourceTopology, timeout time.Duration) (nrtv1alpha2.NodeResourceTopology, error) {
	var equalZones bool
	var updatedNrt nrtv1alpha2.NodeResourceTopology
	nrtKey := client.ObjectKeyFromObject(&ref)
	klog.Infof("NRT change: reference is %s", e2enrt.ToString(ref))
	immediate := true
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, timeout, immediate, func(aContext context.Context) (bool, error) {
		err := cli.Get(aContext, nrtKey, &updatedNrt)
		if err != nil {
			klog.Errorf("cannot get the updated NRT object %s/%s", ref.Namespace, ref.Name)
			return false, err
		}
		equalZones = isEqualNRT(ref, updatedNrt)
		if equalZones {
			klog.Warningf("NRT %s not updated yet", ref.Name)
			return false, nil
		}
		return true, nil
	})
	klog.Infof("NRT change: finished, equalZones=%v", equalZones)
	return updatedNrt, err
}

func isEqualNRT(initialNrt, updatedNrt nrtv1alpha2.NodeResourceTopology) bool {
	// very cheap test to rule out false negatives
	if updatedNrt.ObjectMeta.ResourceVersion == initialNrt.ObjectMeta.ResourceVersion {
		klog.Warningf("NRT %q resource version didn't change", initialNrt.Name)
		return true
	}
	equalZones := apiequality.Semantic.DeepEqual(initialNrt.Zones, updatedNrt.Zones)
	equalAttributes := apiequality.Semantic.DeepEqual(initialNrt.Attributes, updatedNrt.Attributes)

	if !equalZones || !equalAttributes {
		klog.Infof("NRT %q change: updated to %s", initialNrt.Name, e2enrt.ToString(updatedNrt))
	}
	return equalZones && equalAttributes
}

func CheckEqualAvailableResources(nrtInitial, nrtUpdated nrtv1alpha2.NodeResourceTopology) (bool, error) {
	logPFP(nrtInitial, "initial")
	logPFP(nrtUpdated, "updated")
	for idx := 0; idx < len(nrtInitial.Zones); idx++ {
		zoneInitial := &nrtInitial.Zones[idx] // shortcut
		zoneUpdated, err := findZoneByName(nrtUpdated, zoneInitial.Name)
		if err != nil {
			klog.Errorf("missing updated zone %q: %v", zoneInitial.Name, err)
			return false, err
		}
		ok, what, err := checkEqualResourcesInfo(nrtInitial.Name, zoneInitial.Name, zoneInitial.Resources, zoneUpdated.Resources)
		if err != nil {
			klog.Errorf("error checking zone %q: %v", zoneInitial.Name, err)
			return false, err
		}
		if !ok {
			klog.Infof("node %q zone %q resource %q is different", nrtInitial.Name, zoneInitial.Name, what)
			return false, nil
		}
	}
	klog.Infof("=> NRT %d zones equal", len(nrtInitial.Zones))
	return true, nil
}

func logPFP(nrt nrtv1alpha2.NodeResourceTopology, tag string) {
	attr, ok := nrtv1alpha2attr.Get(nrt.Attributes, podfingerprint.Attribute)
	if !ok {
		klog.Warningf("=> %s NRT %s had no PFP attribute", tag, nrt.Name)
		return
	}
	klog.Infof("=> %s NRT %s PFP attribute %s", tag, nrt.Name, attr.Value)
}

func CheckZoneConsumedResourcesAtLeast(nrtInitial, nrtUpdated nrtv1alpha2.NodeResourceTopology, required corev1.ResourceList, podQoS corev1.PodQOSClass) (string, error) {
	for idx := 0; idx < len(nrtInitial.Zones); idx++ {
		zoneInitial := &nrtInitial.Zones[idx] // shortcut
		zoneUpdated, err := findZoneByName(nrtUpdated, zoneInitial.Name)
		if err != nil {
			klog.Errorf("missing updated zone %q: %v", zoneInitial.Name, err)
			return "", err
		}
		ok, err := checkConsumedResourcesAtLeast(zoneInitial.Resources, zoneUpdated.Resources, required, podQoS)
		if err != nil {
			klog.Errorf("error checking zone %q: %v", zoneInitial.Name, err)
			return "", err
		}
		if ok {
			klog.Infof("match for zone %q", zoneInitial.Name)
			return zoneInitial.Name, nil
		}
	}
	return "", nil
}

func CheckNodeConsumedResourcesAtLeast(nrtInitial, nrtUpdated nrtv1alpha2.NodeResourceTopology, required corev1.ResourceList, podQoS corev1.PodQOSClass) (string, error) {
	nodeResInitialInfo, err := accumulateNodeAvailableResources(nrtInitial, "initial")
	if err != nil {
		return "", err
	}
	nodeResUpdatedInfo, err := accumulateNodeAvailableResources(nrtUpdated, "updated")
	if err != nil {
		return "", err
	}
	ok, err := checkConsumedResourcesAtLeast(nodeResInitialInfo, nodeResUpdatedInfo, required, podQoS)
	if err != nil {
		klog.Errorf("error checking node %q: %v", nrtInitial.Name, err)
		return "", err
	}
	if ok {
		klog.Infof("match for node %q", nrtInitial.Name)
		return nrtInitial.Name, nil
	}
	return "", nil
}

func accumulateNodeAvailableResources(nrt nrtv1alpha2.NodeResourceTopology, reason string) ([]nrtv1alpha2.ResourceInfo, error) {
	resList := make(corev1.ResourceList, 2)
	for _, zone := range nrt.Zones {
		for _, res := range zone.Resources {
			if q, ok := resList[corev1.ResourceName(res.Name)]; ok {
				q.Add(res.Available)
				resList[corev1.ResourceName(res.Name)] = q
			} else {
				resList[corev1.ResourceName(res.Name)] = res.Available
			}
		}
	}
	var resInfoList []nrtv1alpha2.ResourceInfo
	for r, q := range resList {
		resInfo := nrtv1alpha2.ResourceInfo{
			Name:        string(r),
			Capacity:    q, // not required, added for consistency
			Allocatable: q, // not required, added for consistency
			Available:   q, // required
		}
		resInfoList = append(resInfoList, resInfo)
	}
	if len(resInfoList) < 1 {
		return resInfoList, fmt.Errorf("failed to accumulate resources for node %q", nrt.Name)
	}
	klog.Infof("resInfoList available %s: %s", reason, e2enrt.ResourceInfoListToString(resInfoList))
	return resInfoList, nil
}
func SaturateZoneUntilLeft(zone nrtv1alpha2.Zone, requiredRes corev1.ResourceList, filter func(*corev1.ResourceList)) (corev1.ResourceList, error) {
	paddingRes := make(corev1.ResourceList)

	filter(&requiredRes)

	for resName, resQty := range requiredRes {
		zoneQty, ok := FindResourceAvailableByName(zone.Resources, string(resName))
		if !ok {
			return nil, fmt.Errorf("resource %q not found in zone %q", string(resName), zone.Name)
		}

		if zoneQty.Cmp(resQty) < 0 {
			klog.Errorf("resource %q already too scarce in zone %q (target %v amount %v)", resName, zone.Name, resQty, zoneQty)
			return nil, ErrNotEnoughResources
		}
		klog.Infof("zone %q resource %q available %s allocation target %s", zone.Name, resName, zoneQty.String(), resQty.String())
		paddingQty := zoneQty.DeepCopy()
		paddingQty.Sub(resQty)
		paddingRes[resName] = paddingQty
	}

	return paddingRes, nil
}

func AllowAll(res *corev1.ResourceList) {
	return
}

func DropHostLevelRes(res *corev1.ResourceList) {
	klog.Info("drop host level resources")
	delete(*res, corev1.ResourceEphemeralStorage)
}
func SaturateNodeUntilLeft(nrtInfo nrtv1alpha2.NodeResourceTopology, requiredRes corev1.ResourceList) (map[string]corev1.ResourceList, error) {
	//TODO: support splitting the requiredRes on multiple numas
	//corrently the function deducts the requiredRes from the first Numa

	paddingRes := make(map[string]corev1.ResourceList)

	zeroRes := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("0"),
		corev1.ResourceMemory: resource.MustParse("0"),
	}
	var zonePadRes corev1.ResourceList
	var err error
	for ind, zone := range nrtInfo.Zones {
		if ind == 0 {
			zonePadRes, err = SaturateZoneUntilLeft(zone, zeroRes, DropHostLevelRes)
		} else {
			zonePadRes, err = SaturateZoneUntilLeft(zone, requiredRes, DropHostLevelRes)
		}
		if err != nil {
			klog.Errorf(fmt.Sprintf("could not make padding pod for zone %q leaving 0 resources available.", zone.Name))
			return nil, err
		}
		klog.Infof("Padding resources for zone %q: %s", zone.Name, e2ereslist.ToString(zonePadRes))
		paddingRes[zone.Name] = zonePadRes
	}

	return paddingRes, nil
}

func checkEqualResourcesInfo(nodeName, zoneName string, resourcesInitial, resourcesUpdated []nrtv1alpha2.ResourceInfo) (bool, string, error) {
	for _, res := range resourcesInitial {
		initialQty := res.Available
		updatedQty, ok := FindResourceAvailableByName(resourcesUpdated, res.Name)
		if !ok {
			return false, res.Name, fmt.Errorf("resource %q not found in the updated set", res.Name)
		}
		if initialQty.Cmp(updatedQty) != 0 {
			klog.Infof("node %q zone %q resource %q initial=%s updated=%s", nodeName, zoneName, res.Name, initialQty.String(), updatedQty.String())
			return false, res.Name, nil
		}
	}
	return true, "", nil
}

func checkConsumedResourcesAtLeast(resourcesInitial, resourcesUpdated []nrtv1alpha2.ResourceInfo, required corev1.ResourceList, podQoS corev1.PodQOSClass) (bool, error) {
	for resName, resQty := range required {
		if podQoS != corev1.PodQOSGuaranteed && (resName == corev1.ResourceCPU || resName == corev1.ResourceMemory) {
			klog.Infof("skip accounting for resource %q as consumed resource in NRT because the pod is of QoS %q", resName, podQoS)
			continue
		}
		initialQty, ok := FindResourceAvailableByName(resourcesInitial, string(resName))
		if !ok {
			return false, fmt.Errorf("resource %q not found in the initial set", string(resName))
		}
		expectedQty := initialQty.DeepCopy()
		expectedQty.Sub(resQty)

		updatedQty, ok := FindResourceAvailableByName(resourcesUpdated, string(resName))
		if !ok {
			return false, fmt.Errorf("resource %q not found in the updated set", string(resName))
		}

		ret := updatedQty.Cmp(expectedQty)
		if ret > 0 {
			klog.Infof("available quantity for resource %q is greater than expected. expected=%s actual=%s", resName, expectedQty.String(), updatedQty.String())
			return false, nil
		}
		klog.Infof("+- resource consumption: %s (consumed %s): expected available [%v] updated available [%v]", resName, resQty.String(), expectedQty.String(), updatedQty.String())
	}
	return true, nil
}

func AccumulateNames(nrts []nrtv1alpha2.NodeResourceTopology) sets.Set[string] {
	nodeNames := sets.New[string]()
	for _, nrt := range nrts {
		nodeNames.Insert(nrt.Name)
	}
	return nodeNames
}

func FilterByTopologyManagerPolicy(nrts []nrtv1alpha2.NodeResourceTopology, tmPolicy string) []nrtv1alpha2.NodeResourceTopology {
	return filterByAttribute(nrts, nrtv1alpha2.AttributeInfo{Name: e2enrt.TopologyManagerPolicyAttribute, Value: tmPolicy})
}

func FilterByTopologyManagerScope(nrts []nrtv1alpha2.NodeResourceTopology, tmScope string) []nrtv1alpha2.NodeResourceTopology {
	return filterByAttribute(nrts, nrtv1alpha2.AttributeInfo{Name: e2enrt.TopologyManagerScopeAttribute, Value: tmScope})
}

func FilterByTopologyManagerPolicyAndScope(nrts []nrtv1alpha2.NodeResourceTopology, tmPolicy, tmScope string) []nrtv1alpha2.NodeResourceTopology {
	return FilterByTopologyManagerScope(FilterByTopologyManagerPolicy(nrts, tmPolicy), tmScope)
}

func filterByAttribute(nrts []nrtv1alpha2.NodeResourceTopology, attrInfo nrtv1alpha2.AttributeInfo) []nrtv1alpha2.NodeResourceTopology {
	ret := []nrtv1alpha2.NodeResourceTopology{}
	for _, nrt := range nrts {
		attr, ok := attribute.Get(nrt.Attributes, attrInfo.Name)
		if !ok {
			klog.Errorf("SKIP: node %q doesn't have required attribute %s", nrt.Name, attrInfo.Name)
			continue
		}
		if attr.Value != attrInfo.Value {
			klog.Warningf("SKIP: node %q doesn't wrong attribute value actual:%q, expected: %q", nrt.Name, attr.Value, attrInfo.Value)
			continue
		}
		ret = append(ret, nrt)
	}
	return ret
}

func FilterZoneCountEqual(nrts []nrtv1alpha2.NodeResourceTopology, count int) []nrtv1alpha2.NodeResourceTopology {
	ret := []nrtv1alpha2.NodeResourceTopology{}
	for _, nrt := range nrts {
		if len(nrt.Zones) != count {
			klog.Warningf("SKIP: node %q has %d zones (desired %d)", nrt.Name, len(nrt.Zones), count)
			continue
		}
		klog.Infof("ADD : node %q has %d zones (desired %d)", nrt.Name, len(nrt.Zones), count)
		ret = append(ret, nrt)
	}
	return ret
}

func FilterOnlyNUMAAffineResources(rl corev1.ResourceList, tag string) corev1.ResourceList {
	res := make(corev1.ResourceList)
	for resName, resQty := range rl {
		if resName == corev1.ResourceStorage || resName == corev1.ResourceEphemeralStorage {
			klog.Infof("%s: resource %q is host-local (hostlevel) not numa-local", tag, resName)
			continue
		}
		res[resName] = resQty
	}
	return res
}

func FilterAnyZoneMatchingResources(nrts []nrtv1alpha2.NodeResourceTopology, requests corev1.ResourceList) []nrtv1alpha2.NodeResourceTopology {
	reqStr := e2ereslist.ToString(requests)
	ret := []nrtv1alpha2.NodeResourceTopology{}
	for _, nrt := range nrts {
		matches := 0
		for _, zone := range nrt.Zones {
			klog.Infof(" ----> node %q zone %q provides %s request %s", nrt.Name, zone.Name, e2ereslist.ToString(AvailableFromZone(zone)), reqStr)
			if !ResourceInfoMatchesRequest(zone.Resources, requests) {
				continue
			}
			matches++
		}
		if matches == 0 {
			klog.Warningf("SKIP: node %q can't provide %s", nrt.Name, reqStr)
			continue
		}
		klog.Infof("ADD : node %q provides at least %s", nrt.Name, reqStr)
		ret = append(ret, nrt)
	}
	return ret
}

func FilterAnyNodeMatchingResources(nrts []nrtv1alpha2.NodeResourceTopology, requests corev1.ResourceList) []nrtv1alpha2.NodeResourceTopology {
	reqStr := e2ereslist.ToString(requests)
	ret := []nrtv1alpha2.NodeResourceTopology{}
	for _, nrt := range nrts {
		nodeRes, err := accumulateNodeAvailableResources(nrt, "initial")
		if err != nil {
			klog.Errorf("ERROR: %v", err)
			continue
		}
		klog.Infof(" ----> node %q provides %s request %s", nrt.Name, e2ereslist.ToString(ResourceInfoListToResourceList(nodeRes)), reqStr)
		// abuse the ResourceInfoMatchesRequest for checking the complete node's resources
		if !ResourceInfoMatchesRequest(nodeRes, requests) {
			klog.Warningf("SKIP: node %q can't provide %s", nrt.Name, reqStr)
			continue
		}

		klog.Infof("ADD : node %q provides at least %s", nrt.Name, reqStr)
		ret = append(ret, nrt)
	}
	return ret
}

func FindFromList(nrts []nrtv1alpha2.NodeResourceTopology, name string) (*nrtv1alpha2.NodeResourceTopology, error) {
	for idx := 0; idx < len(nrts); idx++ {
		if nrts[idx].Name == name {
			return &nrts[idx], nil
		}
	}
	return nil, fmt.Errorf("failed to find NRT for %q", name)
}

// AvailableFromZone returns a ResourceList of all available resources under the zone
func AvailableFromZone(z nrtv1alpha2.Zone) corev1.ResourceList {
	rl := corev1.ResourceList{}

	for _, ri := range z.Resources {
		rl[corev1.ResourceName(ri.Name)] = ri.Available
	}
	return rl
}

func ResourceInfoMatchesRequest(resources []nrtv1alpha2.ResourceInfo, requests corev1.ResourceList) bool {
	for resName, resQty := range requests {
		if !ResourceInfoProviding(resources, string(resName), resQty, true) {
			return false
		}
	}
	return true
}

func ResourceInfoProviding(resources []nrtv1alpha2.ResourceInfo, resName string, resQty resource.Quantity, onEqual bool) bool {
	zoneQty, ok := FindResourceAvailableByName(resources, resName)
	if !ok {
		return false
	}
	cmpRes := zoneQty.Cmp(resQty)
	if cmpRes < 0 {
		return false
	}
	if cmpRes == 0 {
		return onEqual
	}
	return true
}

func findZoneByName(nrt nrtv1alpha2.NodeResourceTopology, zoneName string) (*nrtv1alpha2.Zone, error) {
	for idx := 0; idx < len(nrt.Zones); idx++ {
		if nrt.Zones[idx].Name == zoneName {
			return &nrt.Zones[idx], nil
		}
	}
	return nil, fmt.Errorf("cannot find zone %q", zoneName)
}

func FindResourceAvailableByName(resources []nrtv1alpha2.ResourceInfo, name string) (resource.Quantity, bool) {
	for _, resource := range resources {
		if resource.Name != name {
			continue
		}
		return resource.Available, true
	}
	return *resource.NewQuantity(0, resource.DecimalSI), false
}

func FindResourceAllocatableByName(resources []nrtv1alpha2.ResourceInfo, name string) (resource.Quantity, bool) {
	for _, resource := range resources {
		if resource.Name != name {
			continue
		}
		return resource.Allocatable, true
	}
	return *resource.NewQuantity(0, resource.DecimalSI), false
}

func GetMaxAllocatableResourceNumaLevel(nrtInfo nrtv1alpha2.NodeResourceTopology, resName corev1.ResourceName) resource.Quantity {
	var maxAllocatable resource.Quantity

	// Finding the maximum allocatable resources of a resource type across all zones
	for _, zone := range nrtInfo.Zones {
		zoneQty, ok := FindResourceAllocatableByName(zone.Resources, resName.String())
		if !ok {
			continue
		}
		if zoneQty.Cmp(maxAllocatable) > 0 {
			maxAllocatable = zoneQty
		}
	}
	return maxAllocatable
}

func ResourceInfoListToResourceList(ri nrtv1alpha2.ResourceInfoList) corev1.ResourceList {
	rl := corev1.ResourceList{}

	for _, res := range ri {
		rl[corev1.ResourceName(res.Name)] = res.Available
	}
	return rl
}

func FilterAnyZoneProvidingResourcesAtMost(nrts []nrtv1alpha2.NodeResourceTopology, resourceName string, maxResources int64, maxZones int) []nrtv1alpha2.NodeResourceTopology {
	maxQty := *resource.NewQuantity(maxResources, resource.DecimalSI)
	ret := []nrtv1alpha2.NodeResourceTopology{}
	for _, nrt := range nrts {
		matches := 0
		for _, zone := range nrt.Zones {
			klog.Infof(" ----> node %q zone %q provides %s request resource %q", nrt.Name, zone.Name, e2ereslist.ToString(AvailableFromZone(zone)), resourceName)
			if !ResourceInfoProvidingAtMost(zone.Resources, resourceName, maxQty) {
				continue
			}
			matches++
		}
		if matches == 0 {
			klog.Warningf("SKIP: node %q does NOT provide %q at all!", nrt.Name, resourceName)
			continue
		}
		if matches > maxZones {
			klog.Warningf("SKIP: node %q provides %q on %d zones (looking max=%d)", nrt.Name, resourceName, matches, maxZones)
			continue
		}
		klog.Infof("ADD : node %q provides %q on %d/%d zones", nrt.Name, resourceName, matches, len(nrt.Zones))
		ret = append(ret, nrt)
	}
	return ret
}

func ResourceInfoProvidingAtMost(resources []nrtv1alpha2.ResourceInfo, resName string, resQty resource.Quantity) bool {
	zoneQty, ok := FindResourceAvailableByName(resources, resName)
	klog.Infof("  +--> checking if resources include %q in (0, %s] (zoneQty=%s found=%v)", resName, resQty.String(), zoneQty.String(), ok)
	if !ok {
		return false
	}
	if zoneQty.IsZero() {
		return false
	}
	if zoneQty.Cmp(resQty) < 0 {
		return false
	}
	return true
}

func EqualNRTListsItems(nrtListA, nrtListB nrtv1alpha2.NodeResourceTopologyList) (bool, error) {
	for _, nrtA := range nrtListA.Items {
		nrtB, err := FindFromList(nrtListB.Items, nrtA.Name)
		if err != nil {
			return false, fmt.Errorf("failed to find NRT data for node %q in both lists: %v", nrtA.Name, err)
		}
		rebootTest := false
		ok, err := e2enrt.EqualZones(nrtB.Zones, nrtA.Zones, rebootTest)
		if err != nil {
			return false, fmt.Errorf("failed while comparing %q NRT zones: %v", nrtA.Name, err)
		}
		if !ok {
			klog.Infof("NRT mismatch for node %q", nrtA.Name)
			return false, nil
		}
	}
	return true, nil
}
