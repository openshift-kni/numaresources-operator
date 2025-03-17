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

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type GitCommit struct {
	Commit           string `json:"commit"`
	Author           string `json:"author"`
	AuthorEmail      string `json:"authorEmail"`
	AuthorEmailLocal string `json:"authorEmailLocal"`
	DCOSignTag       string `json:"dcoSignTag"`
	DCOCoauthorTag   string `json:"dcoCoauthorTag"`
}

const dependabot = "dependabot[bot]"

func validate(commit GitCommit) error {
	var errs error
	if "<"+commit.Author+">" == commit.AuthorEmailLocal {
		errs = errors.Join(errs, fmt.Errorf("missing author name - equals to email local part"))
	}
	if commit.DCOSignTag == "" {
		errs = errors.Join(errs, fmt.Errorf("DCO signoff trailer missing"))
	} else {
		expectedDCO := expectedDCOSignTag(commit)
		if strings.Contains(commit.DCOSignTag, expectedDCO) {
			fmt.Printf("git commit email found in sign off list\n")
		} else {
			expectedDCOAuth := expectedDCOCoauthorTag(commit)
			if strings.Contains(commit.DCOCoauthorTag, expectedDCOAuth) {
				fmt.Printf("git commit email not in sign off list, but found in co-author list\n")
			} else {
				errs = errors.Join(errs, fmt.Errorf("DCO signoff malformed: %q does not contain expected %q", commit.DCOSignTag, expectedDCO))
			}
		}
	}
	return errs
}

func expectedDCOSignTag(commit GitCommit) string {
	if commit.Author == dependabot {
		return fmt.Sprintf("Signed-off-by: %s", commit.Author)
	}
	return fmt.Sprintf("Signed-off-by: %s %s", commit.Author, commit.AuthorEmail)
}

func expectedDCOCoauthorTag(commit GitCommit) string {
	return fmt.Sprintf("Co-authored-by: %s %s", commit.Author, commit.AuthorEmail)
}

func toJSON(obj any) string {
	data, err := json.Marshal(obj)
	if err != nil {
		return "<ERROR>"
	}
	return string(data)
}

func scanAndVerify() (int, error) {
	var err error
	commits := []GitCommit{}
	stdin := bufio.NewScanner(os.Stdin)

	fmt.Printf("start verifying commits from stdin\n")
	defer fmt.Printf("done verifying commits from stdin\n")

	for stdin.Scan() {
		commit := GitCommit{}
		line := stdin.Text()
		err = json.Unmarshal([]byte(line), &commit)
		if err != nil {
			return 1, fmt.Errorf("error decoding git commit: %v\n", err)
		}
		fmt.Printf("commit: %s\n", toJSON(commit))
		commits = append(commits, commit)
	}

	fmt.Printf("read: %d commits\n", len(commits))

	for _, commit := range commits {
		err := validate(commit)
		if err != nil {
			return 2, fmt.Errorf("invalid commit: %v\n", err)
		}
	}
	return 0, nil
}

func main() {
	// As part of making the logging more clear for a function we defer executing
	// some code to the end of the check.However, deferred code is not reachable
	// if the function calls `os.Exit` at anypoint even in nested functions. Since
	// `os.Exit(<code>)` functionality cannot be substituted, and we desire to
	// keep using	`defer`, the logic of verifying	commits is implemented in
	// `scanAndVerify`and this in turn returns an error and exit code on failure.
	// This result is examined here main() and the program exists with the return
	// code or succeeds.
	exitcode, err := scanAndVerify()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitcode)
	}
}
