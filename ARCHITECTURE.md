# NUMA Resources Operator architecture

## concepts

The NUMA Resources Operator is a pilot operator to deploy all the components needed for the out-of-tree NUMA aware scheduler:
1. NodeResourceTopology API (out-of-tree CRD-based API)
2. ResourceTopologyExporter (RTE) as topology info provider
3. The out-of-tree secondary scheduler with the NodeResourceTopologyMatch plugins enabled

In addition, the operator repo includes a extensive e2e test suite (so-called "serial suite")

## building blocks

The operator is built on top of two main community-based projects: [the deployer toolkit](https://github.com/k8stopologyawareschedwg/deployer)
and [the resource topology exporter](https://github.com/k8stopologyawareschedwg/resource-topology-exporter).

The deployer toolkit provides functionalities to process the YAML manifests (or their golang object representation) to install all the components
in the NUMA-aware stack. It's the core on which the operator builds on, adding operator-specific customizations.

The resource-topology-exporter is vendored to enable the "bundled operand" pattern (see below). The operand reuses most of the RTE code, just
replacing the main entry point.

Logically, the operator manages three operands (API, RTE, scheduler) but it depends on just a single artifact for the operand (the scheduler),
while the other two operands (API, RTE) are bundled in the same operator image.

## bundled operand

Instead of consuming a separate image like we do for the secondary scheduler, we bundle the RTE operand adding the binary in the operator image,
which has multiple entry points.

This decision was taken in order to minimize the number of artifacts and narrow down as much as possible the dependency chain.

We foresee RTE as the first operand to be replaced, and in general is the easiest to replace. `nfd-topology-updater` is already part of NFD
and a supported component. On the other hand, the secondary scheduler is likely to stick around for a while more - and that's also easier to
support because secondary schedulers are not uncommon in the kubernetes ecosystem.

## controllers

The operator has three controllers and three control loop. Likewise well-formed controllers, they are independent from each other and will
reconcile the cluster state towards a functional NUMA-aware scheduler installation.

1. the numaresourcesoperator controller manages the NRT API and the RTE daemonsets. There's no obvious place to manage the NRT API, so we
   decide to bundle in the same controller which manages RTE, because the RTE is the component which suffers (slightly) more the lack of
   the API, so it makes sense to ensure its presence.
2. the numaresourcesscheduler controller manages the secondary scheduler. It's a minimal replacement for the secondary-scheduler-operator
   with some custom additions specific to the NUMA-aware scheduler. It's relatively isolated in the codebase because it was the last
   addition which was performed when key gaps where identified in the secondary-scheduler-controller for the NUMA-aware usecase.
3. the kubeletconfig controller distills the topology manager configuration from the Machine Config Operator  and makes it available
   to the RTEs to properly propagate the information. RTEs can fetch the same information in many ways (e.g. reading the host's kubelet
   configuration). This approach was taken because it is a good compromise between security, practicality, maintainability, but being
   the least critical controller this can perhaps replaced in the future. This functionality should be subsumed by the operator
   managing the RTE replacement.
