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
	"io/fs"
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
	configletPath, err := filepath.EvalSymlinks(filepath.Clean(filepath.Join(configletDir, configletName)))
	if err != nil {
		return "", err
	}
	if filepath.Dir(configletPath) != configletDir {
		return "", fmt.Errorf("configlet %q is not within %q", configletName, configletDir)
	}
	return configletPath, nil
}

func validateConfigRootPath(configRoot string) (string, error) {
	if configRoot == "" {
		return "", fmt.Errorf("configRoot is not allowed to be an empty string")
	}

	configRoot = filepath.Clean(configRoot)
	cfgRoot, err := filepath.EvalSymlinks(configRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// reset to original value, it somehow passed the symlink check
			cfgRoot = configRoot
		} else {
			return "", fmt.Errorf("failed to validate configRoot path: %w", err)
		}
	}
	// else either success or checking a non-existing path. Which can be still OK.

	pattern, err := IsConfigRootAllowed(cfgRoot, UserRunDir, UserHomeDir)
	if err != nil {
		return "", err
	}
	if pattern == "" {
		return "", errors.New("failed to validate configRoot path: not matches any allowed pattern")
	}

	// clear pattern to make the tool happy
	relPath := strings.TrimPrefix(cfgRoot, pattern)
	return filepath.Abs(filepath.Clean(filepath.Join(pattern, relPath)))
}

func IsConfigRootAllowed(cfgPath string, addDirFns ...func() (string, error)) (string, error) {
	allowedPatterns := []string{
		"/etc/rte",
		"/run/rte",
		"/var/rte",
		"/usr/local/etc/rte",
	}
	for _, addDirFn := range addDirFns {
		userDir, err := addDirFn()
		if err != nil {
			return "", err
		}
		allowedPatterns = append(allowedPatterns, userDir)
	}

	for _, pattern := range allowedPatterns {
		if strings.HasPrefix(cfgPath, pattern) {
			return pattern, nil
		}
	}
	return "", nil
}
