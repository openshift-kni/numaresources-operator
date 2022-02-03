# numacell device plugin

The `numacell` device plugin expose fake (and infinite) devices bound to the NUMA cells found in the system.
When the kubelet's Topology Manager is configured with the `single-numa-node` policy, the workload can request to land
on a specific NUMA cell on a specific NUMA node by explicitely requiring the corresponding resource and setting
the `spec.nodeName` field.

Because of the guarantees the Topology Manager provides, that workload will either run
on the specified NUMA cell of the specified node, or not run at all.
This makes significantly easier to reproduce test scenarios.

However, using this plugin introduces a pretty strong imperative element in the declarative model of kubernetes.
It breaks layering and the resource modeling.

For these reasons, we use the `numacell` device plugin to enable easier testing, but it should be never used
outside this narrow and very specific use case.
