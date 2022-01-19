/*
Copyright 2022 The Kubernetes Authors.

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

package noderesourcetopologies

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"
)

func GetUpdated(cli client.Client, ref nrtv1alpha1.NodeResourceTopologyList, timeout time.Duration) (nrtv1alpha1.NodeResourceTopologyList, error) {
	var updatedNrtList nrtv1alpha1.NodeResourceTopologyList
	err := wait.Poll(1*time.Second, timeout, func() (bool, error) {
		err := cli.List(context.TODO(), &updatedNrtList)
		if err != nil {
			klog.Errorf("cannot get the NRT List: %v", err)
			return false, err
		}
		klog.Infof("NRT List current ResourceVersion %s reference %s", updatedNrtList.ListMeta.ResourceVersion, ref.ListMeta.ResourceVersion)
		return (updatedNrtList.ListMeta.ResourceVersion != ref.ListMeta.ResourceVersion), nil
	})
	return updatedNrtList, err
}

func CheckEqualAvailableResources(nrtInitial, nrtUpdated nrtv1alpha1.NodeResourceTopology) (bool, error) {
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
	return true, nil
}

func CheckZoneConsumedResourcesAtLeast(nrtInitial, nrtUpdated nrtv1alpha1.NodeResourceTopology, required corev1.ResourceList) (string, error) {
	for idx := 0; idx < len(nrtInitial.Zones); idx++ {
		zoneInitial := &nrtInitial.Zones[idx] // shortcut
		zoneUpdated, err := findZoneByName(nrtUpdated, zoneInitial.Name)
		if err != nil {
			klog.Errorf("missing updated zone %q: %v", zoneInitial.Name, err)
			return "", err
		}
		ok, err := checkConsumedResourcesAtLeast(zoneInitial.Resources, zoneUpdated.Resources, required)
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

func checkEqualResourcesInfo(nodeName, zoneName string, resourcesInitial, resourcesUpdated []nrtv1alpha1.ResourceInfo) (bool, string, error) {
	for _, res := range resourcesInitial {
		initialQty := res.Available
		updatedQty, ok := findResourceAvailableByName(resourcesUpdated, res.Name)
		if !ok {
			return false, res.Name, fmt.Errorf("resource %q not found in the updated set", res.Name)
		}
		if initialQty.Cmp(updatedQty) != 0 {
			klog.Infof("node %q zone %q resource %q initial=%v updated=%v", nodeName, zoneName, res.Name, initialQty, updatedQty)
			return false, res.Name, nil
		}
	}
	return true, "", nil
}

func checkConsumedResourcesAtLeast(resourcesInitial, resourcesUpdated []nrtv1alpha1.ResourceInfo, required corev1.ResourceList) (bool, error) {
	for resName, resQty := range required {
		initialQty, ok := findResourceAvailableByName(resourcesInitial, string(resName))
		if !ok {
			return false, fmt.Errorf("resource %q not found in the initial set", string(resName))
		}
		updatedQty, ok := findResourceAvailableByName(resourcesUpdated, string(resName))
		if !ok {
			return false, fmt.Errorf("resource %q not found in the updated set", string(resName))
		}
		expectedQty := initialQty.DeepCopy()
		expectedQty.Sub(resQty)
		ret := updatedQty.Cmp(expectedQty)
		if ret > 0 {
			return false, nil
		}
	}
	return true, nil
}

func ResourcesFromGuaranteedPod(pod corev1.Pod) corev1.ResourceList {
	res := make(corev1.ResourceList)
	for idx := 0; idx < len(pod.Spec.Containers); idx++ {
		cnt := &pod.Spec.Containers[idx] // shortcut
		for resName, resQty := range cnt.Resources.Limits {
			qty := res[resName]
			qty.Add(resQty)
			res[resName] = qty
		}
	}
	return res
}

func AccumulateNames(nrts []nrtv1alpha1.NodeResourceTopology) sets.String {
	nodeNames := sets.NewString()
	for _, nrt := range nrts {
		nodeNames.Insert(nrt.Name)
	}
	return nodeNames
}

func FilterTopologyManagerPolicy(nrts []nrtv1alpha1.NodeResourceTopology, tmPolicy nrtv1alpha1.TopologyManagerPolicy) []nrtv1alpha1.NodeResourceTopology {
	ret := []nrtv1alpha1.NodeResourceTopology{}
	for _, nrt := range nrts {
		if !contains(nrt.TopologyPolicies, string(tmPolicy)) {
			klog.Warningf("SKIP: node %q doesn't support topology manager policy %q", nrt.Name, string(tmPolicy))
			continue
		}
		klog.Infof("ADD : node %q supports topology manager policy %q", nrt.Name, string(tmPolicy))
		ret = append(ret, nrt)
	}
	return ret
}

func FilterAnyZoneMatchingResources(nrts []nrtv1alpha1.NodeResourceTopology, requests corev1.ResourceList) []nrtv1alpha1.NodeResourceTopology {
	ret := []nrtv1alpha1.NodeResourceTopology{}
	for _, nrt := range nrts {
		matches := 0
		for _, zone := range nrt.Zones {
			if !zoneResourcesMatchesRequest(zone.Resources, requests) {
				continue
			}
			klog.Infof(" ----> node %q zone %q provides at least %#v", nrt.Name, zone.Name, requests)
			matches++
		}
		if matches == 0 {
			klog.Warningf("SKIP: node %q can't provide %#v", nrt.Name, requests)
			continue
		}
		klog.Infof("ADD : node %q provides at least %#v", nrt.Name, requests)
		ret = append(ret, nrt)
	}
	return ret
}

func FindFromList(nrts []nrtv1alpha1.NodeResourceTopology, name string) (*nrtv1alpha1.NodeResourceTopology, error) {
	for idx := 0; idx < len(nrts); idx++ {
		if nrts[idx].Name == name {
			return &nrts[idx], nil
		}
	}
	return nil, fmt.Errorf("failed to find NRT for %q", name)
}

func findZoneByName(nrt nrtv1alpha1.NodeResourceTopology, zoneName string) (*nrtv1alpha1.Zone, error) {
	for idx := 0; idx < len(nrt.Zones); idx++ {
		if nrt.Zones[idx].Name == zoneName {
			return &nrt.Zones[idx], nil
		}
	}
	return nil, fmt.Errorf("cannot find zone %q", zoneName)
}

func zoneResourcesMatchesRequest(resources []nrtv1alpha1.ResourceInfo, requests corev1.ResourceList) bool {
	for resName, resQty := range requests {
		zoneQty, ok := findResourceAvailableByName(resources, string(resName))
		if !ok {
			return false
		}
		if zoneQty.Cmp(resQty) < 0 {
			return false
		}
	}
	return true
}

func findResourceAvailableByName(resources []nrtv1alpha1.ResourceInfo, name string) (resource.Quantity, bool) {
	for _, resource := range resources {
		if resource.Name != name {
			continue
		}
		return resource.Available, true
	}
	return *resource.NewQuantity(0, resource.DecimalSI), false
}

func contains(items []string, st string) bool {
	for _, item := range items {
		if item == st {
			return true
		}
	}
	return false
}
