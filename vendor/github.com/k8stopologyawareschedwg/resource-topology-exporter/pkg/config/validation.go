/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	metricssrv "github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/metrics/server"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcemonitor"
)

func Validate(pArgs *ProgArgs) error {
	var err error

	pArgs.RTE.MetricsMode, err = metricssrv.ServingModeIsSupported(pArgs.RTE.MetricsMode)
	if err != nil {
		return err
	}

	pArgs.Resourcemonitor.PodSetFingerprintMethod, err = resourcemonitor.PFPMethodIsSupported(pArgs.Resourcemonitor.PodSetFingerprintMethod)
	if err != nil {
		return err
	}

	return nil
}

func validateConfigLetPath(configletDir, configletName string) (string, error) {
	configletPath, err := filepath.EvalSymlinks(filepath.Join(configletDir, configletName))
	if err != nil {
		return "", err
	}
	fullPath, err := filepath.Abs(filepath.Clean(configletPath))
	if err != nil {
		return "", err
	}
	if filepath.Dir(fullPath) != configletDir {
		return "", fmt.Errorf("configlet %q is not within %q", configletName, configletDir)
	}
	return fullPath, nil
}

func validateConfigRootPath(configRoot string) (string, error) {
	if configRoot == "" {
		return "", fmt.Errorf("configRoot is not allowed to be an empty string")
	}

	cfgRoot, err := filepath.EvalSymlinks(configRoot)
	if err != nil {
		return "", fmt.Errorf("failed to validate configRoot path: %w", err)
	}

	// Resolve and clean the input path
	cfgRoot, err := filepath.Abs(filepath.Clean(cfgRoot))
	if err != nil {
		return "", fmt.Errorf("failed to validate configRoot path: %w", err)
	}

	allowedPatterns := []string{
		"/etc/rte",
		"/run/rte",
		"/var/rte",
		"/usr/local/etc/rte",
	}
	var userDir string
	userDir, err = UserRunDir()
	if err != nil {
		return "", err
	}
	allowedPatterns = append(allowedPatterns, userDir)

	userDir, err = UserHomeDir()
	if err != nil {
		return "", err
	}
	allowedPatterns = append(allowedPatterns, userDir)

	ok, pattern, err := matchAny(cfgRoot, allowedPatterns)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("failed to validate configRoot path: not matches any allowed pattern")
	}

	// clear pattern to make the tool happy
	relPath := strings.TrimPrefix(cfgRoot, pattern)
	return filepath.Abs(filepath.Clean(filepath.Join(pattern, relPath)))
}

func matchAny(cfgPath string, patterns []string) (bool, string, error) {
	for _, pattern := range patterns {
		ok := strings.HasPrefix(cfgPath, pattern)
		log.Printf("path=%q pattern=%q ok=%v", cfgPath, pattern, ok)
		if ok {
			return true, pattern, nil
		}
	}
	return false, "", nil
}
