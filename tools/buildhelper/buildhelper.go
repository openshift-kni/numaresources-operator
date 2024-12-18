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

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mdomke/git-semver/version"

	apibuildinfo "github.com/openshift-kni/numaresources-operator/internal/api/buildinfo"
	"github.com/openshift-kni/numaresources-operator/internal/buildinfo"
)

const (
	develBranchName     = "devel"
	releaseBranchPrefix = "release-"

	versionFileName = "Makefile"
)

func parseVersion() string {
	srcFile := versionFileName
	// enable override the master version file, which is Makefile by default
	if fileName, ok := os.LookupEnv("NRO_BUILD_VERSION_FILE"); ok {
		srcFile = fileName
	}
	return buildinfo.ParseVersionFromFile(srcFile)
}

func getVersion() (string, error) {
	if ver, ok := os.LookupEnv("NRO_BUILD_VERSION"); ok {
		return ver, nil
	}

	if ver := parseVersion(); ver != "" {
		return ver, nil
	}

	v, err := version.Derive()
	return v.String(), err
}

func getCommit() (string, error) {
	if cm, ok := os.LookupEnv("NRO_BUILD_COMMIT"); ok {
		return cm, nil
	}

	cmd := exec.Command("git", "log", "-1", "--pretty=format:%h")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func getRawBranch() (string, error) {
	if cm, ok := os.LookupEnv("NRO_BUILD_BRANCH"); ok {
		return cm, nil
	}

	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func parseBranch(branchName string) string {
	if !strings.HasPrefix(branchName, releaseBranchPrefix) {
		return develBranchName
	}
	return strings.TrimPrefix(branchName, releaseBranchPrefix)
}

func getBranch() (string, error) {
	branchName, err := getRawBranch()
	if err != nil {
		return "", err
	}
	return parseBranch(branchName), nil
}

func showVersion() int {
	ver, err := getVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}
	fmt.Println(ver)
	return 0
}

func showCommit() int {
	cm, err := getCommit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}
	fmt.Println(cm)
	return 0
}

func showBranch() int {
	br, err := getBranch()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}
	fmt.Println(br)
	return 0
}

func inspect() int {
	var bi apibuildinfo.BuildInfo
	var branchName string
	var err error

	bi.Version, err = getVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}

	branchName, err = getRawBranch()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}

	bi.GitBranch = branchName
	bi.Branch = parseBranch(branchName)

	bi.Commit, err = getCommit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}

	if err := validate(bi); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}

	if err := json.NewEncoder(os.Stdout).Encode(&bi); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}
	return 0
}

func validate(bi apibuildinfo.BuildInfo) error {
	// TODO: for devel branch,
	// validate the current version is greater than the version encoded
	// in the highest numbered release branch
	if bi.Branch != develBranchName { // release branch
		if bi.Branch != bi.Version {
			return fmt.Errorf("branch name %q must match version %q", bi.Branch, bi.Version)
		}
	}
	return nil
}

func help() {
	fmt.Fprintf(os.Stderr, "usage: %s [inspect|version|commit|branch]\n", filepath.Base(os.Args[0]))
	os.Exit(1)
}

func main() {
	if len(os.Args) != 2 {
		help()
	}

	switch os.Args[1] {
	case "inspect":
		inspect()
	case "version":
		showVersion()
	case "commit":
		showCommit()
	case "branch":
		showBranch()
	default:
		help()
	}
}
