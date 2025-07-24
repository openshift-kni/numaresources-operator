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

/*
 * Package dangling takes care of detecting and removing dangling object from removed NodeGroups.
 * If we edit nodegroups in such a way that a set of nodes previously managed is no longer managed,
 * the related MachineConfig is left lingering.
 * We need to explicit cleanup objects with are 1:1 with NodeGroups because NodeGroups are not a
 * separate object, and the main NRO object is set as the owner of all the generated object.
 * But in this scenario (NodeGroup no longer managed), the main NRO object is NOT deleted,
 * so the dependant objects are left unnecessarily lingering.
 *
 * Ultimately, we need the dangling package and cleanup or orphaned resources because we can't
 * effectively leverage the kubernetes automatic GC and cascade deletion.
 * This is enabled by correct setting of OwnerReference, which we can't do because we only have
 * a singleton NUMAResourcesOperator object.
 * In the current model, the singleton NUMAResourcesOperator objects includes N NodeGroups
 * definitions. Logically, DSes, NRTs, MachineConfigs and all the objects which can be orphaned
 * are pertaining to a NodeGroup, and should have the OwnerReference pointing to that.
 * But we don't have N NodeGroup objects, we only have a singleton NUMAResourcesOperator object.
 * A better design would thus to have 1 NUMAResourcesOperator and N of these objects
 * (still part of the NROP CRD)
 *
 * [An arrow from an object to another means "is referenced by", or equivalently "owns" either
 * through a ObjectReference or through a simpler NamespacedName]
 *
 * NUMAResourcesOperator +--> NUMAResourcesNodeGroup +--> NodeResourceTopology
 *                       |                           |
 *                       |                           +--> DaemonSet
 *                       |                           |
 *                       |                           `--> MachineConfig (obsolete since 4.18)
 *                       |
 *                       +--> NUMAResourcesNodeGroup +--> NodeResourceTopology
 *                       |                           |
 *                       |                           +--> DaemonSet
 *                       |                           |
 *                       |                           `--> MachineConfig (obsolete since 4.18)
 *                       |
 *                     [...]
 *                       |
 *                       `--> NUMAResourcesNodeGroup +--> NodeResourceTopology
 *                                                   |
 *                                                   +--> DaemonSet
 *                                                   |
 *                                                   `--> MachineConfig (obsolete since 4.18)
 *
 * It is likely it is feasible to fix this design mistake without a API bump.
 * We can introduce the objects, add the references in the NUMAResourcesOperator object,
 * and make the operator itself fan out the objects embedded in the NUMAResourcesOperator
 * into the new ones automatically for few releases (or forever with a parameter).
 * However, this is a bunch of work especially to guarantee backward compatibility and
 * no regression; and the design has to be put to the test.
 * So, more likely, for the time being we are stuck with this mess we created, and we will
 * have to clean it up manually using the facilities of the `dangling` package.
 */
package dangling
