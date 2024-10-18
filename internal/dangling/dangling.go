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

// Package dangling takes care of detecting and removing dangling object from removed NodeGroups.
// If we edit nodegroups in such a way that a set of nodes previously managed is no longer managed, the related MachineConfig is left lingering.
// We need to explicit cleanup objects with are 1:1 with NodeGroups because NodeGroups are not a separate object, and the main NRO object is set as
// the owner of all the generated object. But in this scenario (NodeGroup no longer managed), the main NRO object is NOT deleted, so the dependant
// objects are left unnecessarily lingering. In hindsight, we should probably have set a NUMAResourcesOperator own NodeGroups, or just allow more than
// a NUMAResourcesOperator object, but that ship as sailed and now a NUMAResourcesOperator object is 1:N to NodeGroups (and the latter are not K8S objects).
package dangling
