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

// Scheduler operand RBAC escalation permissions.
//
// The operator creates and updates the "topology-aware-scheduler" ClusterRole
// (see pkg/numaresourcesscheduler/manifests/yaml/clusterrole.nrt.yaml).
// Kubernetes RBAC prevents a subject from granting permissions it does not
// already hold, so the operator's own ClusterRole must include every
// permission present in the scheduler operand's ClusterRole.
//
// These annotations exist solely to satisfy that escalation requirement.
// The operator itself never exercises these permissions.
//
// Future improvement: ship the operand ClusterRoles as static manifests
// in the OLM bundle (pre-deploy) instead of creating them in the reconciler.
// The operator would then only create ClusterRoleBindings, which does not
// require escalation privileges, and all annotations below can be removed.

package controller

//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=delete
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=patch;update
//+kubebuilder:rbac:groups="",resources=pods/binding;bindings,verbs=create
//+kubebuilder:rbac:groups="",resources=replicationcontrollers;services;namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims;persistentvolumes,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
//+kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch;update
//+kubebuilder:rbac:groups=apps;extensions,resources=replicasets,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
//+kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch
//+kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=create
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,resourceNames=numa-scheduler-leader,verbs=get;list;update;watch
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leasecandidates,verbs=create;delete;deletecollection;get;list;patch;update;watch
//+kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses;csinodes;csidrivers;csistoragecapacities;volumeattachments,verbs=get;list;watch
//+kubebuilder:rbac:groups=resource.k8s.io,resources=deviceclasses;resourceclaims;resourceslices,verbs=get;list;watch
//+kubebuilder:rbac:groups=topology.node.k8s.io,resources=noderesourcetopologies,verbs=get;list;watch
