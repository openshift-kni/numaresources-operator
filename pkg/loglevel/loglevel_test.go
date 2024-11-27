/*
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
 *
 * Copyright 2022 Red Hat, Inc.
 */

package loglevel

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/assert"

	rteconfiguration "github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/config"
)

func TestToKlog(t *testing.T) {
	testCases := []struct {
		in  operatorv1.LogLevel
		out klog.Level
	}{
		{
			in:  operatorv1.Normal,
			out: klog.Level(2),
		},
		{
			in:  operatorv1.Debug,
			out: klog.Level(4),
		},
		{
			in:  operatorv1.Trace,
			out: klog.Level(6),
		},
		{
			in:  operatorv1.TraceAll,
			out: klog.Level(8),
		},
		{
			in:  operatorv1.LogLevel(""),
			out: klog.Level(2),
		},
	}
	for _, tc := range testCases {
		kLog := ToKlog(tc.in)
		if kLog != tc.out {
			t.Errorf("in LogLevel: %v; expected klog level %v to be equal to klog level %v", tc.in, kLog, tc.out)
		}
	}
}

func TestUpdatePodSpec(t *testing.T) {
	podSpec := &corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name: "foo",
				Args: []string{
					"bar1=value1",
					"bar2=value2",
				},
			},
		},
	}

	args := podSpec.Containers[0].Args

	testCases := []struct {
		in           operatorv1.LogLevel
		expectedArgs []string
	}{
		{
			in:           operatorv1.Normal,
			expectedArgs: append(getArgsCopy(args), "-v=2"),
		},
		{
			in:           operatorv1.Debug,
			expectedArgs: append(getArgsCopy(args), "-v=4"),
		},
		{
			in:           operatorv1.Trace,
			expectedArgs: append(getArgsCopy(args), "-v=6"),
		},
		{
			in:           operatorv1.TraceAll,
			expectedArgs: append(getArgsCopy(args), "-v=8"),
		},
	}

	for _, tc := range testCases {
		if err := UpdatePodSpec(podSpec, "", tc.in); err != nil {
			t.Errorf("UpdatePodSpec failed with error: %v", err)
		}
		cnt := podSpec.Containers[0]
		assert.ElementsMatch(t, cnt.Args, tc.expectedArgs, "container %s args %v, not equal to %v", cnt.Name, cnt.Args, tc.expectedArgs)
	}
}

func TestUpdateArgs(t *testing.T) {
	tests := []struct {
		name       string
		cntName    string
		level      operatorv1.LogLevel
		expectVerb int
	}{
		{
			name:       "Debug level",
			cntName:    "test-container-debug",
			level:      operatorv1.Debug,
			expectVerb: 4,
		},
		{
			name:       "Trace level",
			cntName:    "test-container-trace",
			level:      operatorv1.Trace,
			expectVerb: 6,
		},
		{
			name:       "Default verbosity",
			cntName:    "test-container-default",
			level:      operatorv1.Normal,
			expectVerb: 2,
		},
		{
			name:       "Default in case of invalid",
			cntName:    "test-container-invalid",
			level:      "InvalidLevel",
			expectVerb: 2, // Should set the default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := &rteconfiguration.ProgArgs{}
			err := UpdateArgs(args, tt.cntName, tt.level)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if args.Global.Verbose != tt.expectVerb {
				t.Errorf("Expected Verbosity %v, got %v", tt.expectVerb, args.Global.Verbose)
			}
		})
	}
}

func getArgsCopy(args []string) []string {
	initialArgs := make([]string, len(args))
	copy(initialArgs, args)
	return initialArgs
}
