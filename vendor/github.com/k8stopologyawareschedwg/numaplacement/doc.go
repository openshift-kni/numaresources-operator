// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Red Hat, Inc.

// Package numaplacement efficiently encodes the per-NUMA placement of
// containers with exclusively assigned resources, so they can themselves
// be considered "affine" to a NUMA node.
//
// The encoding uses LEB89, a self-delimiting variable-length encoding that
// maps non-negative integers to sequences of printable ASCII characters.
// See leb89/doc.go for details.
//
// Both the node agent and the scheduler independently produce the same
// deterministic ordered list of container hashes, and per-NUMA
// offset vectors reference positions in that list.
// This package is expected to be in concert with the noderesourcetopology-api
// kubernetes datatype, but does not depend on it.

package numaplacement
