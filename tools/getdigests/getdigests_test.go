/*
 * Copyright 2026 Red Hat, Inc.
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

package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestFilterTags(t *testing.T) {
	tests := []struct {
		name          string
		tags          []string
		versionString string
		expected      []string
	}{
		{
			name:          "filters by version prefix",
			tags:          []string{"v4.20.0", "v4.20.1", "v4.19.5", "v4.21.0"},
			versionString: "4.20",
			expected:      []string{"v4.20.0", "v4.20.1"},
		},
		{
			name:          "excludes source tags",
			tags:          []string{"v4.20.0", "v4.20.0-source", "v4.20.1"},
			versionString: "4.20",
			expected:      []string{"v4.20.0", "v4.20.1"},
		},
		{
			name:          "trims whitespace from tags",
			tags:          []string{"  v4.20.0  ", "v4.20.1"},
			versionString: "4.20",
			expected:      []string{"v4.20.0", "v4.20.1"},
		},
		{
			name:          "does not match partial prefix",
			tags:          []string{"v4.200.0", "v4.20.0"},
			versionString: "4.20",
			expected:      []string{"v4.200.0", "v4.20.0"},
		},
		{
			name:          "no matches returns nil",
			tags:          []string{"v4.19.0", "v4.21.0"},
			versionString: "4.20",
			expected:      nil,
		},
		{
			name:          "empty input returns nil",
			tags:          []string{},
			versionString: "4.20",
			expected:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterTags(tc.tags, tc.versionString)
			if len(got) != len(tc.expected) {
				t.Fatalf("expected %v (len=%d), got %v (len=%d)", tc.expected, len(tc.expected), got, len(got))
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("index %d: expected %q, got %q", i, tc.expected[i], got[i])
				}
			}
		})
	}
}

func TestPreviousVersion(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		expected    string
		expectError bool
	}{
		{name: "normal minor decrement", version: "4.20", expected: "4.19"},
		{name: "minor version 1", version: "4.1", expected: "4.0"},
		{name: "different major", version: "5.3", expected: "5.2"},
		{name: "5.0 maps to 4.22 (cross-major special case)", version: "5.0", expected: "4.22"},
		{name: "minor version 0 errors", version: "4.0", expectError: true},
		{name: "no dot in version", version: "420", expectError: true},
		{name: "non-numeric minor", version: "4.x", expectError: true},
		{name: "empty string", version: "", expectError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := previousVersion(tc.version)
			if tc.expectError {
				if err == nil {
					t.Errorf("expected error, got nil (result: %q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestSkopeoListTagsArgs(t *testing.T) {
	t.Run("without pull secret", func(t *testing.T) {
		got := skopeoListTagsArgs("registry.example.com/img", "")
		want := []string{"list-tags", "docker://registry.example.com/img"}
		assertStringSlice(t, want, got)
	})

	t.Run("with pull secret", func(t *testing.T) {
		got := skopeoListTagsArgs("registry.example.com/img", "/path/to/secret.json")
		want := []string{"list-tags", "--authfile", "/path/to/secret.json", "docker://registry.example.com/img"}
		assertStringSlice(t, want, got)
	})
}

func TestSkopeoInspectArgs(t *testing.T) {
	t.Run("without pull secret", func(t *testing.T) {
		got := skopeoInspectArgs("registry.example.com/img", "", "v4.20.0")
		want := []string{"inspect", "--format", "{{.Digest}}", "docker://registry.example.com/img:v4.20.0"}
		assertStringSlice(t, want, got)
	})

	t.Run("with pull secret", func(t *testing.T) {
		got := skopeoInspectArgs("registry.example.com/img", "/path/to/secret.json", "v4.20.0")
		want := []string{"inspect", "--authfile", "/path/to/secret.json", "--format", "{{.Digest}}", "docker://registry.example.com/img:v4.20.0"}
		assertStringSlice(t, want, got)
	})
}

func TestGetDigests(t *testing.T) {
	fakeDigests := map[string]string{
		"v4.20.0": "sha256:aaa",
		"v4.20.1": "sha256:bbb",
		"v4.19":   "sha256:ccc",
	}

	rawTags, _ := json.Marshal(listTagsOutput{
		Tags: []string{"v4.20.0", "v4.20.0-source", "v4.20.1", "v4.19.5"},
	})

	fakeRunner := func(name string, args ...string) (string, error) {
		if args[0] == "list-tags" {
			return string(rawTags), nil
		}
		// inspect: last arg is docker://image:tag
		lastArg := args[len(args)-1]
		idx := strings.LastIndex(lastArg, ":")
		tag := lastArg[idx+1:]
		digest, ok := fakeDigests[tag]
		if !ok {
			return "", fmt.Errorf("unknown tag: %s", tag)
		}
		return digest, nil
	}

	result, err := getDigests(fakeRunner, "registry.example.com/img", "", "4.20", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.CurrentChannel) != 2 {
		t.Errorf("expected 2 current channel entries, got %d: %v", len(result.CurrentChannel), result.CurrentChannel)
	}
	for _, wantDigest := range []string{"sha256:aaa", "sha256:bbb"} {
		if !slices.Contains(result.CurrentChannel, wantDigest) {
			t.Errorf("expected digest %q in current channel, got %v", wantDigest, result.CurrentChannel)
		}
	}
	if result.PreviousChannelLast != "sha256:ccc" {
		t.Errorf("expected prev channel digest %q, got %q", "sha256:ccc", result.PreviousChannelLast)
	}
}

func TestGetDigests_NoDuplicateDigests(t *testing.T) {
	// All three tags resolve to the same digest (mirrors the real registry output
	// seen with v4.22, v4.22.0, v4.22.0-41 all pointing to the same image).
	const sharedDigest = "sha256:55d3fe347f35c257b6f828ec0da1aeef057f723bbcec1576d4d0f9018fb74fe7"

	rawTags, _ := json.Marshal(listTagsOutput{
		Tags: []string{"v4.22", "v4.22.0", "v4.22.0-41"},
	})

	fakeRunner := func(_ string, args ...string) (string, error) {
		if args[0] == "list-tags" {
			return string(rawTags), nil
		}
		return sharedDigest, nil
	}

	result, err := getDigests(fakeRunner, "registry.example.com/img", "", "4.22", "4.21")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.CurrentChannel) != 1 {
		t.Errorf("expected 1 unique digest entry, got %d: %v", len(result.CurrentChannel), result.CurrentChannel)
	}
	if !slices.Contains(result.CurrentChannel, sharedDigest) {
		t.Errorf("expected digest %q in current channel, got %v", sharedDigest, result.CurrentChannel)
	}
}

func TestGetDigests_ListTagsError(t *testing.T) {
	fakeRunner := func(_ string, _ ...string) (string, error) {
		return "", fmt.Errorf("registry unavailable")
	}
	_, err := getDigests(fakeRunner, "registry.example.com/img", "", "4.20", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetDigests_NoMatchingTags(t *testing.T) {
	rawTags, _ := json.Marshal(listTagsOutput{Tags: []string{"v4.19.0", "v4.21.0"}})
	fakeRunner := func(_ string, args ...string) (string, error) {
		if args[0] == "list-tags" {
			return string(rawTags), nil
		}
		return "", fmt.Errorf("should not be called")
	}
	_, err := getDigests(fakeRunner, "registry.example.com/img", "", "4.20", "")
	if err == nil {
		t.Fatal("expected error for no matching tags, got nil")
	}
}

func TestGetDigests_InvalidVersion(t *testing.T) {
	rawTags, _ := json.Marshal(listTagsOutput{Tags: []string{"v4.20.0"}})
	fakeRunner := func(_ string, args ...string) (string, error) {
		if args[0] == "list-tags" {
			return string(rawTags), nil
		}
		return "sha256:abc", nil
	}
	_, err := getDigests(fakeRunner, "registry.example.com/img", "", "420", "")
	if err == nil {
		t.Fatal("expected error for invalid version format, got nil")
	}
}

func TestGetDigests_PrevChannelFailureIsFatal(t *testing.T) {
	rawTags, _ := json.Marshal(listTagsOutput{Tags: []string{"v4.20.0"}})
	fakeRunner := func(_ string, args ...string) (string, error) {
		if args[0] == "list-tags" {
			return string(rawTags), nil
		}
		// inspect for current channel succeeds, prev channel fails
		lastArg := args[len(args)-1]
		if strings.HasSuffix(lastArg, ":v4.19") {
			return "", fmt.Errorf("prev channel not found")
		}
		return "sha256:abc", nil
	}
	_, err := getDigests(fakeRunner, "registry.example.com/img", "", "4.20", "")
	if err == nil {
		t.Fatal("expected error when previous channel lookup fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get latest of previous channel") {
		t.Fatalf("expected previous channel error, got: %v", err)
	}
}

func TestGetDigests_PrevVersionOverride(t *testing.T) {
	fakeDigests := map[string]string{
		"v5.0.0": "sha256:new",
		"v4.22":  "sha256:last422",
	}
	rawTags, _ := json.Marshal(listTagsOutput{Tags: []string{"v5.0.0"}})

	fakeRunner := func(_ string, args ...string) (string, error) {
		if args[0] == "list-tags" {
			return string(rawTags), nil
		}
		lastArg := args[len(args)-1]
		idx := strings.LastIndex(lastArg, ":")
		tag := lastArg[idx+1:]
		digest, ok := fakeDigests[tag]
		if !ok {
			return "", fmt.Errorf("unknown tag: %s", tag)
		}
		return digest, nil
	}

	// 4.0 has no auto-resolved previous channel without override
	_, err := getDigests(fakeRunner, "registry.example.com/img", "", "4.0", "")
	if err == nil {
		t.Fatal("expected error for 4.0 without override, got nil")
	}

	// 5.0 auto-resolves to 4.22; explicit override should succeed too
	result, err := getDigests(fakeRunner, "registry.example.com/img", "", "5.0", "4.22")
	if err != nil {
		t.Fatalf("unexpected error with prev-version override: %v", err)
	}
	if !slices.Contains(result.CurrentChannel, "sha256:new") {
		t.Errorf("expected current channel digest %q, got %v", "sha256:new", result.CurrentChannel)
	}
	if result.PreviousChannelLast != "sha256:last422" {
		t.Errorf("expected prev channel digest %q, got %q", "sha256:last422", result.PreviousChannelLast)
	}
}

func assertStringSlice(t *testing.T, want, got []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d]: want %q, got %q", i, want[i], got[i])
		}
	}
}
