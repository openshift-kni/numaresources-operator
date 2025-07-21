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

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strings"

	goversion "github.com/aquasecurity/go-version/pkg/version"
)

const (
	versionFileName  = "Makefile"
	versionTagPrefix = `VERSION`
	// ok, this is an abuse. What we would really need is to parse the full
	// raw version and emit MAJOR.MINOR but the version parsing package
	// we are currently using doesn't allow this easily, so we leverage
	// our versioning convention.
	versionTagSuffix = `.999-snapshot`
)

// ParseVersionFromFile find an annotation like
// `VERSION ?= 4.19.999-snapshot`
// intentionally swallows any error
func ParseVersionFromFile(srcFile string) string {
	data, err := os.ReadFile(srcFile)
	if err != nil {
		return ""
	}
	return ParseVersionFromReader(bytes.NewReader(data))
}

// ParseVersionFromReader intentionally swallows any error
func ParseVersionFromReader(r io.Reader) string {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, versionTagPrefix) {
			continue
		}
		ver := findVersionValueInLine(line)
		if !strings.HasSuffix(ver, versionTagSuffix) {
			continue
		}
		ver = strings.TrimSuffix(ver, versionTagSuffix)
		if _, err := goversion.Parse(ver); err != nil {
			return "" // no point in continuing
		}
		return ver
	}
	return ""
}

func findVersionValueInLine(line string) string {
	// if we are here we know the line starts with `VERSION`
	fields := strings.FieldsFunc(line, func(c rune) bool {
		return c == '='
	})
	if len(fields) < 2 {
		return ""
	}
	// inline comments are not supported (yet?)
	return strings.TrimSpace(fields[1])
}
