// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Red Hat, Inc.

package numaplacement

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/k8stopologyawareschedwg/numaplacement/leb89"
)

const (
	// AttributeMetadata is the NRT top-level attribute declaring the version
	// of the container offsets and how the data is constructed
	AttributeMetadata = "topology.node.k8s.io/numaplacement-metadata"

	// AttributeVector is the NRT per-zone attribute carrying the
	// LEB89-encoded delta vector of container offsets placed on that NUMA.
	AttributeVector = "topology.node.k8s.io/numaplacement-vector"
)

const (
	// Prefix is the string common to all the fingerprints
	// A prefix is always 4 bytes long
	Prefix = "npv0"
	// Version is the version of this fingerprint. You should
	// only compare compatible versions.
	// A Version is always 4 bytes long, in the form v\X\X\X
	Version = "v001"
	// Unknown is the conventional unset/unknown value for int fields
	Unknown = -1
)

const (
	metadataSeparator = "::"
)

var (
	ErrMalformedMetadata              = errors.New("malformed metadata")
	ErrUnknownMetadata                = errors.New("unknown metadata field")
	ErrMalformedMetadataPair          = errors.New("malformed metadata pair")
	ErrMissingMetadataKey             = errors.New("missing metadata key")
	ErrMissingMetadataValue           = errors.New("missing metadata value")
	ErrInconsistentContainerSet       = errors.New("inconsistent container set")
	ErrInconsistentBusiestNode        = errors.New("inconsistent busiest node data")
	ErrUnknownBusiestNode             = errors.New("unknown busiest node")
	ErrInconsistentNUMANodes          = errors.New("inconsistent NUMA node count")
	ErrInconsistentNUMAVectors        = errors.New("inconsistent NUMA vector data")
	ErrCorruptedNUMAVector            = errors.New("corrupted NUMA vector")
	ErrDuplicatedNUMAVector           = errors.New("duplicated data in multiple NUMA vectors")
	ErrUnknownContainer               = errors.New("unknown container")
	ErrWrongNUMAAffinity              = errors.New("wrong NUMA affinity")
	ErrUnsupportedUnknownNUMAAffinity = errors.New("unknown NUMA affinity not supported")
	ErrUnsupportedVectorEncoding      = errors.New("unsupported vector encoding")
)

// Hasher implements the subset of stdlib methods this package needs.
type Hasher interface {
	Reset()
	WriteString(string) (int, error)
	Sum64() uint64
}

// ContainerID contains the identification attributes for a Container.
type ContainerID struct {
	Namespace     string
	PodName       string
	ContainerName string
}

// String returns the human-oriented string representation of a ContainerID
func (ci ContainerID) String() string {
	return ci.Namespace + "/" + ci.PodName + "/" + ci.ContainerName
}

// HashWith returns the canonical hash representation of this ContainerID,
// reusing a given hasher. We assume a xxhash implementation.
func (ci ContainerID) HashWith(hasher Hasher) uint64 {
	hasher.Reset()
	hasher.WriteString(ci.Namespace)
	hasher.WriteString("\x00")
	hasher.WriteString(ci.PodName)
	hasher.WriteString("\x00")
	hasher.WriteString(ci.ContainerName)
	return hasher.Sum64()
}

// Hash() returns the canonical xxhash representation of this ContainerID
func (ci ContainerID) Hash() uint64 {
	return ci.HashWith(xxhash.New())
}

// ContainerAffinity maps a container through its ContainerID to a NUMA Node.
type ContainerAffinity struct {
	ID       ContainerID
	NUMANode int
}

// VectorEncoding identifies the encoding used for per-NUMA vectors.
// Values are short strings (<=8 chars) allocated per-package.
const (
	VectorEncodingLEB89 string = "leb89"
)

// Payload is the encoder output which can be added to or derived from the NRT content
// Payload represents (a slightly abstracted) wire data because this package wants
// to avoid direct manipulation, therefore direct dependency, on NRT objects.
// Furthermore, abstracting the payload from the data format allows us to generalize
// easily the applicability of this package.
type Payload struct {
	// Number of containers this info represents
	Containers int `numaloc:"cc"`
	// Number of NUMA nodes
	NUMANodes int `numaloc:"nn"`
	// Index of busiest NUMA node, therefore omitted on wire
	BusiestNode int `numaloc:"bn"`
	// VectorEncoding reports the meaning of the Vector data
	VectorEncoding string `numaloc:"ve"`
	// map NUMANodeID -> vector placement
	Vectors map[int]string
}

// EmptyPayload constructs an empty valid Payload. Expected to be used mostly in tests.
func EmptyPayload(ve string, busiestNode int) Payload {
	return Payload{
		NUMANodes:      1,
		BusiestNode:    busiestNode,
		VectorEncoding: ve,
		Vectors:        make(map[int]string),
	}
}

// Validate checks the consistency of internal payload fields, returns nil
// if everything looks good, error otherwise detailing the failure.
// Validate will NOT check the semantic of the vectors: it will not validate
// or decode the leb89 encoding; errors in the encoded vector can be
// caught at decoding stage.
func (pl Payload) Validate(validateFunc func(Payload) error) error {
	if err := validateFunc(pl); err != nil {
		return err
	}
	if pl.Containers < 0 {
		return ErrInconsistentContainerSet
	}
	if pl.NUMANodes <= 0 {
		return ErrInconsistentNUMANodes
	}
	if pl.Containers == 0 && len(pl.Vectors) > 0 {
		return ErrInconsistentNUMAVectors
	}
	if len(pl.Vectors) > pl.NUMANodes {
		return ErrInconsistentNUMAVectors
	}
	if pl.BusiestNode >= pl.NUMANodes {
		return ErrInconsistentBusiestNode
	}
	for numaNode := range pl.Vectors {
		if numaNode < 0 || numaNode >= pl.NUMANodes {
			return ErrCorruptedNUMAVector
		}
	}
	return nil
}

type metaField struct {
	name   string
	ptrInt *int
	ptrStr *string
}

func setMetaField(fields []metaField, rawField string) error {
	idx := strings.Index(rawField, "=")
	if idx == -1 {
		return ErrMalformedMetadataPair
	}
	if idx == 0 {
		return ErrMissingMetadataKey
	}
	if idx >= len(rawField)-1 {
		return ErrMissingMetadataValue
	}
	fieldName := rawField[:idx]
	if fieldName == "ve" {
		fieldVal := rawField[idx+1:]
		for _, field := range fields {
			if field.name == fieldName {
				*field.ptrStr = fieldVal
			}
		}
		return nil
	}
	fieldVal, err := strconv.Atoi(rawField[idx+1:])
	if err != nil {
		return fmt.Errorf("error while parsing %q: %w", rawField, err)
	}
	for _, field := range fields {
		if field.name == fieldName {
			*field.ptrInt = fieldVal
			return nil
		}
	}
	return ErrUnknownMetadata
}

// UnpackMetadataInto updates the Payload metadata fields from the given encoded
// metadata string.
// On failure, the returned error is not nil and partial changes may have been
// done to the Payload argument.
func UnpackMetadataInto(pl *Payload, metadata string) error {
	fields := []metaField{
		{
			name:   "ve",
			ptrStr: &pl.VectorEncoding,
		},
		{
			name:   "cc",
			ptrInt: &pl.Containers,
		},
		{
			name:   "nn",
			ptrInt: &pl.NUMANodes,
		},
		{
			name:   "bn",
			ptrInt: &pl.BusiestNode,
		},
	}
	metaPfx := Prefix + Version + metadataSeparator
	if !strings.HasPrefix(metadata, metaPfx) {
		return ErrMalformedMetadata
	}
	for _, field := range fields {
		if field.ptrInt != nil {
			*field.ptrInt = Unknown
		}
		if field.ptrStr != nil {
			*field.ptrStr = ""
		}
	}
	for _, rawField := range strings.Split(strings.TrimPrefix(metadata, metaPfx), metadataSeparator) {
		if err := setMetaField(fields, rawField); err != nil {
			return fmt.Errorf("error while setting %q: %w", rawField, err)
		}
	}
	return nil
}

// PackMetadata encodes the Payload metadata fields into a string representation.
func (pl Payload) PackMetadata() string {
	vals := []string{
		fmt.Sprintf("ve=%s", pl.VectorEncoding),
		fmt.Sprintf("cc=%d", pl.Containers),
		fmt.Sprintf("nn=%d", pl.NUMANodes),
		fmt.Sprintf("bn=%d", pl.BusiestNode),
	}
	return Prefix + Version + metadataSeparator + strings.Join(vals, metadataSeparator)
}

// EncodePerNUMAVector delta-encodes a sorted slice of container offsets into
// a LEB89 string. The first offset is stored as-is; subsequent offsets are
// stored as deltas from the previous value.
//
// The input MUST be sorted in ascending order. When called through
// Encoder.Result(), this is guaranteed because offsets are indices into a
// sorted hash slice and are appended in iteration order. Unsorted input
// would produce negative deltas, which will panic in leb89.EncodeInto.
func EncodePerNUMAVector(sortedOffsets []int32) string {
	if len(sortedOffsets) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.Grow(len(sortedOffsets) + len(sortedOffsets)/8)
	leb89.EncodeInto(&sb, sortedOffsets[0])
	for i := 1; i < len(sortedOffsets); i++ {
		leb89.EncodeInto(&sb, sortedOffsets[i]-sortedOffsets[i-1])
	}
	return sb.String()
}

// DecodePerNUMAVector decodes a LEB89 string back into a sorted slice of
// container offsets (the inverse of EncodePerNUMAVector).
func DecodePerNUMAVector(s string) []int32 {
	if len(s) == 0 {
		return []int32{}
	}
	out := make([]int32, 0, len(s))
	pos := 0
	prev := int32(0)
	first := true
	for pos < len(s) {
		v, newPos := leb89.DecodeFromString(s, pos)
		if first {
			out = append(out, v)
			prev = v
			first = false
		} else {
			prev += v
			out = append(out, prev)
		}
		pos = newPos
	}
	return out
}

// Encoder handles a full set of containers by their name, and their affinity,
// and produces a Payload which compactly represents the affinities vectors.
// Containers can be added in a streaming manner, not necessarily in one go,
// but once the Payload is created, the Encoder instance must be discarded.
type Encoder struct {
	numaNodes    int
	numaLocality map[uint64]int // hash->numaID
	hasher       *xxhash.Digest
}

// NewEncoder creates a new Encoder object for the given NUMA Nodes count,
// optionally encoding the given ContainerAffinities. Even though it is
// supported, the NUMA Node count should usually never change in the
// lifetime of the program.
// If duplicate Affinities are given (same ID with different Affinity) the
// latest added wins and the previous instances are silently discarded.
func NewEncoder(numaNodes int, cas ...ContainerAffinity) (*Encoder, error) {
	if numaNodes <= 0 {
		return nil, ErrInconsistentNUMANodes
	}
	enc := &Encoder{
		numaNodes:    numaNodes,
		hasher:       xxhash.New(),
		numaLocality: make(map[uint64]int, len(cas)),
	}
	return enc.Encode(cas...)
}

// Containers return the number of containers this encoder knows about
func (enc *Encoder) Containers() int {
	return len(enc.numaLocality)
}

// NUMANodes return the number of NUMA nodes this encoder knows about
func (enc *Encoder) NUMANodes() int {
	return enc.numaNodes
}

// Encode encodes the given ContainerAffinities.
// If duplicate Affinities are given (same ID with different Affinity) the
// latest added wins and the previous instances are silently discarded.
func (enc *Encoder) Encode(cas ...ContainerAffinity) (*Encoder, error) {
	for _, ca := range cas {
		if ca.NUMANode == Unknown {
			return nil, ErrUnsupportedUnknownNUMAAffinity
		}
		if ca.NUMANode != Unknown && ca.NUMANode < 0 {
			return nil, ErrWrongNUMAAffinity
		}
		if ca.NUMANode > enc.numaNodes-1 {
			return nil, ErrWrongNUMAAffinity
		}
		hval := ca.ID.HashWith(enc.hasher)
		enc.numaLocality[hval] = ca.NUMANode
	}
	return enc, nil
}

// EncodeContainer encode a container affinity through its essential attributes.
// If duplicate Affinities are given (same ID with different Affinity) the
// latest added wins and the previous instances are silently discarded.
func (enc *Encoder) EncodeContainer(namespace, podName, containerName string, numaAffinity int) (*Encoder, error) {
	return enc.Encode(ContainerAffinity{
		ID: ContainerID{
			Namespace:     namespace,
			PodName:       podName,
			ContainerName: containerName,
		},
		NUMANode: numaAffinity,
	})
}

// Result finalizes the encoding of a set of ContainerAffinities.
// On failure, the Payload must be ignored and the error is not nil.
func (enc *Encoder) Result() (Payload, error) {
	pl := Payload{
		Containers:     len(enc.numaLocality),
		NUMANodes:      enc.numaNodes,
		BusiestNode:    Unknown,
		VectorEncoding: VectorEncodingLEB89,
		Vectors:        make(map[int]string),
	}
	if len(enc.numaLocality) == 0 {
		pl.BusiestNode = 0
		return pl, nil
	}
	hashes := SortedKeys(enc.numaLocality)
	perNuma := make(map[int][]int32, enc.numaNodes)
	for idx, hval := range hashes {
		numaNode := enc.numaLocality[hval]
		perNuma[numaNode] = append(perNuma[numaNode], int32(idx))
	}
	busiestNodeCount := 0
	numaNodes := SortedKeys(perNuma)
	for _, numaNode := range numaNodes {
		vec := perNuma[numaNode]
		if len(vec) > busiestNodeCount {
			busiestNodeCount = len(vec)
			pl.BusiestNode = numaNode
		}
	}
	for numaNode, vec := range perNuma {
		if numaNode == pl.BusiestNode {
			continue
		}
		// vec can't have zero length by construction
		pl.Vectors[numaNode] = EncodePerNUMAVector(vec)
	}
	return pl, nil
}

// Info represents compactly-stored NUMA locality information.
// This is the data the consumer side should store and keep up to date.
type Info interface {
	Containers() int
	NUMAAffinity(id ContainerID) (int, error)
	NUMAAffinityContainer(namespace, podName, containerName string) (int, error)
}

type EncodedInfo struct {
	numaLocality map[uint64]int // hash->numaID
}

// NewInfo create a new empty Info struct.
// Info is *not* safe to be called concurrently except for the
// NUMAAffinity/NUMAAffinityContainer query functions.
// The caller must handle its own locking.
func NewEncodedInfo() *EncodedInfo {
	return &EncodedInfo{
		numaLocality: make(map[uint64]int),
	}
}

// Containers returns the count of the containers we know about.
func (info *EncodedInfo) Containers() int {
	return len(info.numaLocality)
}

// Equal returns true if this info has equal value to the given one, false otherwise.
func (info *EncodedInfo) Equal(obj *EncodedInfo) bool {
	return reflect.DeepEqual(info.numaLocality, obj.numaLocality)
}

// Update clones the numalocality semantics from the given `data`.
func (info *EncodedInfo) Update(data *EncodedInfo) {
	info.numaLocality = maps.Clone(data.numaLocality)
}

// Take moves the numalocality semantics from the given `data`.
func (info *EncodedInfo) Take(data *EncodedInfo) {
	info.numaLocality = data.numaLocality
	data.numaLocality = nil
}

// NUMAAffinity returns the NUMA Node mapping of a given ContainerID.
// On failure, the affinity is not relevant and the error is not nil
func (info *EncodedInfo) NUMAAffinity(id ContainerID) (int, error) {
	numaNode, ok := info.numaLocality[id.Hash()]
	if !ok {
		return -1, ErrUnknownContainer
	}
	return numaNode, nil
}

// NUMAAffinityContainer returns the NUMA Node mapping of a given Container by its attributes.
// On failure, the affinity is not relevant and the error is not nil
func (info *EncodedInfo) NUMAAffinityContainer(namespace, podName, containerName string) (int, error) {
	return info.NUMAAffinity(ContainerID{Namespace: namespace, PodName: podName, ContainerName: containerName})
}

// Decoder takes a Payload (once, fixed) and needs the full set of expected ContainerIDs
// which must be verified to be consistent with the encoding side at time of the Payload was generated
// And produces a Info object which the client code can use to learn the NUMA affinity of a container.
// Likewise the Encoder, containers can be added in a streaming manner, not necessarily in one go,
// but once the Info is created from the Payload, the Decoder instance must be discarded.
type Decoder struct {
	payload   Payload
	hashesSet map[uint64]struct{}
	hasher    *xxhash.Digest
}

// decoderValidateLEB89 validates the Payload fields under LEB89 encoding constraints.
func decoderValidateLEB89(pl Payload) error {
	if pl.VectorEncoding != VectorEncodingLEB89 {
		return ErrUnsupportedVectorEncoding
	}
	if pl.BusiestNode == Unknown {
		return ErrUnknownBusiestNode
	}
	if pl.BusiestNode < 0 || pl.BusiestNode >= pl.NUMANodes {
		return ErrInconsistentBusiestNode
	}
	// LEB89 encoding omits the busiest node (N-1 optimization),
	// so the vector count must be strictly less than NUMANodes
	// and no vector may exist for the busiest node.
	if len(pl.Vectors) >= pl.NUMANodes {
		return ErrInconsistentNUMAVectors
	}
	if _, ok := pl.Vectors[pl.BusiestNode]; ok {
		return ErrCorruptedNUMAVector
	}

	return nil
}

// NewDecoder creates a new Decoder for the given Payload object, optionally prefeeding with
// the given ContainerIDs.
// If duplicate ContainerIDs are given the latest added wins and the previous IDs are silently discarded.
func NewDecoder(pl Payload, ids ...ContainerID) (*Decoder, error) {
	if err := pl.Validate(decoderValidateLEB89); err != nil {
		return nil, err
	}
	dec := &Decoder{
		payload:   pl,
		hashesSet: make(map[uint64]struct{}),
		hasher:    xxhash.New(),
	}
	return dec.Decode(ids...), nil
}

// Containers return the number of containers this decoder knows about.
// If duplicate ContainerIDs are given the latest added wins and the previous IDs are silently discarded.
func (dec *Decoder) Containers() int {
	return len(dec.hashesSet)
}

// Decode adds the given set of ContainerIDs to the decoder.
func (dec *Decoder) Decode(ids ...ContainerID) *Decoder {
	for _, id := range ids {
		dec.hashesSet[id.HashWith(dec.hasher)] = struct{}{}
	}
	return dec
}

// DecodeContainer adds the given container through its essential attributes.
// If duplicate ContainerIDs are given the latest added wins and the previous IDs are silently discarded.
func (dec *Decoder) DecodeContainer(namespace, podName, containerName string) *Decoder {
	return dec.Decode(ContainerID{
		Namespace:     namespace,
		PodName:       podName,
		ContainerName: containerName,
	})
}

// Result finalizes a Decoder and returns Info object the client code can consume to query the
// affinity of the given ContainerID set from the Payload. On failure, error is not nil and
// the Info must be ignored (it is usually `nil`, but is not guaranteed to be `nil`).
func (dec *Decoder) Result() (Info, error) {
	if len(dec.hashesSet) != dec.payload.Containers {
		return nil, ErrInconsistentContainerSet
	}
	// BusiestNode range, vector keys, and structural invariants
	// are already validated by Payload.Validate() in NewDecoder.
	hashes := SortedKeys(dec.hashesSet)
	info := NewEncodedInfo()
	for numaNode, vec := range dec.payload.Vectors {
		offsets := DecodePerNUMAVector(vec)
		for _, off := range offsets {
			// Reject malformed/truncated vectors that decode to negative offsets.
			// A negative offset would panic when indexing hashes[off].
			if int(off) < 0 || int(off) >= len(hashes) {
				return nil, ErrCorruptedNUMAVector
			}
			if _, ok := info.numaLocality[hashes[off]]; ok {
				return nil, ErrDuplicatedNUMAVector
			}
			info.numaLocality[hashes[off]] = numaNode
		}
	}
	for _, hval := range hashes {
		if _, ok := info.numaLocality[hval]; !ok {
			info.numaLocality[hval] = dec.payload.BusiestNode
		}
	}
	return info, nil
}

// SortedKeys returns the keys of a map as a sorted slice.
// Once on Go 1.23+, replace every sortedKeys(m) call with
// slices.Sorted(maps.Keys(m)), then delete both sortedKeys and mapKeys.
func SortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := mapKeys(m)
	slices.Sort(keys)
	return keys
}

// mapKeys returns the keys of a map as a slice.
// Once on Go 1.23+, replace with maps.Keys.
func mapKeys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
