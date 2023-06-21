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

	"k8s.io/apimachinery/pkg/util/sets"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

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

func (wt Waiter) ForNodeResourceTopologiesSettled(ctx context.Context, threshold int, nodeNames ...string) (*nrtv1alpha2.NodeResourceTopologyList, error) {
	expectedNodes := sets.NewString(nodeNames...)
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
			if expectedNodes.Len() > 0 && !expectedNodes.Has(nrt.Name) {
				klog.Infof("-> waiting for NRT to stabilize; ignored unwanted node node=%s", nrt.Name)
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
	return &nrtList, err
}
