# NUMA Resources Operator

Operator to allow to expose the per-NUMA-zone compute resources, using the [RTE - resource topology exporter](https://github.com/openshift-kni/resource-topology-exporter).
The operator also takes care of deploying the [Node Resource Topology API](https://github.com/k8stopologyawareschedwg/noderesourcetopology-api) on which the resource topology exporter depends to provide the data.
The operator provides minimal support to deploy [secondary schedulers](https://github.com/openshift-kni/scheduler-plugins).

## current limitations

Please check the [issues section](https://github.com/openshift-kni/numaresources-operator/issues) for the known issues and limitations of the NUMA resources operator.

## running the e2e suite against your cluster

The NUMA resources operator comes with a growing e2e suite to validate components of the stack (operator proper, RTE) as well as the NUMA aware scheduling as a whole.
Pre-built container images including the suites [are available](https://quay.io/repository/openshift-kni/numaresources-operator-tests).
There is **no support** for these e2e tests images, and they are recommended to be used only for development/CI purposes.

See `README.tests.md` for detailed instructions about how to run the suite.
See `tests/e2e/serial/README.md` for fine details about the suite and developer instructions.
