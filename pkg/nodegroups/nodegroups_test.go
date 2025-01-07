package nodegroups

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
)

func TestValidate(t *testing.T) {
	type testCase struct {
		name                 string
		nodeGroups           []nropv1.NodeGroup
		expectedError        bool
		expectedErrorMessage string
		platf                platform.Platform
	}

	emptyString := ""
	poolName := "poolname-test"
	config := nropv1.DefaultNodeGroupConfig()

	testCases := []testCase{
		{
			name: "both source pools are nil",
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: nil,
					PoolName:                  nil,
				},
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
				},
			},
			expectedError:        true,
			expectedErrorMessage: "missing any pool specifier",
			platf:                platform.OpenShift,
		},
		{
			name: "both source pools are set",
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
					PoolName: &poolName,
				},
			},
			expectedError:        true,
			expectedErrorMessage: "must have only a single specifier set",
			platf:                platform.OpenShift,
		},
		{
			name: "with duplicates - mcp selector",
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
				},
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
				},
			},
			expectedError:        true,
			expectedErrorMessage: "has duplicates",
			platf:                platform.OpenShift,
		},
		{
			name: "with duplicates - pool name",
			nodeGroups: []nropv1.NodeGroup{
				{
					PoolName: &poolName,
				},
				{
					PoolName: &poolName,
				},
			},
			expectedError:        true,
			expectedErrorMessage: "has duplicates",
			platf:                platform.OpenShift,
		},
		{
			name: "bad MCP selector",
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "test",
								Operator: "bad-operator",
								Values:   []string{"test"},
							},
						},
					},
				},
			},
			expectedError:        true,
			expectedErrorMessage: "not a valid label selector operator",
			platf:                platform.OpenShift,
		},
		{
			name: "correct values",
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
				},
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test1": "test1",
						},
					},
				},
				{
					PoolName: &poolName,
				},
			},
			platf: platform.OpenShift,
		},
		{
			name: "MCP selector set on Hypershift platform",
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
					PoolName: &poolName,
				},
			},
			expectedError:        true,
			expectedErrorMessage: "MachineConfigPoolSelector on Hypershift platform",
			platf:                platform.HyperShift,
		},
		{
			name: "empty PoolName on Hypershift platform",
			nodeGroups: []nropv1.NodeGroup{
				{
					Config: &config,
				},
			},
			expectedError:        true,
			expectedErrorMessage: "must specify PoolName on Hypershift platform",
			platf:                platform.HyperShift,
		},
		{
			name: "empty pool name",
			nodeGroups: []nropv1.NodeGroup{
				{
					PoolName: &emptyString,
				},
			},
			expectedError:        true,
			expectedErrorMessage: "cannot be empty",
			platf:                platform.OpenShift,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager, err := NewForPlatform(tc.platf)
			if err != nil {
				t.Errorf("failed to get manager for platform %s; err=%s", tc.platf, err.Error())
			}
			err = manager.Validate(tc.nodeGroups)
			if err == nil && tc.expectedError {
				t.Errorf("expected error, succeeded")
			}
			if err != nil && !tc.expectedError {
				t.Errorf("expected success, failed")
			}
			if tc.expectedErrorMessage != "" {
				if !strings.Contains(err.Error(), tc.expectedErrorMessage) {
					t.Errorf("unexpected error: %v (expected %q)", err, tc.expectedErrorMessage)
				}
			}
		})
	}
}
