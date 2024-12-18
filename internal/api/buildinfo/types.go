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

package buildinfo

type BuildInfo struct {
	// GitBranch is the raw git branch from which the artifacts are built
	GitBranch string `json:"gitbranch"`
	// Branch is the parsed git branch from which the artifacts are built
	Branch string `json:"branch"`
	// Version is the MAJOR.MINOR version of the artifacts being built
	Version string `json:"version"`
	// Commit is the parsed git commit from which the artifacts are built
	Commit string `json:"commit"`
}

func (bi BuildInfo) String() string {
	return bi.Version + " " + bi.Commit + " (" + bi.Branch + ")"
}
