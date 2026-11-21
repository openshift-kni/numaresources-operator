/*
 * Copyright Red Hat, Inc.
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

package kubeletconfig

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	"sigs.k8s.io/yaml"

	mcov1 "github.com/openshift/api/machineconfiguration/v1"
)

// ParseKubeletConfigFromMachineConfigRawData gets the the raw spec.config data from the MachineConfig and parses it into contents;
// out of these contents pick the one matches the kubelet configuration and decode it and return the decoded value, or an error.
func ParseKubeletConfigFromMachineConfigRawData(b []byte) ([]byte, error) {
	// we are only interested in storage.files so get that part only and implement a local version of
	// github.com/openshift/machine-config-operator/pkg/controller/common/helpers.go ParseAndConvertConfig() function
	// to avoid pulling the dependency on the MCO

	type fileContent struct {
		Source string `json:"source"`
	}
	type storageFile struct {
		Path     string      `json:"path"`
		Contents fileContent `json:"contents"`
	}

	type storage struct {
		Files []storageFile `json:"files"`
	}
	type config struct {
		Storage storage `json:"storage"`
	}

	cfg := &config{}
	err := json.Unmarshal(b, cfg)
	if err != nil {
		return nil, err
	}
	const (
		base64Prefix      = "data:text/plain;charset=utf-8;base64,"
		kubeletConfigPath = "/etc/kubernetes/kubelet.conf"
	)

	for _, file := range cfg.Storage.Files {
		if file.Path == kubeletConfigPath {
			if strings.HasPrefix(file.Contents.Source, base64Prefix) {
				base64Data := strings.TrimPrefix(file.Contents.Source, base64Prefix)
				decoded, err := base64.StdEncoding.DecodeString(base64Data)
				if err != nil {
					return nil, err
				}
				return decoded, nil
			}
		}
	}
	return nil, fmt.Errorf("kubelet config not found in MachineConfig data")
}

// BuildKubeletConfigObject gets kubelet configuration in bytes (decoded from MachineConfig) and decodes it into a MCO
// KubeletConfig object. It returns the decoded value, or an error.
func BuildKubeletConfigObject(data []byte) (*mcov1.KubeletConfig, error) {
	kc := &kubeletconfigv1beta1.KubeletConfiguration{}
	if err := yaml.Unmarshal(data, kc); err != nil {
		return nil, err
	}

	rawKc, err := json.Marshal(kc)
	if err != nil {
		return nil, err
	}

	mcoKc := &mcov1.KubeletConfig{}
	mcoKc.Spec.KubeletConfig = &runtime.RawExtension{
		Raw: rawKc,
	}
	return mcoKc, nil
}
