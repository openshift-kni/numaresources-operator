package upgrade

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	"github.com/openshift-kni/numaresources-operator/test/utils/version"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

var _ = Describe("Upgrade", Label("upgrade"), func() {
	var initialized bool

	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")
		}
		initialized = true
		operatorVersion, err := version.OfOperator(context.TODO(), e2eclient.Client)
		Expect(err).NotTo(HaveOccurred())
		if operatorVersion.String() < "4.18" {
			Skip("Upgrade suite is only supported on operator versions 4.18 or newer")
		}
	})

	Context("after operator upgrade", func() {
		It("should remove machineconfigs when no SElinux policy annotation is present", func() {
			updatedNROObj := &nropv1.NUMAResourcesOperator{}

			err := e2eclient.Client.Get(context.TODO(), objects.NROObjectKey(), updatedNROObj)
			Expect(err).NotTo(HaveOccurred())

			if !annotations.IsCustomPolicyEnabled(updatedNROObj.Annotations) {
				mcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), e2eclient.Client, updatedNROObj.Spec.NodeGroups)
				Expect(err).NotTo(HaveOccurred())

				for _, mcp := range mcps {
					mc := &machineconfigv1.MachineConfig{}
					// Check mc not created
					mcKey := client.ObjectKey{
						Name: objectnames.GetMachineConfigName(updatedNROObj.Name, mcp.Name),
					}

					err := e2eclient.Client.Get(context.TODO(), mcKey, mc)
					Expect(err).ToNot(BeNil(), "MachineConfig %s is not expected to to be present", mcKey.String())
					Expect(errors.IsNotFound(err)).To(BeTrue(), "Unexpected error occurred while getting MachineConfig %s: %v", mcKey.String(), err)
				}
			}
		})
	})
})
