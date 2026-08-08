// SPDX-License-Identifier: Apache-2.0

// LEB89 is a self-delimiting, variable-length integer encoding inspired by
// LEB128 (Little-Endian Base 128), the variable-length encoding used in
// DWARF debug info and WebAssembly. Where LEB128 splits each byte into a
// 7-bit payload and a 1-bit continuation flag over the full 0x00-0xFF range,
// LEB89 applies the same principle over an alphabet of 89 printable ASCII
// characters that are safe to embed verbatim in JSON strings.
//
// The "89" in the name refers to the alphabet size: the 94 printable ASCII
// characters (0x21-0x7E) minus the 5 that Go's json.Marshal escapes or
// expands (" & < > \). The 89 characters are split into 64 terminal
// symbols (which end a value, analogous to LEB128's continuation bit = 0)
// and 25 continuation symbols (which signal more digits follow, analogous
// to LEB128's continuation bit = 1). This yields a mixed-radix encoding:
// the final digit is base-64, all preceding digits are base-25.
//
// The result is a compact encoding where every output byte
// is a single safe printable ASCII character.
package leb89
