/*
 * Copyright 2025 Red Hat, Inc.
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

package config

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func DumpEnvironment(w io.Writer) {
	fmt.Fprintf(w, "E2E NROP environment variables dump BEGIN\n")
	defer fmt.Fprintf(w, "E2E NROP environment variables dump END\n")
	for _, keyVal := range os.Environ() {
		if !looksLikeE2ENROPEnv(keyVal) {
			continue
		}
		fmt.Fprintf(w, "- %s\n", keyVal)
	}
}

func looksLikeE2ENROPEnv(s string) bool {
	return strings.HasPrefix(s, "E2E_NROP_") || strings.HasPrefix(s, "E2E_RTE_")
}
