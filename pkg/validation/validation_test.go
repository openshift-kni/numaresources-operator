package validation

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"

	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
)

var _ = Describe("Validation", func() {
	Describe("MachineConfigPoolDuplicates", func() {
		Context("with duplicates", func() {
			It("should return an error", func() {
				mcps := []*machineconfigv1.MachineConfigPool{
					testobjs.NewMachineConfigPool("test", nil, nil, nil),
					testobjs.NewMachineConfigPool("test", nil, nil, nil),
				}

				err := MachineConfigPoolDuplicates(mcps)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("selected by at least two node groups"))
			})
		})

		Context("without duplicates", func() {
			It("should not return any error", func() {
				mcps := []*machineconfigv1.MachineConfigPool{
					testobjs.NewMachineConfigPool("test", nil, nil, nil),
					testobjs.NewMachineConfigPool("test1", nil, nil, nil),
				}

				err := MachineConfigPoolDuplicates(mcps)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Describe("NodeGroups", func() {
		Context("with nil machineConfigPoolSelector", func() {
			It("should return an error", func() {
				nodeGroups := []nropv1alpha1.NodeGroup{
					{
						MachineConfigPoolSelector: nil,
					},
					{
						MachineConfigPoolSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test": "test",
							},
						},
					},
				}

				err := NodeGroups(nodeGroups)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("one of the node groups does not have machineConfigPoolSelector"))
			})
		})

		Context("with duplicates", func() {
			It("should return an error", func() {
				nodeGroups := []nropv1alpha1.NodeGroup{
					{
						MachineConfigPoolSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test": "test",
							},
						},
					},
					{
						MachineConfigPoolSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test": "test",
							},
						},
					},
				}

				err := NodeGroups(nodeGroups)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("has duplicates"))
			})
		})

		Context("with bad machine config pool selector", func() {
			It("should return an error", func() {
				nodeGroups := []nropv1alpha1.NodeGroup{
					{
						MachineConfigPoolSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "test",
									Operator: "bad-operator",
									Values:   []string{"test"},
								},
							},
						},
					},
				}

				err := NodeGroups(nodeGroups)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not a valid pod selector operator"))
			})
		})

		Context("with correct values", func() {
			It("should not return any error", func() {
				nodeGroups := []nropv1alpha1.NodeGroup{
					{
						MachineConfigPoolSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test": "test",
							},
						},
					},
					{
						MachineConfigPoolSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test1": "test1",
							},
						},
					},
				}

				err := NodeGroups(nodeGroups)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})
})
