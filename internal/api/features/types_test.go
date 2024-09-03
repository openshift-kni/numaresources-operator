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
	"reflect"
	"testing"
)

func TestNewTopicInfo(t *testing.T) {
	actual := NewTopicInfo()
	expected := TopicInfo{
		Metadata: Metadata{
			Version: Version,
		},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("got different new TopicInfo than expected: expected:\n%+v\ngot:\n%+v\n", expected, actual)
	}
}

func TestValidate(t *testing.T) {
	nonEmpty := []string{"foo", "bar"}
	posTests := []TopicInfo{
		{
			Metadata: Metadata{
				Version: Version,
			},
			Active: GetTopics().Active,
		},
		{
			Metadata: Metadata{
				Version: "2.0-4",
			},
			Active: nonEmpty,
		},
		{
			Metadata: Metadata{
				Version: "v4.10.0-202312160824.p0.g89f320d.assembly.stream",
			},
			Active: nonEmpty,
		},
		{
			Metadata: Metadata{
				Version: "v1.11.16-rc.1",
			},
			Active: nonEmpty,
		},
		{
			Metadata: Metadata{
				Version: "4.18",
			},
			Active: nonEmpty,
		},
	}

	for _, tc := range posTests {
		if err := tc.Validate(); err != nil {
			t.Errorf("valid topics %+v failed validation:%v\n", tc, err)
		}
	}

	negTests := []TopicInfo{
		NewTopicInfo(),
		{
			Metadata: Metadata{
				Version: "v4.v10.v0-2023121608y.stream",
			},
			Active: nonEmpty,
		},
		{
			Metadata: Metadata{
				Version: "4-v1. 6",
			},
			Active: nonEmpty,
		},
	}

	for _, tc := range negTests {
		if err := tc.Validate(); err == nil {
			t.Errorf("invalid topics %+v passed validation\n", tc)
		}
	}
}
