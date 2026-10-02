# Numazone e2e suite

This suite is a stripped copy of the serial suite. It runs against an already
configured, unloaded TAS cluster, in isolation from other tests and workloads.
RTE must report the worker topology and Topology Manager must use
`single-numa-node`, as in the serial suite. The inherited kubelet/NRT consistency
and SMT configuration checks still apply.

Numazone must already be `Enabled` in at least one NUMAResourcesOperator
NodeGroup. Missing, Disabled, or exclusively Passthrough deployments fail the
suite. The suite neither enables the plugin nor installs a scheduler or numacell.
The plugin must use admission synchronization and its default HTTP metrics
endpoint on port 8080. The operator image must contain `/bin/curl`, following the
existing metrics e2e tests' convention.

The Guaranteed and Burstable burst cases derive from tests 85784/85785 in
`scheduler_cache_stall.go`, with cleanup and diagnostics from `scheduler_cache.go`.
The suite keeps scenarios in `tests/`, setup in `config/`, and metrics, workload,
and plugin observation helpers in `test/internal/numazone`, following the serial
suite's package layout.
Each case targets one ready node with exactly two NUMA zones and no existing
numazone allocations. Every pod requests one numazone unit, 100m CPU, and 64Mi
memory. Guaranteed pods have matching limits; Burstable pods have a 200m CPU
limit. Fractional CPU avoids exclusive CPU hints dominating the placement test.

The batch contains up to 100 pods, bounded by synthetic capacity, available pod
slots, and remaining CPU and memory requests. Among eligible nodes, the suite
chooses a node supporting the largest batch, or `E2E_NROP_TARGET_NODE` when set.
An ineligible explicit target fails. Nodes supporting fewer than four pods are
unsuitable and the tests skip when none qualifies; missing numazone fails.
The assumptions of unloaded nodes and equivalent NUMA zones are inherited from
the serial tests. The selected batch size is always logged.

Ten concurrent API submission workers use a local client with sufficient QPS
and burst capacity. There are no admission/readiness waits between creates.
Pods are bound directly using `spec.nodeName` so a scheduler cannot spread or
pace the batch. Both submission time and total completion time are logged.

The tests require:

- Every pod reaches Running on the selected node with the expected QoS class.
- Podresources-derived allocation gauges converge to the batch size, retain
  every NUMA node's series, and have a maximum-minus-minimum spread of at most one.
- The soft-timeout counter does not increase.
- There is exactly one successful Allocate histogram observation per pod, with
  no increase in other outcomes. Aggregate and mean Allocate times are logged.
- The plugin pod UID, container ID, and restart count remain unchanged.

The spread check uses the plugin's podresources-derived gauges, not a separate
podresources client. It verifies the converged live batch rather than every
intermediate allocation. Namespace cleanup removes all workload pods, followed
by a wait for the allocation gauges to return to zero. Failures dump pod events
and plugin logs.

Submission concurrency reduces artificial spacing, but cluster API latency may
still pace the batch. The logged batch size and submission/completion times must
be assessed when evaluating whether a run produced sufficient admission load.

Build and run:

```sh
make binary-e2e-numazone
make test-numazone-e2e
```

Pass Ginkgo options directly to `hack/run-test-numazone-e2e.sh`, for example:

```sh
hack/run-test-numazone-e2e.sh --ginkgo.focus=Guaranteed
```

`KUBECONFIG`, `E2E_NROP_TARGET_NODE`, `E2E_NROP_VERBOSE`, platform overrides,
and the shared fixture cleanup/settling environment variables retain their
serial-suite meanings. On HyperShift also set
`HYPERSHIFT_MANAGEMENT_CLUSTER_KUBECONFIG` as for the serial suite.

Static checks, without running tests:

```sh
make binary-e2e-numazone vet
bash -n hack/run-test-numazone-e2e.sh
```

Helper unit tests and test discovery, for human evaluation without a cluster:

```sh
make test-unit-numazone-e2e
bin/e2e-nrop-numazone.test --ginkgo.dry-run --ginkgo.v
```
