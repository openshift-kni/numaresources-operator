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

package rbac

import (
	rbacv1 "k8s.io/api/rbac/v1"
)

// TODO replace with k8s constants if/when available
const (
	apiGroupCore         = ""
	apiGroupCoordination = "coordination.k8s.io"
)

// TODO replace with k8s constants if/when available
const (
	resourceEndpoints = "endpoints"
	resourceLeases    = "leases"
)

func RoleForLeaderElection(ro *rbacv1.Role, namespace, resourceName string) {
	ro.Namespace = namespace

	for idx := 0; idx < len(ro.Rules); idx++ {
		ru := &ro.Rules[idx] // shortcut
		if ru.APIGroups[0] == apiGroupCore && ru.Resources[0] == resourceEndpoints {
			ru.ResourceNames = []string{resourceName}
		}
		if ru.APIGroups[0] == apiGroupCoordination && ru.Resources[0] == resourceLeases {
			ru.ResourceNames = []string{resourceName}
		}
	}
}

func RoleBinding(rb *rbacv1.RoleBinding, serviceAccount, namespace string) {
	for idx := 0; idx < len(rb.Subjects); idx++ {
		if serviceAccount != "" {
			rb.Subjects[idx].Name = serviceAccount
		}
		rb.Subjects[idx].Namespace = namespace
	}
}

func ClusterRoleBinding(crb *rbacv1.ClusterRoleBinding, serviceAccount, namespace string) {
	for idx := 0; idx < len(crb.Subjects); idx++ {
		if serviceAccount != "" {
			crb.Subjects[idx].Name = serviceAccount
		}
		crb.Subjects[idx].Namespace = namespace
	}
}
