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

package record

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func NodeNameToFileName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}

func FileNameToNodeName(name string) string {
	return strings.ReplaceAll(name, "_", ".")
}

// DumpToFile encodes in JSON and dumps the given `obj` to a file.
// `dir` is the base directory on which the file must be created
// `file` is the name of the file to create in `dir`.
// The old file, if present, is always updated atomically.
func DumpToFile(dir, file string, obj any) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	dst, err := os.CreateTemp(dir, "__"+file)
	if err != nil {
		return err
	}
	defer os.Remove(dst.Name()) // either way, we need to get rid of this

	_, err = dst.Write(data)
	if err != nil {
		return err
	}

	err = dst.Close()
	if err != nil {
		return err
	}

	return os.Rename(dst.Name(), filepath.Join(dir, file))
}

// LoadFromFile decodes the JSON data found in `file` placed in `dir`
// and stores it into `obj`, which must be a pointer.
func LoadFromFile(dir, file string, obj any) error {
	data, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, obj)
}
