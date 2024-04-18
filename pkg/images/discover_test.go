/*
 * Copyright 2024 Red Hat, Inc.
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

package images

import "testing"

func TestDataPreferred(t *testing.T) {
	testCases := []struct {
		name     string
		imgs     Data
		expected string
	}{
		{
			name:     "empty",
			imgs:     Data{},
			expected: "",
		},
		{
			name: "user-only",
			imgs: Data{
				User: "user-image",
			},
			expected: "user-image",
		},
		{
			name: "self-only",
			imgs: Data{
				Self: "self-image",
			},
			expected: "self-image",
		},
		{
			name: "builtin-only",
			imgs: Data{
				Builtin: "builtin-image",
			},
			expected: "builtin-image",
		},
		{
			name: "user overrides self",
			imgs: Data{
				User: "user-image",
				Self: "self-image",
			},
			expected: "user-image",
		},
		{
			name: "user overrides builtin",
			imgs: Data{
				User:    "user-image",
				Builtin: "builtin-image",
			},
			expected: "user-image",
		},
		{
			name: "self overrides builtin",
			imgs: Data{
				Self:    "self-image",
				Builtin: "builtin-image",
			},
			expected: "self-image",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.imgs.Preferred()
			if got != tc.expected {
				t.Errorf("data %v expected %q got %q", tc.imgs, tc.expected, got)
			}
		})
	}
}
