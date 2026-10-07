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

// RTE operand RBAC escalation permissions.
//
// The operator creates and updates the "rte" ClusterRole (sourced from
// vendor/github.com/k8stopologyawareschedwg/deployer/pkg/manifests/yaml/rte/clusterrole.yaml).
// Kubernetes RBAC prevents a subject from granting permissions it does not
// already hold, so the operator's own ClusterRole must include every
// permission present in the RTE operand's ClusterRole.
//
// These annotations exist solely to satisfy that escalation requirement.
// The operator itself never exercises these permissions (some overlap
// with the operator's own needs, such as nodes list, but they are listed
// here explicitly so the escalation rationale is self-documenting).
//
// Future improvement: ship the operand ClusterRoles as static manifests
// in the OLM bundle (pre-deploy) instead of creating them in the reconciler.
// The operator would then only create ClusterRoleBindings, which does not
// require escalation privileges, and all annotations below can be removed.

package controller

//+kubebuilder:rbac:groups=topology.node.k8s.io,resources=noderesourcetopologies,verbs=get;list;create;update
//+kubebuilder:rbac:groups="",resources=nodes,verbs=watch;list
