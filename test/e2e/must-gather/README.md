# e2e must-gather testsuite

### configuring using the environment variables

- `E2E_NROP_INFRA_SETUP_SKIP` (accepts boolean, e.g. `true`) instructs the suite to NOT deploy on the cluster.
   Detect and use the existing setup.
- `E2E_NROP_INFRA_TEARDOWN_SKIP` (accepts boolean, e.g. `true`) instructs the suite to NOT teardown the cluster,
   leave it as is and exit.
- `E2E_NROP_MUSTGATHER_CLEANUP_SKIP` (accepts boolean, e.g. `true`) instructs the suite to NOT cleanup the local
   destination data directory (will be logged when running), useful for troubleshooting.
- `E2E_NROP_MUSTGATHER_IMAGE` (accepts string, e.g. `quay.io/openshift-kni/numaresources-must-gather`) overrides
   the hardcoded must-gather image to use.
- `E2E_NROP_MUSTGATHER_TAG` (accepts string, e.g. `4.16-snapshot`) overrides the hardcoded must-gather tag to use.
