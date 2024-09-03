# e2e serial testsuite

The "serial" e2e test suite holds e2e tests covering topology-aware scheduling as a whole.

For this purposes "end to end" means the cluster, taken as black box, with minimal breakages to this rule
only iff and when these breakages are extremely helpful (point in case: the numacell plugin).
All these cases are vetted anyway, and the goal is to gradually minimize or eliminate them all.

Because of the fact that "e2e" is the cluster here, the suite expects to run against an already configured
and TAS-enabled cluster, no matter how (using the numaresources-operator proper or any other vehicle).
We intentionally avoid any explicit dependency (e.g. against the "install" test suite for this reason).

All the tests in this suite create, manipulate, destroy cluster resources. They are intrinsically disruptive
and want to exercise access to exclusive resources. Hence the "serial" name: the tests expect to
run in isolation with any other workload, and in isolation to each other.

IOW, the suite expects to run against a pristine, unloaded cluster to validate it.

It is responsibility of the tests in this suite to clean up properly and to restore the cluster state as
they found it before they run.

### configuring using the environment variables

- `E2E_NROP_INSTALL_SKIP_KC` (accepts boolean, e.g. `true`) instructs the suite to NOT deploy the builtin
  `kubeletconfig`. With this enabled, the suite will expect a `kubeletconfig` already deployed (and consumed)
  in the cluster against it is running.
- `E2E_NROP_MCP_UPDATE_TIMEOUT` (accepts a duration, e.g. `5m`) instructs the suite how much time it should
  wait for the MCP updates to take place before to exit with error.
- `E2E_NROP_MCP_UPDATE_INTERVAL` (accepts a duration, e.g. `10s`) instructs the suite about how much it should
  wait between checks for MCP updates.
- `E2E_NROP_PLATFORM` (accepts a string, e.g. `Kubernetes`, `OpenShift`) instructs the suite to *disable* the
  autodetection of the platform name and force it to the provided value.
- `E2E_NROP_PLATFORM_VERSION` (accepts a string, e.g. `1.30`, `4.18`) instructs the suite to *disable* the
  autodetection of the platform version and force it to the provided value.
- `E2E_NROP_DUMP_EVENTS` (accepts boolean, e.g. `true`) requests the suite to dump events pertaining to pods
  failed unexpectedly on standard output, alongside (not replacing) the logging of the said events.
- `E2E_NROP_TARGET_NODE` (accepts string, e.g. `node-0.my-cluster.io`) instructs the suite to always pick the
  node matching the given name when a random node is needed.
- `E2E_NROP_COOLDOWN_THRESHOLD` (accepts integer, e.g 1, 3) instructs the suite to collect data of resources
  topology matching to the initial data for the specified threshold count, before considering it settled.
- `E2E_NROP_TEST_COOLDOWN` (accepts string expressing time unit, e.g. `30s`) instructs the suite to wait
  for the specified amount of time after each spec, to give time to the cluster to settle up.
- `E2E_NROP_TEST_TEARDOWN` (accepts string expressing time unit, e.g. `30s`) instructs the suite to wait
  *up* to the specified amount of time while tearing down the resources needed by each spec.
- `E2E_NROP_TEST_SETTLE_INTERVAL` (accepts string expressing time unit, e.g. `5s`) instructs the suite to poll
  every given time unit to get the current NRT data, in order to check if the values are settled.
- `E2E_NROP_TEST_SETTLE_TIMEOUT` (accepts string expressing time unit, e.g. `1m`) instructs the suite to wait
  *up* to the specified amount of time while checking if the NRT data is settled.
- `E2E_NROP_DEVICE_TYPE_1` (accepts string, e.g `example.com/deviceA`) declares name of a device type that exists on
  the cluster and will be used as a resource of type `device-type-1` in the tests.
- `E2E_NROP_DEVICE_TYPE_2` (accepts string, e.g `example.com/deviceB`) declares name of a device type that exists on
  the cluster and will be used as a resource of type `device-type-2` in the tests.
- `E2E_NROP_DEVICE_TYPE_3` (accepts string, e.g `example.com/deviceC`) declares name of a device type that exists on
  the cluster and will be used as a resource of type `device-type-3` in the tests.
- `E2E_RTE_CI_IMAGE` (accepts string, e.g `quay.io/openshift-kni/resource-topology-exporter:test-ci`) sets the 
  RTE image to be used for testing purposes particularly for modifying the operator object.
  
### Tests tagging 

- Tagging tests to a specific feature functionality were done by adding tags in the spec description like `It("[rtetols][distruptive]...")`.
Starting Jul 2024, while the master branch is pointing to 4.18, the new preferred way to tag tests is using ginkgo `Label("")` such as `Context("should run pods requesting host-level resources", Label("hostlevel","distruptive"), func(){..})`. Tags aren't forbidden yet; they are just deprecated, so whenever a tag is added, a label is also required.
- Each test should have the importance tag which is one of the below:
`tier0` means critical; `tier1` means important; `tier2` means medium priority; `tier3` means low priority test.