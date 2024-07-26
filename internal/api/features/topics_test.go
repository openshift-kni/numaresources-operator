/*
Copyright 2024.

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

package features

import (
	"strings"
	"testing"

	"k8s.io/utils/strings/slices"
)

func TestGetTopics(t *testing.T) {
	type testcase struct {
		name       string
		topicsList string
		isPositive bool
	}
	testcases := []testcase{
		{
			name:       "with list of well known topics that every release operator must support",
			topicsList: "config,nonreg,hostlevel,resacct,cache,stall,schedrst,overhead,wlplacement,unsched,taint,nodelabel,byres,tmpol",
			isPositive: true,
		},
		{
			name:       "with inactive topics included",
			topicsList: "config,nonreg,hostlevel,notfound,cache",
			isPositive: false,
		},
	}

	actual := GetTopics()
	for _, tc := range testcases {
		topicsStr := strings.TrimSpace(tc.topicsList)
		topics := strings.Split(topicsStr, ",")
		tp := findMissingTopics(actual.Active, topics)

		if len(tp) > 0 && tc.isPositive {
			t.Errorf("expected to include topic(s) %v but it didn't, list found: %v", tp, actual.Active)
		}

		if len(tp) == 0 && !tc.isPositive {
			t.Errorf("active topics included unsupported topic(s) %v, list found: %v", tp, actual.Active)
		}
	}
}

func findMissingTopics(agianstList, sublist []string) []string {
	missing := []string{}
	for _, el := range sublist {
		if !slices.Contains(agianstList, el) {
			missing = append(missing, el)
		}
	}
	return missing
}
