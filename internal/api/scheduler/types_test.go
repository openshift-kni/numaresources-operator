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

package scheduler

import (
	"reflect"
	"testing"
)

func TestDigests_AddCurrentChannel(t *testing.T) {
	const (
		digest1 = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		digest2 = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	)

	d := Digests{}
	d.AddCurrentChannel(digest1)

	if !reflect.DeepEqual(d.CurrentChannel, []string{digest1}) {
		t.Fatalf("after first add: got=%v want=%v", d.CurrentChannel, []string{digest1})
	}

	d.AddCurrentChannel(digest2)

	want := []string{digest1, digest2}
	if !reflect.DeepEqual(d.CurrentChannel, want) {
		t.Fatalf("after second add: got=%v want=%v", d.CurrentChannel, want)
	}
}
