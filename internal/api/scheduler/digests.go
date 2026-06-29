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

package scheduler

import (
	"encoding/json"
	"os"
	"regexp"

	_ "embed"

	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	// SchedulerImageValidationEnvVar is the environment variable that controls whether the scheduler image validation is enabled.
	// If it is set to "false", the scheduler image validation is disabled; any other value is considered as enabled.
	SchedulerImageValidationEnvVar = "SCHEDULER_IMAGE_VALIDATION"
)

var (
	//go:embed _digests.json
	digestsData string

	// sha256DigestRE is a regular expression to match a sha256 digest at the end of a string as
	// per the OCI and docker registry specifications, e.g. it matches strings of exactly this shape:
	// sha256:a3f1b2c4d5e6...  (64 hex characters)
	sha256DigestRE = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

func GetImageValidationData() ImageValidation {
	val, ok := os.LookupEnv(SchedulerImageValidationEnvVar)
	if ok && val == "false" {
		return ImageValidation{
			Enabled: false,
			Digests: sets.New[string](),
		}
	}

	return ImageValidation{
		Enabled: true,
		Digests: loadDigests(),
	}
}

func loadDigests() sets.Set[string] {
	var d Digests
	if err := json.Unmarshal([]byte(digestsData), &d); err != nil {
		panic(err)
	}

	digests := sets.New(d.CurrentChannel...)
	digests.Insert(d.PreviousChannelLast)
	return digests
}

func IsValidDigest(digest string) bool {
	return sha256DigestRE.MatchString(digest)
}
