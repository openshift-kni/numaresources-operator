package deploy

import (
	corev1 "k8s.io/api/core/v1"
	"time"

	. "github.com/onsi/ginkgo/v2"
	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
)

var _ Deployer = &HyperShiftNRO{}

type HyperShiftNRO struct {
	NroObj         *nropv1.NUMAResourcesOperator
	KcConfigMapObj *corev1.ConfigMap
}

func (h HyperShiftNRO) Deploy() *nropv1.NUMAResourcesOperator {
	GinkgoHelper()
	By("deploying NRO for HyperShift platform not supported just yet")
	return h.NroObj
}

func (h HyperShiftNRO) Teardown(timeout time.Duration) {
	GinkgoHelper()
	By("Teardown NRO for HyperShift not supported just yet")
}
