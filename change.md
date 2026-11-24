# Deprecate the external deployer and remove `pkg/numaresourcesoperator/`

## Goal

Two things:
1. Stop depending on the external `github.com/k8stopologyawareschedwg/deployer` module.
2. Have **nothing** live under `pkg/numaresourcesoperator/` - that folder no longer exists.

Everything deployer-derived now lives in the operator's `internal/` tree (or beside
its only consumer), and the external module is dropped from `go.mod`/`go.sum`/`vendor/`.

## Where each piece went (and why)

Two different treatments were applied, depending on whether the code had an existing
home:

### Integrated function-level into existing packages
These had a natural existing home, so their functions were merged in rather than kept
as separate copies:

- The deployment-deleted wait helper → added as `ForDeploymentDeleted` **inside the
  existing** `internal/wait` (matches the existing `ForPodDeleted` style). The deployer's
  separate `wait` package was dropped - `internal/wait` already had the
  `Waiter`/`With`/`Interval`/`Timeout` machinery.
- The validator's used surface (`ValidationResult`, the area constants,
  `ValidateClusterNodeKubeletConfig`) → moved **into** `nrovalidate/validator`, its only
  consumer. The `deployervalidator` wrapper import was removed from the callers.
- `deployer.Environment` → folded **into** `internal/clientutil` as `clientutil.Environment`
  (it is a client/context/log bundle and `EnsureClient` already uses `clientutil.New`). The
  separate `deployer` package name is gone.

### Relocated as cohesive packages into `internal/`
These are self-contained units with no existing same-purpose package - and several are
**foundational layers that NROP's own `pkg/` packages build on**, so merging them would
break that layering or collide. They were moved whole into `internal/<name>`:

| from | to | note |
|---|---|---|
| `pkg/numaresourcesoperator/clientutil` | `internal/clientutil` | client helpers + `Environment` |
| `pkg/numaresourcesoperator/assets` | `internal/assets` | embedded RTE/SELinux assets |
| `pkg/numaresourcesoperator/flagcodec` | `internal/flagcodec` | flag encode/decode util |
| `pkg/numaresourcesoperator/images` | `internal/images` | RTE default image refs |
| `pkg/numaresourcesoperator/manifests` | `internal/manifests` | operator/RTE manifest rendering + embedded YAML |
| `pkg/numaresourcesoperator/objectupdate` | `internal/objectupdate` | low-level object mutation |
| `pkg/numaresourcesoperator/options` | `internal/options` | deployment option types |
| `pkg/numaresourcesoperator/platform` | `internal/platform` | platform/version types + detection |

Why these were **not** merged into same-named existing packages:
- `pkg/objectupdate/rte` **calls** `internal/objectupdate/rte` (e.g. its `ContainerConfig`
  wraps the deployer's `ContainerConfig`). They are two layers, not duplicates, and both
  export `ContainerConfig` - merging would be a circular/colliding mess.
- `pkg/images` (operator image discovery) and `internal/images` (RTE default image refs)
  serve different concerns; there is no function duplication.
- `internal/platform` is a base types package that `internal/platforminfo` and ~39 other
  callers import; it is not the same thing as `platforminfo`.

There is **no function duplication** between the relocated packages and existing code.

## Dead code removed

While trimming, these unused symbols were deleted:
- `clientutil` client constructors `NewK8sExt`, `NewTopologyClient`
- the unreachable wait helpers (CRD/DaemonSet waiters, `ForDeploymentComplete` and its
  by-key variant, `ForNamespaceDeleted`, `SetBaseValues`, `ObjectKeyFromObject`)
- the `Validator` type machinery, the dead `Environment.CreateObject`/`DeleteObject`/
  `WithName` methods, and the unused `ComponentFeatureGates` constant
- the now-unused `kubeletconfig` package

## Commit layout

Three commits on top of `upstream/main`:

1. **Import deployer libraries** - copy the remaining deployer packages in-tree
   (`internal/clientutil`, `internal/wait` addition), relocate the packages #6010 had put
   under `pkg/numaresourcesoperator/` into `internal/`, repoint every importer, and drop
   the external module from `go.mod`/`go.sum`/`vendor/`.
2. **Integrate deployer libraries** - trim each imported package to the API NROP actually
   uses; delete what is left unused.
3. **Simplify deployer-derived APIs** - move the validator's used pieces beside their only
   consumer in `nrovalidate/validator`.

## Verification

- `go build ./...` and `go vet ./...` - OK
- `golangci-lint` - 0 issues, on each of the three commits
- `make test-unit-pkgs` - pass (0 failures)
- `make test-controllers` - pass
- 0 references to `k8stopologyawareschedwg/deployer` outside `vendor/` (which no longer
  vendors it), and 0 references to `pkg/numaresourcesoperator/`
