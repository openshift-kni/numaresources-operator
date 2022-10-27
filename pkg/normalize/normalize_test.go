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

package normalize

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
)

func TestNormalizeNodeGroupConfig(t *testing.T) {
	type testCase struct {
		name         string
		nodeGroup    nropv1alpha1.NodeGroup
		expectedConf nropv1alpha1.NodeGroupConfig
	}

	testcases := []testCase{
		{
			name:         "nil conf",
			nodeGroup:    nropv1alpha1.NodeGroup{},
			expectedConf: nropv1alpha1.DefaultNodeGroupConfig(),
		},
		{
			name: "4.11 disable PFP",
			nodeGroup: nropv1alpha1.NodeGroup{
				DisablePodsFingerprinting: boolPtr(true),
			},
			expectedConf: makeNodeGroupConfig(
				nropv1alpha1.PodsFingerprintingDisabled,
				nropv1alpha1.InfoRefreshPeriodicAndEvents,
				10*time.Second,
			),
		},
		{
			name: "defaults passthrough",
			nodeGroup: nropv1alpha1.NodeGroup{
				Config: makeNodeGroupConfigPtr(
					nropv1alpha1.PodsFingerprintingEnabled,
					nropv1alpha1.InfoRefreshPeriodicAndEvents,
					10*time.Second,
				),
			},
			expectedConf: nropv1alpha1.DefaultNodeGroupConfig(),
		},
		{
			name: "conf disable PFP",
			nodeGroup: nropv1alpha1.NodeGroup{
				Config: makeNodeGroupConfigPtr(
					nropv1alpha1.PodsFingerprintingDisabled,
					nropv1alpha1.InfoRefreshPeriodicAndEvents,
					10*time.Second,
				),
			},
			expectedConf: makeNodeGroupConfig(
				nropv1alpha1.PodsFingerprintingDisabled,
				nropv1alpha1.InfoRefreshPeriodicAndEvents,
				10*time.Second,
			),
		},
		{
			name: "conf tuning",
			nodeGroup: nropv1alpha1.NodeGroup{
				Config: makeNodeGroupConfigPtr(
					nropv1alpha1.PodsFingerprintingEnabled,
					nropv1alpha1.InfoRefreshPeriodic,
					20*time.Second,
				),
			},
			expectedConf: makeNodeGroupConfig(
				nropv1alpha1.PodsFingerprintingEnabled,
				nropv1alpha1.InfoRefreshPeriodic,
				20*time.Second,
			),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.nodeGroup.DeepCopy()
			NodeGroupConfig(got)
			if got.Config == nil {
				t.Fatalf("nil config! %#v", got)
			}
			if !cmp.Equal(*got.Config, tc.expectedConf) {
				t.Errorf("config differs, got=%#v expected=%#v", *got.Config, tc.expectedConf)
			}
		})
	}
}

func TestNormalizeNodeGroupConfigTree(t *testing.T) {
	type testCase struct {
		name          string
		trees         []machineconfigpools.NodeGroupTree
		expectedConfs []nropv1alpha1.NodeGroupConfig
	}

	testcases := []testCase{
		{
			name: "set defaults - full",
			trees: []machineconfigpools.NodeGroupTree{
				{
					NodeGroup: &nropv1alpha1.NodeGroup{},
				},
				{
					NodeGroup: &nropv1alpha1.NodeGroup{},
				},
			},
			expectedConfs: []nropv1alpha1.NodeGroupConfig{
				nropv1alpha1.DefaultNodeGroupConfig(),
				nropv1alpha1.DefaultNodeGroupConfig(),
			},
		},
		{
			name: "partial 4.11 disable PFP",
			trees: []machineconfigpools.NodeGroupTree{
				{
					NodeGroup: &nropv1alpha1.NodeGroup{
						DisablePodsFingerprinting: boolPtr(true),
					},
				},
				{
					NodeGroup: &nropv1alpha1.NodeGroup{},
				},
			},
			expectedConfs: []nropv1alpha1.NodeGroupConfig{
				makeNodeGroupConfig(
					nropv1alpha1.PodsFingerprintingDisabled,
					nropv1alpha1.InfoRefreshPeriodicAndEvents,
					10*time.Second,
				),
				nropv1alpha1.DefaultNodeGroupConfig(),
			},
		},
		{
			name: "partial defaults passthrough",
			trees: []machineconfigpools.NodeGroupTree{
				{
					NodeGroup: &nropv1alpha1.NodeGroup{},
				},
				{
					NodeGroup: &nropv1alpha1.NodeGroup{
						Config: makeNodeGroupConfigPtr(
							nropv1alpha1.PodsFingerprintingEnabled,
							nropv1alpha1.InfoRefreshPeriodicAndEvents,
							10*time.Second,
						),
					},
				},
			},
			expectedConfs: []nropv1alpha1.NodeGroupConfig{
				nropv1alpha1.DefaultNodeGroupConfig(),
				nropv1alpha1.DefaultNodeGroupConfig(),
			},
		},
		{
			name: "total defaults passthrough",
			trees: []machineconfigpools.NodeGroupTree{
				{
					NodeGroup: &nropv1alpha1.NodeGroup{
						Config: makeNodeGroupConfigPtr(
							nropv1alpha1.PodsFingerprintingEnabled,
							nropv1alpha1.InfoRefreshPeriodicAndEvents,
							10*time.Second,
						),
					},
				},
				{
					NodeGroup: &nropv1alpha1.NodeGroup{
						Config: makeNodeGroupConfigPtr(
							nropv1alpha1.PodsFingerprintingEnabled,
							nropv1alpha1.InfoRefreshPeriodicAndEvents,
							10*time.Second,
						),
					},
				},
			},
			expectedConfs: []nropv1alpha1.NodeGroupConfig{
				nropv1alpha1.DefaultNodeGroupConfig(),
				nropv1alpha1.DefaultNodeGroupConfig(),
			},
		},
		{
			name: "partial tuning",
			trees: []machineconfigpools.NodeGroupTree{
				{
					NodeGroup: &nropv1alpha1.NodeGroup{},
				},
				{
					NodeGroup: &nropv1alpha1.NodeGroup{
						Config: makeNodeGroupConfigPtr(
							nropv1alpha1.PodsFingerprintingEnabled,
							nropv1alpha1.InfoRefreshPeriodic,
							21*time.Second,
						),
					},
				},
			},
			expectedConfs: []nropv1alpha1.NodeGroupConfig{
				nropv1alpha1.DefaultNodeGroupConfig(),
				makeNodeGroupConfig(
					nropv1alpha1.PodsFingerprintingEnabled,
					nropv1alpha1.InfoRefreshPeriodic,
					21*time.Second,
				),
			},
		},
		{
			name: "totall tuning",
			trees: []machineconfigpools.NodeGroupTree{
				{
					NodeGroup: &nropv1alpha1.NodeGroup{
						Config: makeNodeGroupConfigPtr(
							nropv1alpha1.PodsFingerprintingDisabled,
							nropv1alpha1.InfoRefreshEvents,
							0*time.Second,
						),
					},
				},
				{
					NodeGroup: &nropv1alpha1.NodeGroup{
						Config: makeNodeGroupConfigPtr(
							nropv1alpha1.PodsFingerprintingEnabled,
							nropv1alpha1.InfoRefreshPeriodic,
							21*time.Second,
						),
					},
				},
			},
			expectedConfs: []nropv1alpha1.NodeGroupConfig{
				makeNodeGroupConfig(
					nropv1alpha1.PodsFingerprintingDisabled,
					nropv1alpha1.InfoRefreshEvents,
					0*time.Second,
				),
				makeNodeGroupConfig(
					nropv1alpha1.PodsFingerprintingEnabled,
					nropv1alpha1.InfoRefreshPeriodic,
					21*time.Second,
				),
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got := cloneTrees(tc.trees)
			NodeGroupTreesConfig(got)

			if len(got) != len(tc.expectedConfs) {
				t.Fatalf("mismatched length got=%v expected=%v", len(got), len(tc.expectedConfs))
			}

			gotConfs := make([]nropv1alpha1.NodeGroupConfig, 0, len(got))
			for _, ngt := range got {
				if ngt.NodeGroup == nil || ngt.NodeGroup.Config == nil {
					t.Fatalf("unexpected nil: %s", jsonDump(ngt))
				}
				gotConfs = append(gotConfs, *ngt.NodeGroup.Config)
			}

			if !cmp.Equal(gotConfs, tc.expectedConfs) {
				t.Errorf("config differs:\ngot=%s\nexpected=%s", jsonDump(gotConfs), jsonDump(tc.expectedConfs))
			}
		})
	}

}

func jsonDump(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "<JSON MARSHAL ERROR>"
	}
	return string(data)
}

func cloneTrees(trees []machineconfigpools.NodeGroupTree) []machineconfigpools.NodeGroupTree {
	ret := make([]machineconfigpools.NodeGroupTree, 0, len(trees))
	for _, ngt := range trees {
		ret = append(ret, ngt.Clone())
	}
	return ret
}

func boolPtr(b bool) *bool {
	return &b
}

func makeNodeGroupConfigPtr(podsFp nropv1alpha1.PodsFingerprintingMode, refMode nropv1alpha1.InfoRefreshMode, dur time.Duration) *nropv1alpha1.NodeGroupConfig {
	conf := makeNodeGroupConfig(podsFp, refMode, dur)
	return &conf
}

func makeNodeGroupConfig(podsFp nropv1alpha1.PodsFingerprintingMode, refMode nropv1alpha1.InfoRefreshMode, dur time.Duration) nropv1alpha1.NodeGroupConfig {
	period := metav1.Duration{
		Duration: dur,
	}
	return nropv1alpha1.NodeGroupConfig{
		PodsFingerprinting: &podsFp,
		InfoRefreshPeriod:  &period,
		InfoRefreshMode:    &refMode,
	}
}
