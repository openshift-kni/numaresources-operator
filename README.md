# NUMA Resources Operator

Operator to allow to expose the per-NUMA-zone compute resources, using the [RTE - resource topology exporter](https://github.com/openshift-kni/resource-topology-exporter).
The operator also takes care of deploying the [Node Resource Topology API](https://github.com/k8stopologyawareschedwg/noderesourcetopology-api) on which the resource topology exporter depends to provide the data.
The operator provides minimal support to deploy [secondary schedulers](https://github.com/openshift-kni/scheduler-plugins).
