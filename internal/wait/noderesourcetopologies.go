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

package wait

import (
	"context"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	nrtv1alpha2attr "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2/helper/attribute"
	"github.com/k8stopologyawareschedwg/podfingerprint"
)

type PFPCount struct {
	Value string
	Count int
}

func (pc PFPCount) String() string {
	return fmt.Sprintf("PFP=%s count=%d", pc.Value, pc.Count)
}

// map nodeName -> PFPCount
type PFPState map[string]PFPCount

func (ps PFPState) Len() int {
	return len(ps)
}

func (ps PFPState) CountReady(threshold int) int {
	ready := 0
	for _, count := range ps {
		if count.Count >= threshold {
			ready++
		}
	}
	return ready
}

func (ps PFPState) AllReady(threshold int) bool {
	for _, count := range ps {
		if count.Count < threshold {
			return false
		}
	}
	return true
}

func (ps PFPState) Observe(nodeName, pfpValue string) int {
	count, ok := ps[nodeName]
	if !ok || count.Value != pfpValue {
		ps[nodeName] = PFPCount{
			Value: pfpValue,
			Count: 1,
		}
		return 1
	}
	count.Count++
	ps[nodeName] = count
	return count.Count
}

type NRTShouldIgnoreFunc func(nrt *nrtv1alpha2.NodeResourceTopology) bool

func NRTIgnoreNothing(nrt *nrtv1alpha2.NodeResourceTopology) bool {
	return false
}

func (wt Waiter) ForNodeResourceTopologiesSettled(ctx context.Context, threshold int, nrtShouldIgnore NRTShouldIgnoreFunc) (nrtv1alpha2.NodeResourceTopologyList, error) {
	pfpState := make(PFPState)
	nrtList := nrtv1alpha2.NodeResourceTopologyList{}

	err := k8swait.PollImmediate(wt.PollInterval, wt.PollTimeout, func() (bool, error) {
		klog.Infof("waiting for NRT to stabilize; observed nodes=%d ready=%d", pfpState.Len(), pfpState.CountReady(threshold))

		err := wt.Cli.List(context.TODO(), &nrtList)
		if err != nil {
			return false, err
		}
		for idx := range nrtList.Items {
			nrt := &nrtList.Items[idx]

			if nrtShouldIgnore(nrt) {
				klog.Warningf("skipping NRT %q because of callback", nrt.Name)
				continue
			}

			attr, ok := nrtv1alpha2attr.Get(nrt.Attributes, podfingerprint.Attribute)
			if !ok {
				return false, fmt.Errorf("NRT %s missing PFP attribute", nrt.Name)
			}
			cnt := pfpState.Observe(nrt.Name, attr.Value)
			klog.Infof("-> waiting for NRT to stabilize; node=%s value=%s count=%d threshold=%d", nrt.Name, attr.Value, cnt, threshold)
		}
		return pfpState.AllReady(threshold), nil
	})

	klog.Infof("done waiting for NRT to stabilize; observed nodes=%d ready=%d", pfpState.Len(), pfpState.CountReady(threshold))
	return nrtList, err
}

func (wt Waiter) ForNodeResourceTopologiesEqualTo(ctx context.Context, nrtListReference *nrtv1alpha2.NodeResourceTopologyList, nrtShouldIgnore NRTShouldIgnoreFunc) (nrtv1alpha2.NodeResourceTopologyList, error) {
	var updatedNrtList nrtv1alpha2.NodeResourceTopologyList
	klog.Infof("Waiting up to %v to have %d NRT objects restored", wt.PollTimeout, len(nrtListReference.Items))
	err := k8swait.Poll(wt.PollInterval, wt.PollTimeout, func() (bool, error) {
		err := wt.Cli.List(ctx, &updatedNrtList)
		if err != nil {
			klog.Errorf("cannot get the NRT List: %v", err)
			return false, err
		}
		for idx := range nrtListReference.Items {
			referenceNrt := &nrtListReference.Items[idx]

			if nrtShouldIgnore(referenceNrt) {
				klog.Warningf("skipping NRT %q because of callback", referenceNrt.Name)
				continue
			}

			updatedNrt, err := findNRTByName(updatedNrtList.Items, referenceNrt.Name)
			if err != nil {
				klog.Errorf("missing NRT for %s: %v", referenceNrt.Name, err)
				return false, err
			}
			if !isNRTEqual(*referenceNrt, *updatedNrt) {
				klog.Warningf("NRT for %s does not match reference", referenceNrt.Name)
				return false, err
			}
			klog.Infof("NRT for %s matches reference", referenceNrt.Name)
		}
		klog.Infof("Matched reference for all %d NRT objects", len(nrtListReference.Items))
		return true, nil
	})
	return updatedNrtList, err
}

func (wt Waiter) ForNodeResourceTopologyToHave(ctx context.Context, nrtName string, haveResourceFunc func(resInfo nrtv1alpha2.ResourceInfo) bool) (nrtv1alpha2.NodeResourceTopology, error) {
	updatedNrt := nrtv1alpha2.NodeResourceTopology{}
	nrtKey := client.ObjectKey{Name: nrtName}
	klog.Infof("Waiting up to %v for NRT %q to expose the desired resource", wt.PollTimeout, nrtName)
	err := k8swait.Poll(wt.PollInterval, wt.PollTimeout, func() (bool, error) {
		err := wt.Cli.Get(ctx, nrtKey, &updatedNrt)
		if err != nil {
			klog.Errorf("cannot get the NRT %q: %v", nrtName, err)
			return false, err
		}
		zonesOk := 0
		for _, zone := range updatedNrt.Zones {
			for _, resInfo := range zone.Resources {
				if haveResourceFunc(resInfo) {
					klog.Infof("found resources in zone %q for NRT %q", zone.Name, updatedNrt.Name)
					zonesOk++
					break
				}
			}
		}
		klog.Infof("resource check for NRT %q: %d/%d zone expose the required resources", updatedNrt.Name, zonesOk, len(updatedNrt.Zones))
		return (zonesOk == len(updatedNrt.Zones)), nil
	})
	return updatedNrt, err
}

func findNRTByName(nrts []nrtv1alpha2.NodeResourceTopology, name string) (*nrtv1alpha2.NodeResourceTopology, error) {
	for idx := 0; idx < len(nrts); idx++ {
		if nrts[idx].Name == name {
			return &nrts[idx], nil
		}
	}
	return nil, fmt.Errorf("failed to find NRT for %q", name)
}

func isNRTEqual(initialNrt, updatedNrt nrtv1alpha2.NodeResourceTopology) bool {
	// very cheap test to rule out false negatives
	if updatedNrt.ObjectMeta.ResourceVersion == initialNrt.ObjectMeta.ResourceVersion {
		klog.Warningf("NRT %q resource version didn't change", initialNrt.Name)
		return true
	}
	equalZones := apiequality.Semantic.DeepEqual(initialNrt.Zones, updatedNrt.Zones)
	if !equalZones {
		klog.Infof("NRT %q change: updated to %s", initialNrt.Name, intnrt.ToString(updatedNrt))
	}
	return equalZones
}
