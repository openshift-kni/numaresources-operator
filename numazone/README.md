# numazone device plugin

The `numazone` device plugin exposes the synthetic extended resource
`node.openshift-kni.io/numazone`.

This plugin steers kubelet NUMA placement through the classic device plugin API.
It does not represent real hardware and it does not provide a hard placement
guarantee on its own.

## Operating modes

`-mode=enforcing` is the default and enables the placement, synchronization,
reconciliation, metrics, and error logging described below.

Use `-mode=passthrough` as an emergency fallback when NUMA spread enforcement
must be bypassed. Workloads keep their existing numazone requests and limits.
The plugin continues registering with kubelet and serving a fixed inventory
with the same device IDs and pool sizing, but every device is healthy and has no
NUMA topology. Kubelet can allocate these devices without a numazone topology
constraint, allowing workloads to operate with degraded NUMA placement.

In passthrough mode, `Allocate()` immediately returns an empty successful
response for each requested container. It does not validate device IDs, acquire
state or synchronization locks, record speculative allocations or admission
metrics, poll podresources, or arm the watchdog. The plugin never creates a
podresources client and runs no allocation reconcile loop, spread diagnostics,
or observed-allocation metrics. The `--preferred-spare`, admission synchronization,
and pending-allocation settings have no effect in this mode.

Changing `-mode` takes effect when the plugin process restarts. Keep the existing
pool-size configuration to preserve the device inventory. The advertised pool
still bounds resource availability; passthrough guarantees success in the plugin
allocation handler, while kubelet still owns scheduling and admission. Kubelet
device-plugin registration and sysfs discovery remain necessary to serve that
inventory. The HTTP metrics endpoint still follows `--metrics-bind-address`.

## Operating model

The plugin advertises a **fixed** pool of synthetic devices per NUMA node and
periodically reconciles the advertised inventory against the kubelet
`podresources` API.

The pool size per NUMA node is stable for the lifetime of the process: devices
are never added or removed in steady state. What the plugin changes dynamically
is only the **health** (`Healthy` / `Unhealthy`) of the pool devices.

The plugin steers with a **winner-takes-all** health policy:

- allocated devices are always advertised as `Healthy`
- the least-allocated NUMA node or nodes (the *winners*) advertise their whole
  free pool as `Healthy`
- every other NUMA node (the *losers*) advertises **zero** healthy free devices;
  all of their unallocated devices are `Unhealthy`

Because loser nodes advertise zero available devices, kubelet can never emit a
single-node topology hint for them for any positive request. So a workload only
needs to request a small **fixed** amount, independent of any plugin
configuration. The intended request is **exactly `1`**, but any value that fits
the winner's free pool steers correctly.

The pool size per node is set with `--pool-size`. By default (`--pool-size=0`)
each NUMA node is sized to the number of **logical CPUs kubelet detects on that
node**, so the synthetic `capacity` mirrors the real core count and numazone can
never advertise more concurrent workloads than the node physically has cores for.
An explicit `--pool-size` overrides this uniformly for all nodes.

Optionally, `--preferred-spare` caps how many free devices a winner advertises as
healthy (default `0` = expose the whole free pool). Losers always advertise zero
regardless of this value. Capping bounds how many workloads a single node can
absorb between reconciles, at the cost of a lower advertised node `allocatable`.

## Why health toggling instead of resizing the pool

Kubelet derives the node's `allocatable` for a device-plugin resource from the
number of **healthy** devices, while `capacity` is healthy **plus** unhealthy
devices. Topology-hint generation only considers devices that are healthy and
not already allocated.

Keeping the total pool fixed and flipping health therefore has two properties:

- `capacity` for the resource stays constant, independent of allocations.
- Because only *unallocated* devices are ever marked `Unhealthy`, `allocatable`
  can never drop below the number of devices already allocated.

Note that `allocatable` is **not** constant: it still moves as the winning nodes
and their free-device counts change, so the scheduler's view of this resource
remains somewhat dynamic. The guarantee is only that it never under-reports below
what is already in use.

## How this steers kubelet

Kubelet's Topology Manager, not the device plugin, makes the final placement
decision. The device plugin can only shape the set of topology hints kubelet
considers. The relevant kubelet behavior for a request of size `N` on node `X`
is:

- A single-node hint for node `X` is **emitted** only if `X` has at least `N`
  *available* (healthy, unallocated) devices.
- A hint is marked `Preferred` when its NUMA-node count equals the smallest node
  count that can satisfy the request from the *total* device inventory. Because
  the pool is fixed and larger than the request, a single node can always satisfy
  the request from its total inventory, so the preferred affinity size is one
  NUMA node.

With the winner-takes-all policy and a request of `N` (typically `1`):

- Each winner advertises its whole free pool (`>= N`), so its single-node hint is
  emitted and, being a single node, marked `Preferred`.
- Every loser advertises `0` available devices (`0 < N`), so no single-node hint
  is emitted for it.
- Multi-node masks may still be emitted, but they are never preferred because
  their NUMA-node count is greater than one.

The net effect is that the only preferred hints are the least-allocated node or
nodes. Because there is always at least one least-allocated node, a preferred
single-node hint always exists, which avoids the "no preferred hint at all" case.

To reduce the race window for back-to-back admissions, the plugin also updates
its internal state speculatively on `Allocate()`: it re-elects the winner set
after each allocation, which spreads bursts across NUMA nodes. Recent allocations
are kept in a short-lived pending set until the kubelet `podresources` API
confirms them or their TTL expires. The authoritative state still comes from
`podresources`.

## How it works

**For single-container pods, waiting inside `Allocate(A)` synchronizes the
health inventory for the next pod, B.** Pod A's NUMA affinity and device IDs
have already been selected when the plugin receives the request. This wait
cannot revise that decision.

In the inspected kubelet implementation, the relevant sequence is:

```text
AddPod(A)                       [holds kubelet admission lock]
|
+-- Topology Manager
|   +-- collect topology hints from the resource managers
|   +-- merge hints and choose NUMA affinity for A
|   +-- store A's affinity
|
+-- Device Manager
|   +-- select device IDs consistent with A's affinity
|   +-- reserve those IDs
|   +-- release Device Manager lock
|   |
|   +-- numazone.Allocate(selected IDs)
|       +-- account for A as a pending allocation
|       +-- recompute winners and device health
|       +-- signal ListAndWatch
|       +-- WAIT until kubelet reports that health inventory
|       +-- return success
|   |
|   +-- commit A's container/device assignment
|
+-- finish admission            [release admission lock]

AddPod(B)
+-- generate hints using the updated health inventory
```

The affinity-before-allocation ordering appears in Topology Manager's
`pkg/kubelet/cm/topologymanager/scope_container.go`. The reservation, RPC, and
subsequent assignment commit appear in Device Manager's
`allocateContainerResources()` in `pkg/kubelet/cm/devicemanager/manager.go`.

The race we are closing is between **returning from Allocate** and **processing
the health update**. Suppose both NUMA nodes initially have zero allocations,
and A chooses NUMA 0:

```text
Without the barrier:

A chooses NUMA 0
    |
plugin counts become (1, 0); publishes new health
    |
Allocate(A) returns
    |
B generates hints from OLD health; may also choose NUMA 0
    |
kubelet finally processes the health update
```

The barrier establishes the desired ordering:

```text
Kubelet admission        Numazone                 Other kubelet handlers
-----------------        --------                 ----------------------
A chooses NUMA 0
call Allocate(A) -------> record A as pending
                         counts = (1, 0)
                         publish new health ----> ListAndWatch receiver
                                                  updates health cache
                         poll inventory --------> GetAllocatableResources
                         <----------------------- cached healthy inventory
                         inventory matches
receive success <------- return

commit A's assignment
finish A's admission

B generates hints
using updated health
```

This works because **the two kubelet locks have different scopes**:

- The admission lock remains held across `Allocate(A)`, preventing B's
  admission from advancing. See `AddPod()` in
  `pkg/kubelet/allocation/allocation_manager.go`.
- The Device Manager lock is released before calling the plugin. Its
  `ListAndWatch` receiver and inventory query can therefore acquire that lock
  while A's RPC is waiting.

The poll provides confirmation, rather than merely allowing some time to pass.
`GetAllocatableResources()` reaches `GetAllocatableDevices()` in
`pkg/kubelet/cm/devicemanager/manager.go`, which reads the cached healthy
inventory under the same lock used to update it. Numazone compares the exact
device-ID and topology sets.

There is one important distinction:

```text
GetAllocatableResources(): healthy inventory, INCLUDING allocated devices

Devices available for B:  healthy inventory MINUS allocated devices
```

After A chooses NUMA 0, numazone keeps A's device Healthy, marks NUMA 0's
remaining free devices Unhealthy, and leaves NUMA 1's free devices Healthy.
Once kubelet observes that inventory, subtracting allocated devices leaves B
with available devices on NUMA 1.

We cannot instead wait for **A's assignment to appear in podresources `List()`**:
kubelet commits that assignment after `Allocate(A)` returns. Waiting for it
inside the RPC would create a circular dependency.

Two qualifications apply: container scope can make another topology decision
for a later container in the same pod, whereas pod scope chooses affinity once
for the whole pod. Also, a soft timeout returns success through the fail-open
path, so the ordering guarantee applies when synchronization succeeds.

## Synchronous admission synchronization

Container-scope pod admission is serialized by kubelet, but kubelet does not
normally wait for a `ListAndWatch` update to reach its Device Manager cache
before starting the next admission. Updating the plugin state synchronously in
`Allocate()` is therefore not sufficient by itself: the next admission can
still generate topology hints from the previous health inventory.

To close this window, numazone uses `Allocate()` as a bounded synchronization
barrier by default:

1. Serialize numazone `Allocate()` handlers.
2. Record the requested device IDs as pending allocations and recompute device
   health.
3. Publish the resulting inventory through `ListAndWatch`.
4. Poll the kubelet `podresources` v1 `GetAllocatableResources()` RPC until the
   devices reported for `node.openshift-kni.io/numazone` exactly match the
   healthy device ID and topology set published in step 3.
5. Return the successful `AllocateResponse`, allowing kubelet to complete this
   admission and start the next one.

`GetAllocatableResources()` reports the Device Manager's healthy devices,
including devices which are already allocated. This is the right state to
compare: kubelet reserves the requested device IDs in its `allocatedDevices`
set before invoking the plugin's `Allocate()` RPC, while topology-hint
generation uses the healthy set minus that allocated set. The expected health
inventory must therefore continue to include allocated devices.

The comparison is set-based and ignores response ordering. It includes each
device ID and its NUMA topology, not just the total device count. An exact match
is sufficient even though the podresources API does not expose an inventory
generation: topology-hint generation depends on the current state, not on which
update produced it.

### Why polling while `Allocate()` is blocked is safe

In kubelet, the Device Manager selects and reserves device IDs while holding its
mutex, then releases that mutex before making the plugin `Allocate()` RPC.
`ListAndWatch` processing reacquires the Device Manager mutex to replace the
cached all-device and healthy-device maps. `GetAllocatableResources()` reaches
`GetAllocatableDevices()`, which acquires the same mutex and copies the healthy
devices. Consequently, a poll observes either the complete old inventory or the
complete new inventory, never a partially applied update, and it does not wait
for the blocked `Allocate()` call to return. The podresources service and the
device-plugin client are also served by independent gRPC handlers.

The plugin must preserve the corresponding lock discipline:

- use a dedicated, context-aware allocation-synchronization gate to serialize
  the complete update, publication, and observation sequence; a plain
  `sync.Mutex` is unsuitable because waiting to acquire it cannot be cancelled
- coordinate reconciles which can change device health with that gate, so the
  target inventory stays stable while it is being observed
- release the device-state mutex before signalling `ListAndWatch` or polling
  podresources, because `ListAndWatch` needs that mutex to take its snapshot
- give `ListAndWatch` an immutable deep snapshot; it must not retain pointers to
  device records which another goroutine can mutate during gRPC serialization
- capture the expected inventory as an immutable value before starting the poll

Waiting for the allocation to appear in `ListPodResources()` cannot be used as
the barrier. Kubelet records the container's device assignment in the state
reported by `ListPodResources()` only after the plugin's `Allocate()` RPC
returns, so waiting for that observation inside `Allocate()` would deadlock.

### Bounded, fail-open behavior

Admission synchronization is enabled by default. The single master opt-out is
`--admission-sync=false`; it bypasses both the synchronization gate and polling
and the built-in watchdog, then returns from `Allocate()` immediately after the
existing speculative update and reconcile kick. There is deliberately no flag
which can leave synchronization enabled while disabling its watchdog.

When enabled, `--admission-sync-timeout` controls the soft deadline. Its default
is one second and values above the hard limit of five seconds are rejected. An
earlier cancellation or deadline from the RPC context is also honored. The
polling interval is 25 milliseconds, which stays below the podresources endpoint
rate limit, and both reconciliation and synchronization reuse a podresources
client connection created at plugin startup rather than redialing from
`Allocate()`. These defaults and limits are centralized as constants in the
plugin package so they can be reviewed and changed together.

The inspected kubelet implementation does not add a dedicated timeout to the
device-plugin `Allocate()` call, so numazone must not rely on kubelet to bound
this wait.

If the plugin's deadline expires, or the expected inventory cannot be observed,
numazone records one error log and passes the reason and wait duration to its
metrics instrumentation, then returns the successful allocation response.
This fail-open behavior preserves pod admission at the cost of temporarily
losing the spread guarantee. The speculative allocation and published inventory
remain in place so the normal asynchronous flow can still converge. If the
caller cancels the RPC context, numazone stops waiting immediately because a
response can no longer be delivered reliably.

Prometheus metrics expose soft-deadline expirations and the full duration of
completed `Allocate()` calls, labeled by outcome. Logs are emitted once per
failed barrier rather than once per poll attempt.

## Operator deployment

The NUMA Resources Operator can deploy numazone alongside RTE, using the same
node selection and tolerations for each NodeGroup. Configure the plugin through
`spec.nodeGroups[].numazone.mode` in the `nodetopology.openshift.io/v1`
`NUMAResourcesOperator` object:

```yaml
spec:
  nodeGroups:
  - poolName: worker
    numazone:
      mode: Enabled
      logLevel: Trace
```

The modes are `Disabled` (the default; no plugin DaemonSet), `Enabled`
(`--mode=enforcing`), and `Passthrough` (`--mode=passthrough`). Omitting `numazone`
or its `mode` disables the plugin. Changing modes updates the same DaemonSet;
disabling the plugin or removing its NodeGroup deletes that group's plugin
DaemonSet. Other groups keep their configured mode. For emergency degraded
operation, switch to `Passthrough` to keep advertising the resource without
enforcing NUMA spread. Mode changes are reconciled even while RTE is unready.

`numazone.logLevel` uses the operator log-level values: `Normal` (the default,
`-v=2`), `Debug` (`-v=4`), `Trace` (`-v=6`), and `TraceAll` (`-v=8`). Set
`Trace` to follow an allocation flow by its `flowID`.

The plugin binary is bundled in the operator image. Its pods run as privileged
root with a dedicated service account and OpenShift SCC to access the kubelet
Unix sockets. The device-plugin directory is mounted read/write; sysfs and the
podresources directory are mounted read-only. The pods do not request the
synthetic resource they manage. They expose the existing metrics endpoint on
container port 8080.

`status.nodeGroups[].numazoneDaemonSet` reports the plugin DaemonSet's namespace
and name beside the RTE `daemonsets` reference, and is omitted for disabled
groups. Plugin deployment errors and rollout progress feed the operator's
existing conditions. The top-level `status.daemonsets` continues to list RTE
DaemonSets.

See [the multi-group example](../doc/examples/numazone.yaml).

## Prometheus metrics

Numazone serves an HTTP `/metrics` endpoint on `:8080` by default. Use
`--metrics-bind-address` to change the listening address, or set it to `0` to
disable the endpoint.

- `numazone_admission_sync_soft_timeouts_total` counts expirations of the
  plugin's admission synchronization soft deadline, including time spent waiting
  for the synchronization gate and polling failures that last until that deadline.
  Caller cancellations, caller deadlines, and immediate observation errors do
  not increment this counter.
- `numazone_allocate_duration_seconds` is a histogram of the full duration of
  each completed `Allocate()` call in seconds, including gate waiting,
  synchronization, and failure handling. Its `outcome` label is one of `success`,
  `deadline`, `observation_error`, `caller_canceled`, `request_error`, or
  `disabled`. Buckets span 1 millisecond through 6 seconds, covering the polling
  interval, default soft deadline, and maximum soft deadline plus watchdog grace.
- `numazone_allocated_devices{numa_node="…"}` is a gauge of the number of
  allocated numazone devices on each NUMA node, from the last successful kubelet
  `podresources` query. Every NUMA node discovered at startup has a series,
  initially zero; the topology is assumed to remain fixed while the plugin runs.
  Successful reconciles replace the counts, including setting them back to zero
  when allocations are released. Query failures retain the last observed counts.
  Pending speculative allocations are excluded. This measures allocated resource
  units, rather than pods or CPU utilization; a request of `1` contributes one
  device. The snapshot follows the reconcile cadence, normally every two seconds
  and also triggered after allocations.

Hard-watchdog expirations are not recorded because the process exits immediately.

## Diagnostic logging

Soft-deadline expirations emit one error log explicitly identifying the soft
timeout and fail-open behavior, with the elapsed duration, configured timeout,
and synchronization outcome. Hard-watchdog expiry writes an `ERROR` diagnostic
directly to standard error announcing that self-kill is in progress and giving
the exit status; it does not use the normal logger or its locks.

Every successful reconcile logs at info severity when the difference between
the largest and smallest kubelet-reported NUMA allocation counts exceeds one
device. The log
includes every discovered NUMA node's count, including zero allocations, the
minimum and maximum, and the allowed difference. Pending speculative allocations
are excluded. This diagnoses the observed imbalance; uneven releases, requests
larger than one, and other topology constraints can also trigger it.

## Built-in hard watchdog recovery

The bounded synchronization wait handles expected failures such as a delayed
`ListAndWatch` update, a rejected podresources request, or a lost podresources
connection. It cannot by itself recover if an implementation bug prevents the
timeout path from running. Examples include blocking on an uncancellable lock,
waiting on a goroutine which never reports completion, or attempting timeout
cleanup while holding a lock needed by that cleanup.

This failure is especially disruptive because kubelet is waiting synchronously
for the device-plugin `Allocate()` response. While the call is stuck, kubelet's
pod-admission serialization remains held and its main pod update loop can also
remain inside admission. A normal DaemonSet update which changes numazone flags
is therefore not a reliable way to terminate the stuck process: kubelet may be
unable to process that update until the blocked RPC has already ended.

When admission synchronization is enabled, numazone therefore also enables a
built-in hard watchdog. The watchdog is a last-resort process recovery mechanism,
not another retry or admission policy:

1. Arm it before attempting to acquire the allocation-synchronization gate or
   performing any other potentially blocking synchronization work.
2. Give the normal synchronization path its shorter soft deadline. Expiry of
   that deadline remains fail-open: record the failure and return a successful
   allocation response.
3. Give the watchdog a hard deadline exactly one second after the configured
   soft deadline, allowing a bounded interval for the soft-timeout path to
   unwind and return.
4. Disarm it only after the `Allocate()` handler has completed its synchronization
   work and is ready to return.
5. If the hard deadline wins, emit a minimal best-effort diagnostic directly to
   standard error and terminate the process immediately with a non-zero status.

The watchdog completion state is tracked with atomics or a dedicated channel
which is independent of plugin state locks, the allocation-synchronization gate,
gRPC connections, and logging infrastructure. An atomic active/completed/fired
transition prevents a stale timer callback from terminating the process after a
successful allocation. The hard-expiry path calls `os.Exit` directly and does
not run deferred cleanup, flush buffered logs, update in-process metrics, or
acquire locks; any of those operations could reproduce the deadlock the watchdog
is intended to break.

Process termination closes the device-plugin connection. Kubelet then receives
an error from the blocked `Allocate()` RPC, discards its tentative allocation,
unwinds the failed admission, and releases admission serialization. The affected
workload pod is failed rather than retried by kubelet; a workload controller must
create its replacement. After kubelet is unblocked, it can restart and register
numazone and process an updated plugin configuration. The numazone pod itself
must not request the synthetic resource that it manages.

The watchdog requires no additional Go library. It does require an existing
external process restart policy, such as a DaemonSet pod with
`restartPolicy: Always` or a system service with restart enabled. In-process
metrics cannot reliably record hard-watchdog expiry because the process exits
before they can be scraped. Container restart metrics are the authoritative
external signal; a subsequent process may additionally report a persisted
watchdog marker if durable local state is configured. Normal soft-deadline
failures continue to be reported through numazone logs and metrics before
`Allocate()` returns.

The watchdog protects against ordinary Go control-flow and locking deadlocks. It
cannot run if the entire process is unschedulable, for example after `SIGSTOP` or
while blocked by a kernel failure. Those cases still require an external
supervisor or node-level recovery.

## Sizing and the workload request

The workload request is decoupled from the plugin configuration:

- **Request `1`** (recommended). Any positive request up to a winner's free-pool
  size also steers correctly, so occasional drift to `2`, `4`, ... still works.
- **`--pool-size`** (default = per-node logical CPUs) sets the stable capacity and
  the maximum number of concurrent workloads a node can hold.
- **`--preferred-spare`** (default `0` = unlimited) optionally caps a winner's
  advertised available devices for burst control.

Because nodes can have different logical CPU counts, the pool is sized per NUMA
node. A node with at least one free device can always win and host a request of
`1`, so there is no minimum-`Q` constraint as in earlier revisions.

## Examples

The intended pod requests `1` unit of `node.openshift-kni.io/numazone`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: numazone-example
spec:
  containers:
  - name: workload
    image: quay.io/openshift-kni/pause:test-ci
    command: ["/pause"]
    resources:
      requests:
        node.openshift-kni.io/numazone: "1"
      limits:
        node.openshift-kni.io/numazone: "1"
```

The same pattern applies in higher-level workload objects:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: numazone-example
spec:
  replicas: 1
  selector:
    matchLabels:
      app: numazone-example
  template:
    metadata:
      labels:
        app: numazone-example
    spec:
      containers:
      - name: workload
        image: quay.io/openshift-kni/pause:test-ci
        command: ["/pause"]
        resources:
          requests:
            node.openshift-kni.io/numazone: "1"
          limits:
            node.openshift-kni.io/numazone: "1"
```

For extended resources, requests and limits must match.

## Assumptions

This approach works best when all of the following are true:

- Workloads request a small fixed amount (recommended `1`) of
  `node.openshift-kni.io/numazone`.
- The resource is used only as a synthetic placement signal.
- The plugin can query a healthy kubelet `podresources` API.
- The kubelet Topology Manager is enabled and is considering this resource's
  single-NUMA device topology.
- The reconcile loop is up to date enough that the advertised inventory closely
  tracks the current allocation state.
- The pending allocation TTL is long enough for `podresources` to reflect a
  successful allocation, but short enough to recover from failed starts.
- The pool size is large enough for the expected number of concurrent workloads
  per node.

## Caveats

This plugin improves the bias toward least-allocated NUMA nodes, but it does
not guarantee kubelet will always choose the least-allocated node.

Known caveats:

- Kubelet still owns the final topology decision. The device plugin can shape
  the hint space, but it does not directly choose the winning topology hint.
- Other topology-aware subsystems also influence the result, including CPU
  Manager, Memory Manager, and other device-plugin resources.
- Allocation state is learned through `podresources`, so there is an inherent
  delay between an allocation change and the next reconcile.
- Concurrent admissions can observe slightly stale synthetic inventories and may
  choose the same NUMA node before the reconcile loop catches up.
- Ties are intentionally unbiased. If two or more NUMA nodes are equally least
  allocated, kubelet may choose any of them.
- The resource is synthetic, so the reported `allocatable` value is also
  synthetic and changes as allocations change. It never drops below the amount
  already allocated, but it is not constant. `capacity` is stable.
- The advertised device IDs are stable, but their health is derived from observed
  allocations, so which specific devices are healthy changes over time.

## Things to pay attention to

The following situations deserve extra attention because they can weaken or
break the intended flow:

- Back-to-back admissions on the same node can race with the reconcile loop. A
  second workload may still observe the old synthetic inventory before the first
  workload's allocation is reflected through `podresources`. The speculative
  pending-allocation path narrows this window, but does not remove it entirely.
  A smaller `--preferred-spare` bounds how many workloads a single node absorbs
  during such a burst.
- Mixed workloads, where some pods use `numazone` and others do not, can make
  debugging harder because `numazone` influences only the pods that request it.
- Kubelet restarts, plugin restarts, or temporary `podresources` unavailability
  can delay convergence while the plugin re-derives device health from observed
  allocations. After a restart with a different pool size, externally observed
  allocations whose device IDs fall outside the current pool are adopted as
  healthy and pruned once they are released.
- Failed or aborted starts can temporarily skew the bias because speculative
  allocations are applied before they are confirmed by `podresources`. The
  pending allocation TTL bounds how long this skew can last.
- Additional topology-aware constraints from CPU, memory, or other device
  plugin resources can overrule the preferred `numazone` placement.
- Ties are expected behavior. If two or more NUMA nodes are equally least
  allocated, the plugin deliberately does not break the tie.
- A node whose pool is fully allocated advertises zero available devices and
  drops out of the winner set until some of its devices are released.

## Note on GetPreferredAllocation

By the time kubelet calls `GetPreferredAllocation()`, the Topology Manager has
already chosen the NUMA affinity, and kubelet passes the plugin only the device
IDs already located on the chosen NUMA node.
The plugin therefore cannot influence NUMA node selection from that call;
it could only pick among fungible device IDs on an already-decided node,
which has no effect on placement. All steering comes from the advertised
per-NUMA health/inventory.
