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

// This tool prints a JSON representation of:
// - unique digests for all vX.Y.* tags in the current image repository
// - the digest of the previous-channel image URL
// - optionally, the digest of the EUS-channel image URL

// cmdRunner executes a named command and returns its trimmed stdout or an error
// that includes stderr when available.
type cmdRunner func(name string, args ...string) (string, error)

// listTagsOutput matches the JSON structure returned by `skopeo list-tags`.
type listTagsOutput struct {
	Tags []string `json:"Tags"`
}

// imageRef is a repository reference plus a tag of the form vX.Y.
type imageRef struct {
	Repository string // e.g. registry.redhat.io/openshift4/noderesourcetopology-scheduler-rhel9
	Tag        string // e.g. v4.20
	Version    string // e.g. 4.20 (Tag without leading "v")
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

// parseImageURL splits a full image URL with a vX.Y tag into repository and version.
// Expected form: registry.example.com/path/image:v4.20
// NOTE: no validation is done on the image URL format, so the user must ensure it is valid.
func parseImageURL(fullURL string) (imageRef, error) {
	fullURL = strings.TrimSpace(fullURL)
	if fullURL == "" {
		return imageRef{}, fmt.Errorf("image URL is empty")
	}

	idx := strings.LastIndex(fullURL, ":")
	if idx < 0 {
		return imageRef{}, fmt.Errorf("image URL %q missing tag; expected form registry/image:vX.Y", fullURL)
	}
	// Avoid treating host:port as a tag separator when no tag is present.
	// A valid tag always starts with 'v' after the last colon for our use case.
	repo := fullURL[:idx]
	tag := fullURL[idx+1:]
	if repo == "" || tag == "" {
		return imageRef{}, fmt.Errorf("image URL %q missing repository or tag; expected form registry/image:vX.Y", fullURL)
	}
	if !strings.HasPrefix(tag, "v") {
		return imageRef{}, fmt.Errorf("image URL tag %q must be of form vX.Y", tag)
	}
	version := strings.TrimPrefix(tag, "v")
	parts := strings.SplitN(version, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return imageRef{}, fmt.Errorf("image URL tag %q must be of form vX.Y", tag)
	}
	// Reject patch-level tags like v4.20.1 — the tool expects channel tags vX.Y (minor)
	if strings.Contains(parts[1], ".") {
		return imageRef{}, fmt.Errorf("image URL tag %q must be of form vX.Y (no patch component)", tag)
	}
	if _, err := strconv.Atoi(parts[0]); err != nil {
		return imageRef{}, fmt.Errorf("image URL tag %q must be of form vX.Y where X and Y are integers", tag)
	}
	if _, err := strconv.Atoi(parts[1]); err != nil {
		return imageRef{}, fmt.Errorf("image URL tag %q must be of form vX.Y where X and Y are integers", tag)
	}

	return imageRef{Repository: repo, Tag: tag, Version: version}, nil
}

// getDigests fetches unique digests for all vX.Y.* tags from currentURL's repository,
// the digest of prevURL, and optionally the digest of eusURL.
func getDigests(run cmdRunner, pullSecretPath string, currentURL string, prevURL string, eusURL string) (schedulerapi.Digests, error) {
	current, err := parseImageURL(currentURL)
	if err != nil {
		return schedulerapi.Digests{}, fmt.Errorf("parsing --current-url: %w", err)
	}
	prev, err := parseImageURL(prevURL)
	if err != nil {
		return schedulerapi.Digests{}, fmt.Errorf("parsing --prev-url: %w", err)
	}

	raw, err := run("skopeo", skopeoListTagsArgs(current.Repository, pullSecretPath)...)
	if err != nil {
		return schedulerapi.Digests{}, fmt.Errorf("listing tags for %s: %w", currentURL, err)
	}

	var tagsOut listTagsOutput
	if err := json.Unmarshal([]byte(raw), &tagsOut); err != nil {
		return schedulerapi.Digests{}, fmt.Errorf("parsing list-tags output: %w", err)
	}

	tags := filterTags(tagsOut.Tags, current.Version)
	if len(tags) == 0 {
		return schedulerapi.Digests{}, fmt.Errorf("no tags found matching version %q in %s", current.Version, current.Repository)
	}

	digests := schedulerapi.Digests{}
	for _, tag := range tags {
		digest, err := run("skopeo", skopeoInspectArgs(current.Repository, pullSecretPath, tag)...)
		if err != nil {
			return schedulerapi.Digests{}, fmt.Errorf("inspecting tag %s: %w", tag, err)
		}

		// only add the digest if it's not already in the set
		if slices.Contains(digests.CurrentChannel, digest) {
			continue
		}

		digests.AddCurrentChannel(digest)
	}

	latestOfPrev, err := run("skopeo", skopeoInspectArgs(prev.Repository, pullSecretPath, prev.Tag)...)
	if err != nil {
		return schedulerapi.Digests{}, fmt.Errorf("failed to get digest of previous channel (%s:%s): %v", prev.Repository, prev.Tag, err)
	}
	digests.PreviousChannelLast = latestOfPrev

	if eusURL != "" {
		eus, err := parseImageURL(eusURL)
		if err != nil {
			return schedulerapi.Digests{}, fmt.Errorf("parsing --eus-url: %w", err)
		}
		latestOfEUS, err := run("skopeo", skopeoInspectArgs(eus.Repository, pullSecretPath, eus.Tag)...)
		if err != nil {
			return schedulerapi.Digests{}, fmt.Errorf("failed to get digest of EUS channel (%s:%s): %v", eus.Repository, eus.Tag, err)
		}
		digests.EUSChannelLast = latestOfEUS
	}

	return digests, nil
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
	currentURL := ""
	prevURL := ""
	eusURL := ""
	pullSecretPath := ""
	outputFile := ""

	flag.StringVar(&currentURL, "current-url", "", "full image URL for the current channel with a vX.Y tag (e.g. registry.redhat.io/openshift4/noderesourcetopology-scheduler-rhel9:v4.20)")
	flag.StringVar(&prevURL, "prev-url", "", "full image URL for the previous channel with a vX.Y tag (e.g. registry.redhat.io/openshift4/noderesourcetopology-scheduler-rhel9:v4.19)")
	flag.StringVar(&eusURL, "eus-url", "", "optional full image URL for the EUS channel with a vX.Y tag (e.g. registry.redhat.io/openshift4/noderesourcetopology-scheduler-rhel9:v4.18)")
	flag.StringVar(&pullSecretPath, "pull-secret", "", "path to the pull secret JSON file for registry authentication")
	flag.StringVar(&outputFile, "output", "", "path to output file; if not provided, output is written to stdout")
	flag.Parse()

	if currentURL == "" {
		log.Fatal("--current-url is required")
	}
	if prevURL == "" {
		log.Fatal("--prev-url is required")
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

	result, err := getDigests(execRunner, pullSecretPath, currentURL, prevURL, eusURL)
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
