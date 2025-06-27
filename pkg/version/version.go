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

package version

import (
	"encoding/json"
	"os"
	"path/filepath"

	_ "embed"

	"github.com/openshift-kni/numaresources-operator/internal/api/buildinfo"
)

//go:embed _buildinfo.json
var buildInfo string

func GetBuildInfo() buildinfo.BuildInfo {
	var bi buildinfo.BuildInfo
	_ = json.Unmarshal([]byte(buildInfo), &bi)
	return bi
}

// Get returns the version as a string
func Get() string {
	bi := GetBuildInfo()
	return bi.Version
}

func GetGitCommit() string {
	bi := GetBuildInfo()
	return bi.Commit
}

func ProgramName() string {
	if len(os.Args) == 0 {
		return "undefined"
	}
	return filepath.Base(os.Args[0])
}

func OperatorProgramName() string {
	return "numaresources-operator"
}

func ExporterProgramName() string {
	return "resource-topology-exporter"
}
