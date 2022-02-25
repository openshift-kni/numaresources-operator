# NUMA Resources Operator

Operator to allow to expose the per-NUMA-zone compute resources, using the [RTE - resource topology exporter](https://github.com/openshift-kni/resource-topology-exporter).
The operator also takes care of deploying the [Node Resource Topology API](https://github.com/k8stopologyawareschedwg/noderesourcetopology-api) on which the resource topology exporter depends to provide the data.
The operator provides minimal support to deploy [secondary schedulers](https://github.com/openshift-kni/scheduler-plugins).

## current limitations

* the NUMA-aware scheduling stack does not yet support [the "container" topology manager scope](https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#topology-manager-scopes)
* the NUMA-aware scheduling stack does not yet support alignment of [memory resources](https://kubernetes.io/docs/tasks/administer-cluster/memory-manager/)

Please check the issues section for the known issues and limitations of the NUMA resources operator.

## running the e2e suite against your cluster

The NUMA resources operator comes with a growing e2e suite to validate components of the stack (operator proper, RTE) as well as the NUMA aware scheduling as a whole.
Pre-built container images including the suites [are available](https://quay.io/repository/openshift-kni/numaresources-operator-tests?tab=info).
There is **no support** for these e2e tests images, and they are recommended to be used only for development/CI purposes.

The e2e suite can *optionally* setup/teardown the numaresources stack, but the suite expects a pre-configured kubeletconfig.
Please find [here](https://raw.githubusercontent.com/openshift-kni/numaresources-operator/main/doc/examples/kubeletconfig.yaml) an example of recommended kubeletconfig.

The e2e suite can set a default `kubeletconfig`, but this is not recommended. The recommended flow is to pre-configure the cluster with a `kubeletconfig` object.
Should you decide to use the default `kubeletconfig`, please omit the `-e E2E_NROP_INSTALL_SKIP_KC=true` from all the `podman` command lines below.

The e2e suite assumes the cluster has the numaresources operator installed, but with no configuration. To install the numaresources operator, you can use the vehicle which best suits your use case (OLM, `make deploy`...).

To actually run the tests and the tests only, assuming a pre-configured numaresources stack
```
podman run -ti -v $KUBECONFIG:/kubeconfig:z -e KUBECONFIG=/kubeconfig -e E2E_NROP_INSTALL_SKIP_KC=true quay.io/openshift-kni/numaresources-operator-tests:4.11.999-snapshot
```

To setup the stack from scratch and then run the tests (you may want to do that with *ephemeral* CI clusters)
```
podman run -ti -v $KUBECONFIG:/kubeconfig:z -e KUBECONFIG=/kubeconfig -e E2E_NROP_INSTALL_SKIP_KC=true quay.io/openshift-kni/numaresources-operator-tests:4.11.999-snapshot --setup
```

To setup the stack, run the tests and then restore the pristine cluster state:
```
podman run -ti -v $KUBECONFIG:/kubeconfig:z -e KUBECONFIG=/kubeconfig -e E2E_NROP_INSTALL_SKIP_KC=true quay.io/openshift-kni/numaresources-operator-tests:4.11.999-snapshot --setup --teardown
```
