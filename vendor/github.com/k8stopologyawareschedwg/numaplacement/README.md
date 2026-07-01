[![Go Reference](https://pkg.go.dev/badge/github.com/k8stopologyawareschedwg/numaplacement.svg)](https://pkg.go.dev/github.com/k8stopologyawareschedwg/numaplacement)

# numaplacement: efficient per-NUMA container placement encoding

This package provides wire-efficient encoding of the NUMA placement of kubernetes containers.
"NUMA Placement" means the single NUMA node from which a container gets all its exclusively assigned resources,
like devices, CPU cores, memory areas. If, for example, a container C1 has all resources assigned and pinned
to the same NUMA node N, we therefore classify C1 with NUMA Affinity to N (C1=N).
Multi-affinity is not supported yet.

## Status

pre-alpha. Subject to change without notice. Please wait for tagged and official releases.

## Example

A runnable end-to-end encoder/decoder example is available in:

- `examples/codec/main.go`

Run it with:

```bash
go run ./examples/codec
```

## LICENSE

apache v2

