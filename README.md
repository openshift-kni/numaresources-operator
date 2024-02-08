# NUMA Resources Operator

Operator to allow to expose the per-NUMA-zone compute resources, using the [RTE - resource topology exporter](https://github.com/k8stopologyawareschedwg/resource-topology-exporter).
The operator also takes care of deploying the [Node Resource Topology API](https://github.com/k8stopologyawareschedwg/noderesourcetopology-api) on which the resource topology exporter depends to provide the data.
The operator provides minimal support to deploy [secondary schedulers](https://github.com/openshift-kni/scheduler-plugins).

## deploying using OLM

The currently recommended way of deploying the operator in your cluster is using [OLM](https://github.com/operator-framework/operator-lifecycle-manager/). OLM greatly simplifies webhook management, which the operator requires.
Assuming you can push container images to a container registry and you are in the root directory of this project, a deployment flow can look like:

1. fix environment variables as per your need. You will most likely need to override `VERSION` `REPO` `CONTAINER_ENGINE`
1. build and upload the operator container image: `make container-build container-push`
1. build and upload the manifest bundle container image: `make bundle bundle-build bundle-push`
1. leverage `operator-sdk` to deploy the container: `operator-sdk run bundle ${REPO}/numaresources-operator-bundle:${VERSION}`. Note the build procedure typically downloads a local copy of `operator-sdk` in `bin/` which you can reuse

For further details, please refer to the [operator-sdk documentation](https://sdk.operatorframework.io/docs/olm-integration/tutorial-bundle/)

## current limitations

Please check the [issues section](https://github.com/openshift-kni/numaresources-operator/issues) for the known issues and limitations of the NUMA resources operator.

## Additional noteworthy information

NRT objects only take into consideration exclusively allocated CPUs while accounting. In order for a pod to be allocated exclusive CPUs, it HAS to belong to Guaranteed QoS class (request=limit) and request has to be integral. Therefore, CPUs in the shared pool because of pods belonging to best effort/burstable QoS or guaranteed pod with non-integral CPU request would not be accounted for in the NRT objects. Please refer to CPU Manager docs [here](https://kubernetes.io/docs/tasks/administer-cluster/cpu-management-policies/#static-policy) for more detail on this.

In addition to this, PodResource API is used to extract the resource information from Kubelet for resource accounting. CPUs exposed by the List endpoint of Podresource API correspond to exclusive CPUs allocated to a particular container. CPUs that belong to the shared pool are therefore not exposed by this API.

## running the e2e suite against your cluster

The NUMA resources operator comes with a growing e2e suite to validate components of the stack (operator proper, RTE) as well as the NUMA aware scheduling as a whole.
Pre-built container images including the suites [are available](https://quay.io/repository/openshift-kni/numaresources-operator-tests).
There is **no support** for these e2e tests images, and they are recommended to be used only for development/CI purposes.

See `README.tests.md` for detailed instructions about how to run the suite.
See `tests/e2e/serial/README.md` for fine details about the suite and developer instructions.
