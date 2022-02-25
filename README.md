# NUMA Resources Operator

Operator to allow to expose the per-NUMA-zone compute resources, using the [RTE - resource topology exporter](https://github.com/openshift-kni/resource-topology-exporter).
The operator also takes care of deploying the [Node Resource Topology API](https://github.com/k8stopologyawareschedwg/noderesourcetopology-api) on which the resource topology exporter depends to provide the data.
The operator provides minimal support to deploy [secondary schedulers](https://github.com/openshift-kni/scheduler-plugins).

## current limitations

* the NUMA-aware scheduling stack does not yet support [the "container" topology manager scope](https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#topology-manager-scopes)
* the NUMA-aware scheduling stack does not yet support alignment of [memory resources](https://kubernetes.io/docs/tasks/administer-cluster/memory-manager/)

Please check the issues section for the known issues and limitations of the NUMA resources operator.
