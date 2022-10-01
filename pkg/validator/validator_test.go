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

package validator

import (
	"reflect"
	"strings"
	"testing"
)

func TestRequested(t *testing.T) {
	type testCase struct {
		what          string
		expectedValue string
		expectedError error
	}

	available := strings.Join(Available().List(), ",")

	testcases := []testCase{
		{
			what:          "",
			expectedValue: "",
		},
		{
			what:          "help",
			expectedValue: "help",
		},
		{
			what:          "nrt,help",
			expectedValue: "help",
		},
		{
			what:          "help, foobar",
			expectedValue: "help",
		},
		{
			what:          "all",
			expectedValue: available,
		},
		{
			what:          "k8scfg,podst,nrt",
			expectedValue: available,
		},
		{
			what:          "nrt,k8scfg,podst",
			expectedValue: available,
		},
		{
			what:          " nrt,  k8scfg ,   podst ",
			expectedValue: available,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.what, func(t *testing.T) {
			got, err := Requested(tc.what)
			if err != tc.expectedError {
				t.Errorf("Requested(%s): got error %v expected %v", tc.what, err, tc.expectedError)
			}

			gotValue := strings.Join(got.List(), ",")
			if !reflect.DeepEqual(gotValue, tc.expectedValue) {
				t.Errorf("Requested(%s): got %v expected %v", tc.what, got, tc.expectedValue)
			}
		})
	}
}
