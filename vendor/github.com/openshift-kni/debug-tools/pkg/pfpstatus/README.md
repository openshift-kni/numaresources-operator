# PFPStatus

PFPStatus offer facilities to store and expose [podfingerprint](https://github.com/k8stopologyawareschedwg/podfingerprint) status info.

The status info is exposed read-only; external actors cannot alter the PFP status.
The PFP status exposes the namespace/name pairs of the pods whose container have exclusive resources assigned and which are
detected by the system. While the information discolosure is minimal, this may be undesirable, so caution is advised.

## Exposing status info

The status info can be exposed by
- dumping the content as JSON files, one per node, in the filesystem.
  We recommend to use a fixed size tmpfs to avoid unbounded storage consumption.
  The writer dumps the status content atomically and intentionally ignores write errors because assumes fixed size storage.
