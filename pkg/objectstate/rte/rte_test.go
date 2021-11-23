package rte

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"

	"github.com/google/go-cmp/cmp"
	"github.com/openshift-kni/numaresources-operator/pkg/tmpolicy"
)

func TestUpdateDaemonSetTopologyPolicy(t *testing.T) {
	type testCase struct {
		name     string
		envVars  []v1.EnvVar
		value    string
		expected []v1.EnvVar
	}

	testCases := []testCase{
		{
			name: "environment variable exists",
			envVars: []v1.EnvVar{
				{
					Name:  "foo-42",
					Value: "something1",
				},
				{
					Name:  tmpolicy.PolicyEnv,
					Value: "something2",
				},
			},
			value: "foo",
			expected: []v1.EnvVar{
				{
					Name:  "foo-42",
					Value: "something1",
				},
				{
					Name:  tmpolicy.PolicyEnv,
					Value: "foo",
				},
			},
		},
		{
			name: "environment variable doesn't exists",
			envVars: []v1.EnvVar{
				{
					Name:  "foo-42",
					Value: "something1",
				},
			},
			value: "bar",
			expected: []v1.EnvVar{
				{
					Name:  "foo-42",
					Value: "something1",
				},
				{
					Name:  tmpolicy.PolicyEnv,
					Value: "bar",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("when %s", tc.name), func(t *testing.T) {
			UpdateDaemonSetTopologyPolicy(&tc.envVars, tc.value)
			if diff := cmp.Diff(tc.expected, tc.envVars); diff != "" {
				t.Errorf("want %v\n; got %v\n; diff %v\n", tc.expected, tc.envVars, diff)
			}
		})
	}
}

func TestUpdateDaemonSetTopologyScope(t *testing.T) {
	type testCase struct {
		name     string
		envVars  []v1.EnvVar
		value    string
		expected []v1.EnvVar
	}

	testCases := []testCase{
		{
			name: "environment variable exists",
			envVars: []v1.EnvVar{
				{
					Name:  "bar-55",
					Value: "something1",
				},
				{
					Name:  tmpolicy.ScopeEnv,
					Value: "something2",
				},
			},
			value: "foo",
			expected: []v1.EnvVar{
				{
					Name:  "bar-55",
					Value: "something1",
				},
				{
					Name:  tmpolicy.ScopeEnv,
					Value: "foo",
				},
			},
		},
		{
			name: "environment variable doesn't exists",
			envVars: []v1.EnvVar{
				{
					Name:  "bar-55",
					Value: "something1",
				},
			},
			value: "bar",
			expected: []v1.EnvVar{
				{
					Name:  "bar-55",
					Value: "something1",
				},
				{
					Name:  tmpolicy.ScopeEnv,
					Value: "bar",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("when %s", tc.name), func(t *testing.T) {
			UpdateDaemonSetTopologyScope(&tc.envVars, tc.value)
			if diff := cmp.Diff(tc.expected, tc.envVars); diff != "" {
				t.Errorf("want %v\n; got %v\n; diff %v\n", tc.expected, tc.envVars, diff)
			}
		})
	}
}
