// SPDX-License-Identifier: Apache-2.0

package leb89

import "strings"

const (
	Unmapped int32 = -1
)

const (
	AlphabetSize    = 89
	NumTerminal     = 64
	NumContinuation = 25 // 89 - 64
)

// safeASCII holds the 89 printable ASCII characters that Go's json.Marshal
// passes through without escaping: all of 0x21-0x7E except " & < > \
var safeASCII [89]byte

// asciiIndex is the reverse map: byte -> index in safeASCII, -1 if unmapped.
var asciiIndex [128]int

func init() {
	idx := 0
	for b := byte(0x21); b <= 0x7E; b++ {
		if b == '"' || b == '&' || b == '<' || b == '>' || b == '\\' {
			continue
		}
		safeASCII[idx] = b
		idx++
	}
	for i := range asciiIndex {
		asciiIndex[i] = int(Unmapped)
	}
	for i, b := range safeASCII {
		asciiIndex[b] = i
	}
}

// EncodeInto encodes v >= 0 as a LEB89 character sequence written to w.
// Values 0-63: 1 character (terminal).
// Values 64-1663: 2 characters (1 continuation + terminal).
// Values 1664-40063: 3 characters (2 continuations + terminal).
//
// v must be non-negative. Negative values will cause an array index
// out of bounds panic. In the normal encoder flow, v is always a
// non-negative offset or a delta between sorted ascending offsets,
// so negative values cannot occur.
func EncodeInto(w *strings.Builder, v int32) {
	if v < int32(NumTerminal) {
		w.WriteByte(safeASCII[v])
		return
	}
	v -= int32(NumTerminal)

	terminal := v % int32(NumTerminal)
	v /= int32(NumTerminal)

	var digits [8]int32
	n := 0
	for {
		digits[n] = v % int32(NumContinuation)
		n++
		v /= int32(NumContinuation)
		if v == 0 {
			break
		}
	}
	for i := n - 1; i >= 0; i-- {
		w.WriteByte(safeASCII[NumTerminal+digits[i]])
	}
	w.WriteByte(safeASCII[terminal])
}

// EncodeIntoString encodes v >= 0 as a LEB89 string.
// Convenience wrapper around EncodeInto for single-value encoding.
func EncodeIntoString(v int32) string {
	var w strings.Builder
	EncodeInto(&w, v)
	return w.String()
}

// DecodeFromString reads one LEB89-encoded value from s[pos:].
// Returns (value, newPos). Returns (Unmapped, pos) on malformed input.
func DecodeFromString(s string, pos int) (int32, int) {
	var contValue int32
	nCont := 0
	for pos < len(s) {
		idx := asciiIndex[s[pos]]
		pos++
		if idx < NumTerminal {
			if nCont == 0 {
				return int32(idx), pos
			}
			return int32(NumTerminal) + contValue*int32(NumTerminal) + int32(idx), pos
		}
		contValue = contValue*int32(NumContinuation) + int32(idx-NumTerminal)
		nCont++
	}
	return Unmapped, pos
}
