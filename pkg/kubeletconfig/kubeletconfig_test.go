/*
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
 *
 * Copyright 2024 Red Hat, Inc.
 */

package kubeletconfig

import (
	"errors"
	"testing"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
)

func TestMissingPayloadMCOKubeletConfToKubeletConf(t *testing.T) {
	kc, err := MCOKubeletConfToKubeletConf(&mcov1.KubeletConfig{})
	if kc != nil {
		t.Errorf("non-nil kubeletconfig from nil payload")
	}
	if !errors.Is(err, MissingPayloadError) {
		t.Errorf("unexpected error from nil payload; %v", err)
	}
}

func TestMissingPayloadKubeletConfToMCKubeletConf(t *testing.T) {
	k8sKc := kubeletconfigv1beta1.KubeletConfiguration{}
	mcoKc := mcov1.KubeletConfig{}
	err := KubeletConfToMCKubeletConf(&k8sKc, &mcoKc)
	if !errors.Is(err, MissingPayloadError) {
		t.Errorf("unexpected error from nil payload; %v", err)
	}
}
