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

func TestTag(t *testing.T) {
	if Tag() == "" {
		t.Errorf("empty tag")
	}
}

func TestName(t *testing.T) {
	if Name() == "" {
		t.Errorf("empty name")
	}
}

func TestRepo(t *testing.T) {
	if Repo() == "" {
		t.Errorf("empty repo")
	}
}

func TestSpecPath(t *testing.T) {
	if SpecPath() == "" {
		t.Errorf("empty spec path")
	}
}
