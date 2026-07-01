package numalocality

import (
	"k8s.io/apimachinery/pkg/util/sets"
	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"
)

// GetNUMAIDs returns the NUMAs affinity for the given topology. The API allows for a resource to be
// pinned to multiple NUMA nodes depending on the topology manager policy, and the function uses int set
// to avoid duplicates. It returns a slice of NUMA IDs or an empty slice if the topology is not pinned to
// any NUMA node.
func GetNUMAIDs(topo *podresourcesapi.TopologyInfo) []int {
	if topo == nil || topo.Nodes == nil {
		return []int{}
	}

	numaIDs := sets.New[int]()
	// if Nodes is not given, this means "don't care about locality". It's a legal representation
	for _, node := range topo.Nodes {
		// setting node.ID == -1 is also a legal representation for "don't care about locality".
		if node.ID >= 0 {
			numaIDs.Insert(int(node.ID))
		}
	}
	return numaIDs.UnsortedList()
}
