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

package config

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestReadNonExistent(t *testing.T) {
	cfg, err := ReadFile("/does/not/exist")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cfg.ExcludeList != nil || cfg.TopologyManagerPolicy != "" {
		t.Errorf("unexpected data: %#v", cfg)
	}
}

func TestReadMalformed(t *testing.T) {
	_, err := ReadFile("/etc/services")
	if err == nil {
		t.Errorf("unexpected success reading unrelated data")
	}
}

func TestReadValidData(t *testing.T) {
	content := []byte(testData)
	tmpfile, err := ioutil.TempFile("", "testrteconfig")
	if err != nil {
		t.Errorf("creating tempfile: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write(content); err != nil {
		t.Errorf("writing content into tempfile: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Errorf("closing the tempfile: %v", err)
	}
	cfg, err := ReadFile(tmpfile.Name())
	if err != nil {
		t.Errorf("unexpected error reading back the config: %v", err)
	}
	if cfg.TopologyManagerPolicy != "restricted" {
		t.Errorf("unexpected values: %#v", cfg)
	}
	if cfg.TopologyManagerScope != "pod" {
		t.Errorf("unexpected values: %#v", cfg)
	}
	if cfg.ExcludeList["masternode"][0] != "memory" {
		t.Errorf("unexpected values: %#v", cfg)
	}
}

const testData string = `resources:
  reservedcpus: "0"
  resourcemapping:
    "8086:1520": "intel_sriov_netdevice"
topologymanagerpolicy: "restricted"
topologymanagerscope: "pod"
excludelist:
  masternode: [memory, device/exampleA]
  workernode1: [memory, device/exampleB]
  workernode2: [cpu]`
