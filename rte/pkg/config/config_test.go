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
	"errors"
	"os"
	"testing"

	"k8s.io/klog"
	"sigs.k8s.io/yaml"
)

func TestReadNonExistent(t *testing.T) {
	cfg, err := readFile("/does/not/exist")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cfg.PodExclude != nil || cfg.Kubelet.TopologyManagerPolicy != "" {
		t.Errorf("unexpected data: %#v", cfg)
	}
}

func TestReadMalformed(t *testing.T) {
	_, err := readFile("/etc/services")
	if err == nil {
		t.Errorf("unexpected success reading unrelated data")
	}
}

func TestReadValidData(t *testing.T) {
	content := []byte(testData)
	tmpfile, err := os.CreateTemp("", "testrteconfig")
	if err != nil {
		t.Errorf("creating tempfile: %v", err)
	}
	defer func(name string) {
		if err := os.Remove(name); err != nil {
			t.Errorf("error while removing tempfile: %v", err)
		}
	}(tmpfile.Name())

	if _, err := tmpfile.Write(content); err != nil {
		t.Errorf("writing content into tempfile: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Errorf("closing the tempfile: %v", err)
	}
	cfg, err := readFile(tmpfile.Name())
	if err != nil {
		t.Errorf("unexpected error reading back the config: %v", err)
	}
	if cfg.Kubelet.TopologyManagerPolicy != "restricted" {
		t.Errorf("unexpected values: %#v", cfg)
	}
	if cfg.Kubelet.TopologyManagerScope != "pod" {
		t.Errorf("unexpected values: %#v", cfg)
	}
	if cfg.ResourceExclude["masternode"][0] != "memory" {
		t.Errorf("unexpected values: %#v", cfg)
	}
}

func readFile(configPath string) (Config, error) {
	conf := Config{}
	data, err := os.ReadFile(configPath)
	if err != nil {
		// config is optional
		if errors.Is(err, os.ErrNotExist) {
			klog.Warningf("Info: couldn't find configuration in %q", configPath)
			return conf, nil
		}
		return conf, err
	}
	err = yaml.Unmarshal(data, &conf)
	return conf, err
}

const testData string = `resources:
  reservedcpus: "0"
  resourcemapping:
    "8086:1520": "intel_sriov_netdevice"
kubelet:
  topologymanagerpolicy: "restricted"
  topologymanagerscope: "pod"
resourceExclude:
  masternode: [memory, device/exampleA]
  workernode1: [memory, device/exampleB]
  workernode2: [cpu]`
