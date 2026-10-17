/*
 * Copyright 2026 Red Hat, Inc.
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

package numazone

import (
	"testing"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

func TestGetManifests(t *testing.T) {
	for _, plat := range []platform.Platform{platform.OpenShift, platform.HyperShift, platform.Kubernetes} {
		t.Run(string(plat), func(t *testing.T) {
			mf, err := GetManifests(plat, "test")
			if err != nil {
				t.Fatal(err)
			}
			if mf.DaemonSet.Namespace != "test" || mf.ServiceAccount.Namespace != "test" {
				t.Fatal("incorrect manifest namespace")
			}
			if mf.DaemonSet.Spec.Template.Spec.ServiceAccountName != mf.ServiceAccount.Name {
				t.Fatal("daemonset references a different service account")
			}
			if plat == platform.Kubernetes {
				if mf.SecurityContextConstraint != nil {
					t.Fatal("SCC must only be deployed on OpenShift platforms")
				}
			} else {
				if !mf.SecurityContextConstraint.AllowPrivilegedContainer || mf.SecurityContextConstraint.Users[0] != "system:serviceaccount:test:numazone" {
					t.Fatal("SCC does not authorize the plugin service account")
				}
			}
			cloned := mf.Clone()
			cloned.DaemonSet.Spec.Template.Spec.Containers[0].Args[0] = "changed"
			if mf.DaemonSet.Spec.Template.Spec.Containers[0].Args[0] == "changed" {
				t.Fatal("clone shares container arguments")
			}
			container := mf.DaemonSet.Spec.Template.Spec.Containers[0]
			if container.SecurityContext.RunAsUser == nil || *container.SecurityContext.RunAsUser != 0 || !*container.SecurityContext.Privileged {
				t.Fatal("plugin lacks host socket access")
			}
			_, requestsResource := container.Resources.Requests["node.openshift-kni.io/numazone"]
			_, limitsResource := container.Resources.Limits["node.openshift-kni.io/numazone"]
			if requestsResource || limitsResource {
				t.Fatal("plugin must not request the resource it manages")
			}
			foundDevicePlugins := false
			for _, mount := range container.VolumeMounts {
				if mount.Name == "device-plugins" {
					foundDevicePlugins = mount.MountPath == "/var/lib/kubelet/device-plugins" && !mount.ReadOnly
				}
			}
			if !foundDevicePlugins {
				t.Fatal("missing writable kubelet device plugin socket mount")
			}
		})
	}
}
