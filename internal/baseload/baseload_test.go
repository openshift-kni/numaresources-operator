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

package baseload

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestRound(t *testing.T) {
	type testCase struct {
		data     Load
		expected string // it's just easier to compare strings
	}

	testCases := []testCase{
		{
			data: Load{
				Name:   "test",
				CPU:    resource.MustParse("990m"),
				Memory: resource.MustParse("4G"),
			},
			expected: "load for node \"test\": CPU=2 Memory=4G",
		},
		{
			data: Load{
				Name:   "test",
				CPU:    resource.MustParse("1"),
				Memory: resource.MustParse("4Gi"),
			},
			expected: "load for node \"test\": CPU=2 Memory=5000000000",
		},
		{
			data: Load{
				Name:   "test",
				CPU:    resource.MustParse("3"),
				Memory: resource.MustParse("2232Mi"),
			},
			expected: "load for node \"test\": CPU=4 Memory=3000000000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.data.String(), func(t *testing.T) {
			got := tc.data.Round().String()
			if got != tc.expected {
				t.Errorf("expected %q got %q", tc.expected, got)
			}
		})
	}
}
