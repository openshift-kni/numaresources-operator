package deploy

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

var _ Deployer = &KubernetesNRO{}

type KubernetesNRO struct {
	OpenShiftNRO
}

// Deploy returns a struct containing all the deployed objects,
// so it will be easier to introspect and delete them later.
func (k *KubernetesNRO) Deploy(ctx context.Context) *v1.NUMAResourcesOperator {
	GinkgoHelper()

	mcpObj := objects.TestMCP()
	By(fmt.Sprintf("creating the machine config pool object: %s", mcpObj.Name))
	err := e2eclient.Client.Create(ctx, mcpObj)
	Expect(err).NotTo(HaveOccurred())
	k.McpObj = mcpObj
	matchLabels := map[string]string{"test": "test"}

	return k.OpenShiftNRO.deployWithLabels(ctx, matchLabels)
}

func (k *KubernetesNRO) Teardown(ctx context.Context, timeout time.Duration) {
	k.OpenShiftNRO.Teardown(ctx, timeout)
}
