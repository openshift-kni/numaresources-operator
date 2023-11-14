/*
 * Copyright 2023 Red Hat, Inc.
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

package relatedobjects

import (
	configv1 "github.com/openshift/api/config/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
)

// 'Resource' should be in lowercase and plural
// See BZ1851214
// See https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-subdomain-names

func ResourceTopologyExporter(namespace string, dsStatuses []nropv1.NamespacedName) []configv1.ObjectReference {
	ret := []configv1.ObjectReference{
		{
			Resource: "namespaces",
			Name:     namespace,
		},
		{
			Group:    "machineconfiguration.openshift.io",
			Resource: "kubeletconfigs",
		},
		{
			Group:    "machineconfiguration.openshift.io",
			Resource: "machineconfigs",
		},
		{
			Group:    "topology.node.k8s.io",
			Resource: "noderesourcetopologies",
		},
	}

	for _, dsStatus := range dsStatuses {
		ret = append(ret, configv1.ObjectReference{
			Group:     "apps",
			Resource:  "daemonsets",
			Namespace: dsStatus.Namespace,
			Name:      dsStatus.Name,
		})
	}

	return ret
}

func Scheduler(namespace string, dpStatus nropv1.NamespacedName) []configv1.ObjectReference {
	return []configv1.ObjectReference{
		{
			Resource: "namespaces",
			Name:     namespace,
		},
		{
			Group:     "apps",
			Resource:  "deployments",
			Namespace: dpStatus.Namespace,
			Name:      dpStatus.Name,
		},
	}
}
