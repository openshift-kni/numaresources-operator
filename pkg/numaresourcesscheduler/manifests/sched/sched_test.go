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
 * Copyright 2021 Red Hat, Inc.
 */

package sched

import (
	"reflect"
	"testing"
)

func TestGetManifests(t *testing.T) {
	mf, err := GetManifests("")
	if err != nil {
		t.Fatalf("GetManifests() failed: err=%v", err)
	}

	for _, obj := range mf.ToObjects() {
		if obj == nil {
			t.Fatalf("GetManifests(): loaded nil manifest")
		}
	}
}

func TestCloneManifests(t *testing.T) {
	mf, err := GetManifests("")
	if err != nil {
		t.Fatalf("GetManifests() failed: err=%v", err)
	}

	mf2 := mf.Clone()
	if !reflect.DeepEqual(mf, mf2) {
		t.Fatalf("Clone() returned manifests failing DeepEqual")
	}

	for _, obj := range mf2.ToObjects() {
		if obj == nil {
			t.Fatalf("Clone(): produced nil manifest")
		}
	}
}
