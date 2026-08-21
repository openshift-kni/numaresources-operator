package plugin

import "k8s.io/klog/v2"

const maximumExpectedNUMASpread = 1

func (p *Plugin) logUnexpectedNUMASpread(allocatedByNode map[int]map[string]struct{}) {
	counts := make(map[int]int)
	minAllocated, maxAllocated := -1, 0
	for _, numaID := range p.numaIDs() {
		count := len(allocatedByNode[numaID])
		counts[numaID] = count
		if minAllocated == -1 || count < minAllocated {
			minAllocated = count
		}
		if count > maxAllocated {
			maxAllocated = count
		}
	}
	spread := maxAllocated - minAllocated
	if spread > maximumExpectedNUMASpread {
		klog.ErrorS(nil, "numazone unexpected NUMA spread",
			"resourceName", p.options.ResourceName,
			"allocatedDevicesByNUMANode", counts,
			"minAllocated", minAllocated,
			"maxAllocated", maxAllocated,
			"spread", spread,
			"maxAllowedSpread", maximumExpectedNUMASpread)
	}
}
