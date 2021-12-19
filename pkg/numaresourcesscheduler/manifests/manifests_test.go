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
 * Copyright 2021 Red Hat, Inc.
 */

package manifests

import (
	"testing"
)

func TestLoad(t *testing.T) {
	if obj, err := ServiceAccount(""); obj == nil || err != nil {
		t.Errorf("ServiceAccount() failed: err=%v", err)
	}
	if obj, err := ClusterRole(); obj == nil || err != nil {
		t.Errorf("ClusterRole() failed: err=%v", err)
	}
	if obj, err := ClusterRoleBindingK8S(""); obj == nil || err != nil {
		t.Errorf("ClusterRoleBindingK8S() failed: err=%v", err)
	}
	if obj, err := ClusterRoleBindingNRT(""); obj == nil || err != nil {
		t.Errorf("ClusterRoleBindingNRT() failed: err=%v", err)
	}
	if obj, err := ConfigMap(""); obj == nil || err != nil {
		t.Errorf("ConfigMap() failed: err=%v", err)
	}
	if obj, err := Deployment(""); obj == nil || err != nil {
		t.Errorf("Deployment() failed: err=%v", err)
	}
}
