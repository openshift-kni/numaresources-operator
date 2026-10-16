package deploy

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	inthelper "github.com/openshift-kni/numaresources-operator/internal/api/annotations/helper"
	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
	e2epause "github.com/openshift-kni/numaresources-operator/test/internal/objects/pause"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ Deployer = &OpenShiftNRO{}

type OpenShiftNRO struct {
	McpObj *machineconfigv1.MachineConfigPool
	KcObj  *machineconfigv1.KubeletConfig
	NroObj *nropv1.NUMAResourcesOperator
}

// Deploy deploys NUMAResourcesOperator object and
// other dependencies, so the controller will be able to install TAS
// stack properly
func (o *OpenShiftNRO) Deploy(ctx context.Context, timeout time.Duration) *nropv1.NUMAResourcesOperator {
	GinkgoHelper()

	return o.deployWithLabels(ctx, timeout, objects.OpenshiftMatchLabels())
}

func (o *OpenShiftNRO) deployWithLabels(ctx context.Context, timeout time.Duration, matchLabels map[string]string) *nropv1.NUMAResourcesOperator {
	GinkgoHelper()
	nroObj := objects.TestNRO(objects.NROWithMCPSelector(matchLabels))
	kcObj, err := objects.TestKC(matchLabels)
	Expect(err).To(Not(HaveOccurred()))

	By("getting reference values for target MCPs")
	// need to get reference values before any mutations
	mcps, err := nropmcp.GetListByNodeGroupsV1(ctx, e2eclient.Client, nroObj.Spec.NodeGroups)
	Expect(err).NotTo(HaveOccurred())
	klog.Infof("Found %d target MCPs", len(mcps))

	By("pausing the target MCPs")
	unpause, err := e2epause.MachineConfigPoolsByNodeGroups(nroObj.Spec.NodeGroups)
	Expect(err).NotTo(HaveOccurred())

	var createKubelet bool
	if _, ok := os.LookupEnv("E2E_NROP_INSTALL_SKIP_KC"); ok {
		By("using cluster kubeletconfig (if any)")
	} else {
		By(fmt.Sprintf("creating the KC object: %s", kcObj.Name))
		Expect(createOrUpdate(ctx, e2eclient.Client, kcObj)).To(Succeed())
		o.KcObj = kcObj
		createKubelet = true
	}

	By(fmt.Sprintf("creating the NRO object: %s", nroObj.Name))
	Expect(createOrUpdate(ctx, e2eclient.Client, nroObj)).To(Succeed())
	o.NroObj = nroObj

	By("unpausing the target MCPs")
	Eventually(unpause).WithTimeout(timeout).WithPolling(configuration.MachineConfigPoolUpdateInterval).Should(Succeed())

	By("updating the target NRO object")
	Expect(e2eclient.Client.Get(ctx, client.ObjectKeyFromObject(nroObj), nroObj)).To(Succeed())
	o.NroObj = nroObj

	if createKubelet || inthelper.IsCustomPolicyEnabled(nroObj) {
		By("waiting for MCP to get updated")
		Expect(WaitForMCPsCondition(e2eclient.Client, ctx, machineconfigv1.MachineConfigPoolUpdated, mcps...)).To(Succeed())
	}
	return nroObj
}

func (o *OpenShiftNRO) Teardown(ctx context.Context, timeout time.Duration) {
	GinkgoHelper()

	var wg sync.WaitGroup
	if o.McpObj != nil {
		deleted, err := deleteIfNeeded(ctx, e2eclient.Client, o.McpObj)
		Expect(err).ToNot(HaveOccurred())
		if deleted {
			wg.Add(1)
			go func(mcpObj *machineconfigv1.MachineConfigPool) {
				defer GinkgoRecover()
				defer wg.Done()
				klog.Infof("waiting for MCP %q to be gone", mcpObj.Name)
				err := wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(timeout).ForMachineConfigPoolDeleted(ctx, mcpObj)
				Expect(err).ToNot(HaveOccurred(), "MCP %q failed to be deleted", mcpObj.Name)
			}(o.McpObj)
		}
	}

	var unpause func() error
	if o.NroObj != nil && configuration.Plat == platform.OpenShift {
		By("pausing target MCPs to batch teardown rollout")
		var err error
		unpause, err = e2epause.MachineConfigPoolsByNodeGroups(o.NroObj.Spec.NodeGroups)
		Expect(err).ToNot(HaveOccurred(), "failed to pause target MCPs")
	}
	unpaused := false
	defer func() {
		if unpause != nil && !unpaused {
			By("unpausing target MCPs (deferred)")
			Eventually(unpause).WithTimeout(timeout).WithPolling(configuration.MachineConfigPoolUpdateInterval).Should(Succeed())
		}
	}()

	if o.KcObj != nil {
		deleted, err := deleteIfNeeded(ctx, e2eclient.Client, o.KcObj)
		Expect(err).ToNot(HaveOccurred())
		if deleted {
			wg.Add(1)
			go func(kcObj *machineconfigv1.KubeletConfig) {
				defer GinkgoRecover()
				defer wg.Done()
				klog.Infof("waiting for KC %q to be gone", kcObj.Name)
				err := wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(timeout).ForKubeletConfigDeleted(ctx, kcObj)
				Expect(err).ToNot(HaveOccurred(), "KC %q failed to be deleted", kcObj.Name)
			}(o.KcObj)
		}
	}

	if o.NroObj != nil {
		deleted, err := deleteIfNeeded(ctx, e2eclient.Client, o.NroObj)
		Expect(err).ToNot(HaveOccurred())
		if deleted {
			wg.Add(1)
			go func(nropObj *nropv1.NUMAResourcesOperator) {
				defer GinkgoRecover()
				defer wg.Done()
				klog.Infof("waiting for NROP %q to be gone", nropObj.Name)
				err := wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(timeout).ForNUMAResourcesOperatorDeleted(ctx, nropObj)
				Expect(err).ToNot(HaveOccurred(), "NROP %q failed to be deleted", nropObj.Name)
			}(o.NroObj)
		}
	}

	wg.Wait()

	if unpause != nil {
		By("unpausing target MCPs")
		Eventually(unpause).WithTimeout(timeout).WithPolling(configuration.MachineConfigPoolUpdateInterval).Should(Succeed())
		unpaused = true
	}

	if o.NroObj != nil {
		WaitForMCPUpdatedAfterNRODeleted(o.NroObj)
	}
}
