package deploy

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"

	inthelper "github.com/openshift-kni/numaresources-operator/internal/api/annotations/helper"
	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/internal/wait"

	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2epause "github.com/openshift-kni/numaresources-operator/test/utils/objects/pause"
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
		Expect(e2eclient.Client.Create(ctx, kcObj)).To(Succeed())
		o.KcObj = kcObj
		createKubelet = true
	}

	By(fmt.Sprintf("creating the NRO object: %s", nroObj.Name))
	Expect(e2eclient.Client.Create(ctx, nroObj)).To(Succeed())
	o.NroObj = nroObj

	By("unpausing the target MCPs")
	Eventually(unpause).WithTimeout(timeout).WithPolling(configuration.MachineConfigPoolUpdateInterval).Should(Succeed())

	By("updating the target NRO object")
	Expect(e2eclient.Client.Get(ctx, client.ObjectKeyFromObject(nroObj), nroObj)).To(Succeed())
	o.NroObj = nroObj

	if createKubelet || inthelper.IsCustomPolicyEnabled(nroObj) {
		By("waiting for MCP to get updated")
		Expect(WaitForMCPsCondition(e2eclient.Client, ctx, mcps, machineconfigv1.MachineConfigPoolUpdated)).To(Succeed())
	}
	return nroObj
}

func (o *OpenShiftNRO) Teardown(ctx context.Context, timeout time.Duration) {
	GinkgoHelper()

	var wg sync.WaitGroup
	if o.McpObj != nil {
		err := e2eclient.Client.Delete(ctx, o.McpObj)
		Expect(err).ToNot(HaveOccurred())

		wg.Add(1)
		go func(mcpObj *machineconfigv1.MachineConfigPool) {
			defer GinkgoRecover()
			defer wg.Done()
			klog.Infof("waiting for MCP %q to be gone", mcpObj.Name)
			err := wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(timeout).ForMachineConfigPoolDeleted(ctx, mcpObj)
			Expect(err).ToNot(HaveOccurred(), "MCP %q failed to be deleted", mcpObj.Name)
		}(o.McpObj)
	}

	var err error
	if o.KcObj != nil {
		err = e2eclient.Client.Delete(ctx, o.KcObj)
		Expect(err).ToNot(HaveOccurred())
		wg.Add(1)
		go func(kcObj *machineconfigv1.KubeletConfig) {
			defer GinkgoRecover()
			defer wg.Done()
			klog.Infof("waiting for KC %q to be gone", kcObj.Name)
			err := wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(timeout).ForKubeletConfigDeleted(ctx, kcObj)
			Expect(err).ToNot(HaveOccurred(), "KC %q failed to be deleted", kcObj.Name)
		}(o.KcObj)
	}

	err = e2eclient.Client.Delete(ctx, o.NroObj)
	Expect(err).ToNot(HaveOccurred())
	wg.Add(1)
	go func(nropObj *nropv1.NUMAResourcesOperator) {
		defer GinkgoRecover()
		defer wg.Done()
		klog.Infof("waiting for NROP %q to be gone", nropObj.Name)
		err := wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(timeout).ForNUMAResourcesOperatorDeleted(ctx, nropObj)
		Expect(err).ToNot(HaveOccurred(), "NROP %q failed to be deleted", nropObj.Name)
	}(o.NroObj)

	wg.Wait()

	WaitForMCPUpdatedAfterNRODeleted(o.NroObj)
}
