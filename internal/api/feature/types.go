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
 * Copyright 2024 Red Hat, Inc.
 */

package feature

import (
	"fmt"

	goversion "github.com/aquasecurity/go-version/pkg/version"
)

const (
	Version = "v1.0.0"
)

type Metadata struct {
	// semantic versioning vX.Y.Z, e.g. v1.0.0
	Version string `json:"version"`
}

type TopicInfo struct {
	Metadata Metadata `json:"metadata"`
	// unsorted list of topics active (supported)
	Active []string `json:"active"`
}

func NewTopicInfo() TopicInfo {
	return TopicInfo{
		Metadata: Metadata{
			Version: Version,
		},
	}
}

func (tp TopicInfo) Validate() error {
	if tp.Metadata.Version == "" {
		return fmt.Errorf("metadata: missing version")
	}
	if _, err := goversion.Parse(tp.Metadata.Version); err != nil {
		return fmt.Errorf("metadata: malformed version: %w", err)
	}
	if len(tp.Active) == 0 {
		return fmt.Errorf("missing active topics")
	}
	return nil
}

func (tp TopicInfo) Clone() TopicInfo {
	return TopicInfo{
		Metadata: tp.Metadata,
		Active:   append([]string{}, tp.Active...),
	}
}
