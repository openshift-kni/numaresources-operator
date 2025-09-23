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

package mustgather

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func FindFilesUnderDir(mustgatherDir string, suffix string) (map[string]string, error) {
	foundDir, err := getDirPath(mustgatherDir, suffix)
	if err != nil {
		return map[string]string{}, err
	}

	filesMap, err := getFilesPaths(foundDir)
	if err != nil {
		return map[string]string{}, err
	}

	return filesMap, nil
}

func getDirPath(mustgatherDir string, dirSuffix string) (string, error) {
	foundPaths := []string{}
	err := filepath.Walk(mustgatherDir, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() && strings.HasSuffix(path, dirSuffix) {
			foundPaths = append(foundPaths, path)
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to find directory %s: %v", dirSuffix, err)
	}

	if len(foundPaths) == 0 {
		return "", fmt.Errorf("no directory found ending with %s", dirSuffix)
	}

	if len(foundPaths) > 1 {
		return "", fmt.Errorf("multiple directories found ending with %s", dirSuffix)
	}

	return foundPaths[0], nil
}

func getFilesPaths(mustgatherDir string) (map[string]string, error) {
	foundFiles := map[string]string{}
	mustgatherDir = filepath.Clean(mustgatherDir)

	content, err := os.ReadDir(mustgatherDir)
	if err != nil {
		return foundFiles, fmt.Errorf("failed to read directory: %v", err)
	}
	for _, content := range content {
		if content.IsDir() {
			continue
		}

		foundFiles[content.Name()] = filepath.Join(mustgatherDir, content.Name())
	}

	return foundFiles, nil
}
