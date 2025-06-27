package deploy

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/openshift-kni/numaresources-operator/api/v1"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ Deployer = &KubernetesNRO{}

type KubernetesNRO struct {
	OpenShiftNRO
}

// Deploy returns a struct containing all the deployed objects,
// so it will be easier to introspect and delete them later.
func (k *KubernetesNRO) Deploy(ctx context.Context, timeout time.Duration) *v1.NUMAResourcesOperator {
	GinkgoHelper()

	mcpObj := objects.TestMCP()
	By(fmt.Sprintf("creating the machine config pool object: %s", mcpObj.Name))
	err := e2eclient.Client.Create(ctx, mcpObj)
	Expect(err).NotTo(HaveOccurred())
	k.McpObj = mcpObj
	matchLabels := map[string]string{"test": "test"}

	return k.OpenShiftNRO.deployWithLabels(ctx, timeout, matchLabels)
}

func (k *KubernetesNRO) Teardown(ctx context.Context, timeout time.Duration) {
	k.OpenShiftNRO.Teardown(ctx, timeout)
}
