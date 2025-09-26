/*
 * Copyright 2025 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package record

import (
	"errors"
	"time"

	"github.com/k8stopologyawareschedwg/podfingerprint"
)

var (
	ErrInvalidCapacity  = errors.New("invalid capacity")
	ErrInvalidNodeCount = errors.New("invalid node count")
	ErrMissingNode      = errors.New("missing node")
	ErrMismatchingNode  = errors.New("mismatching node")
	ErrTooManyNodes     = errors.New("excessive node count")
)

// RecordedStatus is a Status plus additional metadata
type RecordedStatus struct {
	podfingerprint.Status
	// RecordTime is a timestamp of when the RecordedStatus was added to the record
	RecordTime time.Time `json:"recordTime"`
}

func (rs RecordedStatus) Equal(x RecordedStatus) bool {
	return rs.Status.Equal(x.Status)
}

// Timestampr is the function called to get the timestamps. A good default is time.Now
type Timestamper func() time.Time

// NodeRecorder stores all the recorded statuses for a given node name.
// Statuses belonging to different nodes won't be accepted.
type NodeRecorder struct {
	timestamper func() time.Time
	nodeName    string // shortcut
	capacity    int
	statuses    []RecordedStatus
}

type NodeOption func(*NodeRecorder)

func WithCapacity(capacity int) NodeOption {
	return func(nr *NodeRecorder) {
		nr.capacity = capacity
	}
}

// NewNodeRecorder creates a new recorder for the given node with the given capacity.
// The record is a ring buffer, so only the latest <capacity> Statuses are kept at any time.
// The timestamper callback is used to mark times. Use `time.Now` if unsure.
// Returns the newly created instance; if parameters are incorrect, returns an error, on which
// case the returned instance should be ignored.
func NewNodeRecorder(nodeName string, timestamper Timestamper, opts ...NodeOption) (*NodeRecorder, error) {
	if nodeName == "" {
		return nil, ErrMissingNode
	}
	nr := NodeRecorder{
		timestamper: timestamper,
		nodeName:    nodeName,
	}
	for _, opt := range opts {
		opt(&nr)
	}
	if nr.capacity < 1 {
		return nil, ErrInvalidCapacity
	}
	if nr.capacity == 1 { // handle common special case
		nr.statuses = make([]RecordedStatus, 1)
	}
	return &nr, nil
}

func (nr *NodeRecorder) dropOldest() {
	if nr.Len() < 1 {
		return
	}
	nr.statuses = nr.statuses[1:]
}

func (nr *NodeRecorder) makeRoom() {
	if nr.Len() < nr.Cap() {
		return
	}
	nr.dropOldest()
}

// Push adds a new Status to the record, evicting the oldest Status if necessary.
// The pushed status is a full independent copy of the provided Status.
// If the Status added is inconsistent, returns an error detailing the reason.
// Statuses are evicted only in case of success.
func (nr *NodeRecorder) Push(st podfingerprint.Status) error {
	if st.NodeName == "" {
		return ErrMissingNode
	}
	if st.NodeName != nr.nodeName {
		return ErrMismatchingNode
	}
	ts := nr.timestamper()
	item := RecordedStatus{
		Status:     st.Clone(),
		RecordTime: ts,
	}
	if nr.capacity == 1 { // handle common special case, avoid any resize
		nr.statuses[0] = item
		return nil
	}
	nr.makeRoom()
	nr.statuses = append(nr.statuses, item)
	return nil
}

// Len returns how many Statuses are currently held in the NodeRecorder
func (nr *NodeRecorder) Len() int {
	return len(nr.statuses)
}

// Cap returns the maximum capacity of the NodeRecorder
func (nr *NodeRecorder) Cap() int {
	return nr.capacity
}

// Content() returns a shallow copy of all the recorded statuses.
func (nr *NodeRecorder) Content() []RecordedStatus {
	return nr.statuses
}

// Recorder stores all the recorded statuses, dividing them by node name.
// There is a hard cap of how many nodes are managed, and how many Statuses are recorded per node.
type Recorder struct {
	nodes        map[string]*NodeRecorder
	nodeCapacity int
	maxNodes     int
	timestamper  Timestamper
}

// NewRecorder creates a new recorder up to the given node count, each with the given capacity.
// Each per-node recorder is a ring buffer, so only the latest <nodeCapacity> Statuses are kept
// at any time for each node. The per-node records are created lazily as needed.
// The timestamper callback is used to mark times. Use `time.Now` if unsure.
// Returns the newly created instance; if parameters are incorrect, returns an error, on which
// case the returned instance should be ignored.
func NewRecorder(maxNodes, nodeCapacity int, timestamper Timestamper) (*Recorder, error) {
	if maxNodes < 1 {
		return nil, ErrInvalidNodeCount
	}
	if nodeCapacity < 1 {
		return nil, ErrInvalidCapacity
	}
	return &Recorder{
		nodes:        make(map[string]*NodeRecorder),
		nodeCapacity: nodeCapacity,
		maxNodes:     maxNodes,
		timestamper:  timestamper,
	}, nil
}

// Cap returns the maximum nodes allowed in this Recorder
func (rr *Recorder) MaxNodes() int {
	return rr.maxNodes
}

// Cap returns the maximum capacity of each NodeRecorder
func (rr *Recorder) Cap() int {
	return rr.nodeCapacity
}

// CountNodes returns how many Nodes are known to the Recorder
func (rr *Recorder) CountNodes() int {
	return len(rr.nodes)
}

// CountRecords returns how many Records are held for the give nodeName in the Recorder
func (rr *Recorder) CountRecords(nodeName string) int {
	nr, ok := rr.nodes[nodeName]
	if !ok {
		return 0
	}
	return nr.Len()
}

// Len returns the total number of records across all nodes
func (rr *Recorder) Len() int {
	tot := 0
	for _, nr := range rr.nodes {
		tot += nr.Len()
	}
	return tot
}

// Push adds a new Status to the record for its node, evicting the oldest Status
// belonging to the same node if necessary.
// Per-node records are created lazily as needed, up to the configured maximum.
// The pushed status is a full independent copy of the provided Status.
// If the Status added is inconsistent, returns an error detailing the reason.
// Statuses are evicted only in case of success.
func (rr *Recorder) Push(st podfingerprint.Status) error {
	if st.NodeName == "" {
		return ErrMissingNode
	}

	var err error
	nr, ok := rr.nodes[st.NodeName]

	if !ok && rr.maxCapacityReached() {
		return ErrTooManyNodes
	}

	if !ok {
		nr, err = NewNodeRecorder(st.NodeName, rr.timestamper, WithCapacity(rr.nodeCapacity))
		if err != nil {
			return err
		}
		rr.nodes[st.NodeName] = nr
	}
	return nr.Push(st)
}

// Content() returns a shallow copy of all the recorded statuses, by node name.
func (rr *Recorder) Content() map[string][]RecordedStatus {
	ret := make(map[string][]RecordedStatus, len(rr.nodes))
	for nodeName, nr := range rr.nodes {
		ret[nodeName] = nr.Content()
	}
	return ret
}

// ContentForNode returns a shallow copy of all the recorded status for the given nodeName.
// Returns the content and a boolean which tells if the node is known or not
func (rr *Recorder) ContentForNode(nodeName string) ([]RecordedStatus, bool) {
	nr, ok := rr.nodes[nodeName]
	if !ok {
		return []RecordedStatus{}, false
	}
	return nr.Content(), true
}

func (rr *Recorder) maxCapacityReached() bool {
	return len(rr.nodes) >= rr.maxNodes
}
