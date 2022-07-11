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

package objectnames

import "testing"

func TestDefaultNUMAResourcesOperatorCrName(t *testing.T) {
	if DefaultNUMAResourcesOperatorCrName == "" {
		t.Errorf("empty DefaultNUMAResourcesOperatorCrName")
	}
}

func TestGetMachineConfigName(t *testing.T) {
	got := GetMachineConfigName("foo", "bar")
	if len(got) == 0 {
		t.Errorf("generated empty MachineConfigName")
	}
}

func TestGetComponentName(t *testing.T) {
	got := GetComponentName("foo", "bar")
	if len(got) == 0 {
		t.Errorf("generated empty ComponentName")
	}
}
