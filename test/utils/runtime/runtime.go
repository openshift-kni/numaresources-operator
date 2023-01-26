/*
 * Copyright 2021 Red Hat, Inc.
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

package runtime

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"

	"k8s.io/klog/v2"
)

func GetRootPath() (string, error) {
	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot retrieve tests directory")
	}
	basedir := filepath.Dir(file)
	return filepath.Abs(filepath.Join(basedir, "..", "..", ".."))
}

func GetBinariesPath() (string, error) {
	root, err := GetRootPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin"), nil
}

func FindBinaryPath(exe string) (string, error) {
	root, err := GetRootPath()
	if err != nil {
		return "", err
	}
	binpaths := []string{
		// source tree
		filepath.Join(root, "bin"),
		// prow CI
		root,
	}
	for _, binpath := range binpaths {
		fullpath := filepath.Join(binpath, exe)
		klog.Infof("trying path %q", fullpath)

		info, err := os.Stat(fullpath)
		if err != nil {
			klog.Warningf("skipping path %q (does not exist err=%v)", fullpath, err)
			continue
		}

		if !IsExecOwner(info.Mode()) {
			klog.Warningf("skipping path %q (not executable mode=%v)", fullpath, info.Mode())
			continue
		}

		klog.Infof("found path %q", fullpath)
		return fullpath, nil
	}

	return "", fmt.Errorf("cannot find %q in candidate paths", exe)
}

func IsExecOwner(mode os.FileMode) bool {
	return mode&0100 != 0
}

func listDir(path string) {
	files, err := os.ReadDir(path)
	if err != nil {
		klog.Warningf("failed to list %q: %v", path, err)
		return
	}
	var buf bytes.Buffer
	for _, file := range files {
		fmt.Fprintf(&buf, "%s: %s\n", file.Name(), file.Type().String())
	}
	klog.Infof("listing content of path %q:\n%s", path, buf.String())
}
