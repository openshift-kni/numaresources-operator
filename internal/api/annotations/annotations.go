/*
Copyright 2024.

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

package annotations

const (
	// introduced in: 4.18
	// remove in: 4.24 (tentative) or when 4.18 becomes the last supported version
	SELinuxPolicyConfigAnnotation = "config.node.openshift-kni.io/selinux-policy"
	SELinuxPolicyCustom           = "custom"

	// introduced in: 4.18
	// remove in: 4.22 (or 4.24?)
	// MultiplePoolsPerTreeAnnotation an annotation used to re-enable the support of multiple node pools per tree; starting 4.18 it is disabled by default
	// the annotation is on when it's set to "enabled", every other value is equivalent to disabled
	MultiplePoolsPerTreeAnnotation = "config.node.openshift-kni.io/multiple-pools-per-tree"
	MultiplePoolsPerTreeEnabled    = "enabled"

	// introduced in: 4.18
	// remove in: never
	PauseReconciliationAnnotation        = "config.numa-operator.openshift.io/pause-reconciliation"
	PauseReconciliationAnnotationEnabled = "enabled"

	// introduced in: 4.19
	// remove in: never (?)
	NRTAPIDefinitionAnnotation = "config.numa-operator.openshift.io/nrt-api-definition"
	NRTAPIFromCluster          = "cluster" // trust whatever it is already in the cluster, if at all
)

func IsCustomPolicyEnabled(annot map[string]string) bool {
	if v, ok := annot[SELinuxPolicyConfigAnnotation]; ok && v == SELinuxPolicyCustom {
		return true
	}
	return false
}

func IsMultiplePoolsPerTreeEnabled(annot map[string]string) bool {
	if v, ok := annot[MultiplePoolsPerTreeAnnotation]; ok && v == MultiplePoolsPerTreeEnabled {
		return true
	}
	return false
}

func IsPauseReconciliationEnabled(annot map[string]string) bool {
	if v, ok := annot[PauseReconciliationAnnotation]; ok && v == PauseReconciliationAnnotationEnabled {
		return true
	}
	return false
}

func IsNRTAPIDefinitionCluster(annot map[string]string) bool {
	if v, ok := annot[NRTAPIDefinitionAnnotation]; ok && v == NRTAPIFromCluster {
		return true
	}
	return false
}
