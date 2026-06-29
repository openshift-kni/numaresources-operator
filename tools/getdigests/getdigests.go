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

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	schedulerapi "github.com/openshift-kni/numaresources-operator/internal/api/scheduler"
)

// This is tool to print json representation of the digests of the images in the current channel
// and the latest one in the previous channel

// cmdRunner executes a named command and returns its trimmed stdout or an error
// that includes stderr when available.
type cmdRunner func(name string, args ...string) (string, error)

// listTagsOutput matches the JSON structure returned by `skopeo list-tags`.
type listTagsOutput struct {
	Tags []string `json:"Tags"`
}

func execRunner(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// getDigests fetches the digests of all tags matching versionString for the
// current channel, plus the latest tag of the previous channel.
// prevVersionOverride, when non-empty, is used as the previous version directly
// instead of auto-calculating it (required when the current minor version is 0,
// e.g. "5.0" where the previous channel is "4.22").
func getDigests(run cmdRunner, imageURL string, pullSecretPath string, versionString string, prevVersionOverride string) (schedulerapi.Digests, error) {
	raw, err := run("skopeo", skopeoListTagsArgs(imageURL, pullSecretPath)...)
	if err != nil {
		return schedulerapi.Digests{}, fmt.Errorf("listing tags for %s: %w", imageURL, err)
	}

	var tagsOut listTagsOutput
	if err := json.Unmarshal([]byte(raw), &tagsOut); err != nil {
		return schedulerapi.Digests{}, fmt.Errorf("parsing list-tags output: %w", err)
	}

	tags := filterTags(tagsOut.Tags, versionString)
	if len(tags) == 0 {
		return schedulerapi.Digests{}, fmt.Errorf("no tags found matching version %q", versionString)
	}

	digests := schedulerapi.Digests{}
	for _, tag := range tags {
		digest, err := run("skopeo", skopeoInspectArgs(imageURL, pullSecretPath, tag)...)
		if err != nil {
			return schedulerapi.Digests{}, fmt.Errorf("inspecting tag %s: %w", tag, err)
		}

		// only add the digest if it's not already in the set
		if slices.Contains(digests.CurrentChannel, digest) {
			continue
		}

		digests.AddCurrentChannel(digest)
	}

	prevVer := prevVersionOverride
	if prevVer == "" {
		prevVer, err = previousVersion(versionString)
		if err != nil {
			return schedulerapi.Digests{}, err
		}
	}
	prevTag := "v" + prevVer
	latestOfPrev, err := run("skopeo", skopeoInspectArgs(imageURL, pullSecretPath, prevTag)...)
	if err != nil {
		return schedulerapi.Digests{}, fmt.Errorf("failed to get latest of previous channel (%s): %v", prevTag, err)
	}

	digests.PreviousChannelLast = latestOfPrev

	return digests, nil
}

// previousVersion returns the version string for the immediately preceding minor release.
// e.g. "4.20" -> "4.19"
func previousVersion(versionString string) (string, error) {
	if versionString == "5.0" {
		// special case for 5.0, which is the first release of the 5.x series
		return "4.22", nil
	}

	parts := strings.SplitN(versionString, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid version format %q: expected X.Y", versionString)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", fmt.Errorf("invalid minor version in %q: %v", versionString, err)
	}
	if minor == 0 {
		// we cannot make a guess for the previous minor version so we error out
		return "", fmt.Errorf("no previous minor version for %q", versionString)
	}
	return fmt.Sprintf("%s.%d", parts[0], minor-1), nil
}

func skopeoListTagsArgs(imageURL string, pullSecretPath string) []string {
	args := []string{"list-tags"}
	if pullSecretPath != "" {
		args = append(args, "--authfile", pullSecretPath)
	}
	args = append(args, "docker://"+imageURL)
	return args
}

func skopeoInspectArgs(imageURL string, pullSecretPath string, tag string) []string {
	args := []string{"inspect"}
	if pullSecretPath != "" {
		args = append(args, "--authfile", pullSecretPath)
	}
	args = append(args, "--format", "{{.Digest}}")
	args = append(args, "docker://"+imageURL+":"+tag)
	return args
}

func filterTags(tags []string, versionString string) []string {
	// follow the pattern of X.Y.Z
	prefix := "v" + versionString + "."
	var filtered []string
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if strings.HasPrefix(tag, prefix) && !strings.HasSuffix(tag, "-source") {
			filtered = append(filtered, tag)
		}
	}
	return filtered
}

func run() error {
	imageURL := ""
	pullSecretPath := ""
	outputFile := ""
	versionString := ""
	prevVersionOverride := ""

	flag.StringVar(&imageURL, "image", "", "image URL to query (e.g. registry.redhat.io/openshift4/noderesourcetopology-scheduler-rhel9)")
	flag.StringVar(&pullSecretPath, "pull-secret", "", "path to the pull secret JSON file for registry authentication")
	flag.StringVar(&outputFile, "output", "", "path to output file; if not provided, output is written to stdout")
	flag.StringVar(&versionString, "version", "", "version string to filter tags (e.g. 4.20)")
	flag.StringVar(&prevVersionOverride, "prev-version", "", "previous channel version to use instead of auto-calculating (required when --version has minor=0, e.g. --version 5.0 --prev-version 4.22)")
	flag.Parse()

	if imageURL == "" {
		log.Fatal("--image is required")
	}
	if versionString == "" {
		log.Fatal("--version is required")
	}

	out := os.Stdout
	if outputFile != "" {
		f, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("failed to open output file: %w", err)
		}
		defer func() {
			if closeErr := f.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to close output file: %v\n", closeErr)
			}
		}()
		out = f
	}

	result, err := getDigests(execRunner, imageURL, pullSecretPath, versionString, prevVersionOverride)
	if err != nil {
		return fmt.Errorf("getting digests: %w", err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling result to JSON: %w", err)
	}
	if _, err := fmt.Fprintln(out, string(data)); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
