package testutils

import (
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nrov1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
)

func NewNUMAResourcesOperator(name string, labelSelectors []*metav1.LabelSelector) *nrov1alpha1.NUMAResourcesOperator {
	var nodeGroups []nrov1alpha1.NodeGroup
	for _, selector := range labelSelectors {
		nodeGroups = append(nodeGroups, nrov1alpha1.NodeGroup{
			MachineConfigPoolSelector: selector,
		})
	}

	return &nrov1alpha1.NUMAResourcesOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NUMAResourcesOperator",
			APIVersion: nrov1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: nrov1alpha1.NUMAResourcesOperatorSpec{
			NodeGroups: nodeGroups,
		},
	}
}

func NewMachineConfigPool(name string, labels map[string]string, machineConfigSelector *metav1.LabelSelector, nodeSelector *metav1.LabelSelector) *machineconfigv1.MachineConfigPool {
	return &machineconfigv1.MachineConfigPool{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MachineConfigPool",
			APIVersion: machineconfigv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: machineconfigv1.MachineConfigPoolSpec{
			MachineConfigSelector: machineConfigSelector,
			NodeSelector:          nodeSelector,
		},
	}
}
