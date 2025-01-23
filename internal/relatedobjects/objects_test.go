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

package relatedobjects

import (
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
)

func TestResourceTopologyExporter(t *testing.T) {
	type testCase struct {
		name      string
		namespace string
		dsStatus  []nropv1.NamespacedName
		expected  []configv1.ObjectReference
	}

	testCases := []testCase{
		{
			name:      "fully initialized single DS",
			namespace: "test-ns",
			dsStatus: []nropv1.NamespacedName{
				{
					Namespace: "test-ds-ns",
					Name:      "test-ds",
				},
			},
			expected: []configv1.ObjectReference{
				{
					Resource: "namespaces",
					Name:     "test-ns",
				},
				{
					Group:    "machineconfiguration.openshift.io",
					Resource: "kubeletconfigs",
				},
				{
					Group:    "machineconfiguration.openshift.io",
					Resource: "machineconfigs",
				},
				{
					Group:    "topology.node.k8s.io",
					Resource: "noderesourcetopologies",
				},
				{
					Group:     "apps",
					Resource:  "daemonsets",
					Namespace: "test-ds-ns",
					Name:      "test-ds",
				},
			},
		},
		{
			name:      "fully initialized multi DS",
			namespace: "test-ns",
			dsStatus: []nropv1.NamespacedName{
				{
					Namespace: "test-ds-ns-1",
					Name:      "test-ds-1",
				},
				{
					Namespace: "test-ds-ns-2",
					Name:      "test-ds-2",
				},
				{
					Namespace: "test-ds-ns-3",
					Name:      "test-ds-3",
				},
			},
			expected: []configv1.ObjectReference{
				{
					Resource: "namespaces",
					Name:     "test-ns",
				},
				{
					Group:    "machineconfiguration.openshift.io",
					Resource: "kubeletconfigs",
				},
				{
					Group:    "machineconfiguration.openshift.io",
					Resource: "machineconfigs",
				},
				{
					Group:    "topology.node.k8s.io",
					Resource: "noderesourcetopologies",
				},
				{
					Group:     "apps",
					Resource:  "daemonsets",
					Namespace: "test-ds-ns-1",
					Name:      "test-ds-1",
				},
				{
					Group:     "apps",
					Resource:  "daemonsets",
					Namespace: "test-ds-ns-2",
					Name:      "test-ds-2",
				},
				{
					Group:     "apps",
					Resource:  "daemonsets",
					Namespace: "test-ds-ns-3",
					Name:      "test-ds-3",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResourceTopologyExporter(tc.namespace, tc.dsStatus)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("mismatching objects got=%#v expected=%#v", got, tc.expected)
			}
		})
	}
}

func TestScheduler(t *testing.T) {
	type testCase struct {
		name      string
		namespace string
		dpStatus  nropv1.NamespacedName
		expected  []configv1.ObjectReference
	}

	testCases := []testCase{
		{
			name: "no-namespace",
			dpStatus: nropv1.NamespacedName{
				Namespace: "test-dp-ns",
				Name:      "test-dp",
			},
			expected: []configv1.ObjectReference{
				{
					Resource: "namespaces",
				},
				{
					Group:     "apps",
					Resource:  "deployments",
					Namespace: "test-dp-ns",
					Name:      "test-dp",
				},
			},
		},
		{
			name:      "fully initialized",
			namespace: "test-ns",
			dpStatus: nropv1.NamespacedName{
				Namespace: "test-dp-ns",
				Name:      "test-dp",
			},
			expected: []configv1.ObjectReference{
				{
					Resource: "namespaces",
					Name:     "test-ns",
				},
				{
					Group:     "apps",
					Resource:  "deployments",
					Namespace: "test-dp-ns",
					Name:      "test-dp",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Scheduler(tc.namespace, tc.dpStatus)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("mismatching objects got=%#v expected=%#v", got, tc.expected)
			}
		})
	}
}
