package iter

import (
	"iter"

	v1 "k8s.io/api/core/v1"
)

// ContainerType signifies container type
type ContainerType int

const (
	// Containers is for normal containers
	Containers ContainerType = 1 << iota
	// InitContainers is for init containers
	InitContainers
	// EphemeralContainers is for ephemeral containers
	EphemeralContainers
)

func (ct ContainerType) String() string {
	switch ct {
	case Containers:
		return "app"
	case InitContainers:
		return "init"
	case EphemeralContainers:
		return "eph"
	default:
		return "UNK"
	}
}

// OnContainers is a clone of k8s.io/kubernetes/pkg/api/v1/pod.ContainerIter 1.37.0
// ContainerIter returns an iterator over all containers in the given pod spec with a masked type.
// The iteration order is InitContainers, then main Containers, then EphemeralContainers.
func OnContainers(podSpec *v1.PodSpec, mask ContainerType) iter.Seq2[*v1.Container, ContainerType] {
	return func(yield func(*v1.Container, ContainerType) bool) {
		if mask&InitContainers != 0 {
			for i := range podSpec.InitContainers {
				if !yield(&podSpec.InitContainers[i], InitContainers) {
					return
				}
			}
		}
		if mask&Containers != 0 {
			for i := range podSpec.Containers {
				if !yield(&podSpec.Containers[i], Containers) {
					return
				}
			}
		}
		if mask&EphemeralContainers != 0 {
			for i := range podSpec.EphemeralContainers {
				if !yield((*v1.Container)(&podSpec.EphemeralContainers[i].EphemeralContainerCommon), EphemeralContainers) {
					return
				}
			}
		}
	}
}
