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

package version

import "testing"

func TestGet(t *testing.T) {
	if Get() == "" {
		t.Errorf("empty version")
	}
}

func TestUndefined(t *testing.T) {
	if !Undefined() {
		t.Errorf("final version should be defined at link stage")
	}
}

func TestGetGitCommit(t *testing.T) {
	if GetGitCommit() == "" {
		t.Errorf("empty gitcommit")
	}
}

func TestUndefinedGitCommit(t *testing.T) {
	if !UndefinedGitCommit() {
		t.Errorf("final gitcommit should be defined at link stage")
	}
}

func TestProgramName(t *testing.T) {
	if ProgramName() == "undefined" {
		t.Errorf("program name should always be defined")
	}
}
