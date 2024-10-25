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
	return fmt.Sprintf("Signed-off-by: %s %s", commit.Author, commit.AuthorEmail)
}

func expectedDCOCoauthorTag(commit GitCommit) string {
	return fmt.Sprintf("Co-authored-by: %s %s", commit.Author, commit.AuthorEmail)
}

func main() {
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
			fmt.Fprintf(os.Stderr, "error decoding git commit: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("commit: %+v\n", commit)
		commits = append(commits, commit)
	}

	fmt.Printf("read: %d commits\n", len(commits))

	for _, commit := range commits {
		err := validate(commit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid commit: %v\n", err)
			os.Exit(2)
		}
	}
}
