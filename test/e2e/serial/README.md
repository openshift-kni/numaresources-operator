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

It is responsability of the tests in this suite to clean up properly and to restore the cluster state as
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
  autodetection of the platform and force it to the provided value.
