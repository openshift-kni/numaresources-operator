package deploy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/hypershift"
	"github.com/openshift-kni/numaresources-operator/test/internal/nodepools"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ Deployer = &HyperShiftNRO{}

type HyperShiftNRO struct {
	NroObj         *nropv1.NUMAResourcesOperator
	KcConfigMapObj *corev1.ConfigMap
}

const (
	HostedClustersNamespaceName = "clusters"
	ConfigDataKey               = "config"
)

func (h *HyperShiftNRO) Deploy(ctx context.Context, _ time.Duration) *nropv1.NUMAResourcesOperator {
	GinkgoHelper()

	hostedClusterName, err := hypershift.GetHostedClusterName()
	Expect(err).To(Not(HaveOccurred()))
	np, err := nodepools.GetByClusterName(ctx, e2eclient.MNGClient, hostedClusterName)
	Expect(err).To(Not(HaveOccurred()))

	if _, ok := os.LookupEnv("E2E_NROP_INSTALL_SKIP_KC"); ok {
		By("using cluster kubeletconfig configmap (if any)")
	} else {
		By("creating KubeletConfig ConfigMap on the management cluster")
		kcObj, err := objects.TestKC(objects.EmptyMatchLabels())
		Expect(err).To(Not(HaveOccurred()))
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: kcObj.GetName(), Namespace: HostedClustersNamespaceName}}
		b, err := encodeManifest(kcObj, e2eclient.Client.Scheme())
		Expect(err).To(Not(HaveOccurred()))
		cm.Data = map[string]string{ConfigDataKey: string(b)}
		Expect(createOrUpdate(ctx, e2eclient.MNGClient, cm)).To(Succeed())
		h.KcConfigMapObj = cm

		By(fmt.Sprintf("attaching KubeletConfig ConfigMap to nodepool %s", np.Name))
		attached, err := nodepools.AttachConfigObject(ctx, e2eclient.MNGClient, cm)
		Expect(err).ToNot(HaveOccurred())

		if attached {
			By(fmt.Sprintf("waiting for nodepool %s transition to updating config", np.Name))
			Expect(wait.ForUpdatingConfig(ctx, e2eclient.MNGClient, np.Name, np.Namespace)).To(Succeed())
			By(fmt.Sprintf("waiting for nodepool %s transition to config ready", np.Name))
			Expect(wait.ForConfigToBeReady(ctx, e2eclient.MNGClient, np.Name, np.Namespace)).To(Succeed())
		}
	}

	nroObj := objects.TestNRO()
	nroObj.Spec.NodeGroups = append(nroObj.Spec.NodeGroups, nropv1.NodeGroup{PoolName: &np.Name})
	By(fmt.Sprintf("creating the NRO object: %s", nroObj.Name))
	Expect(createOrUpdate(ctx, e2eclient.Client, nroObj)).To(Succeed())
	h.NroObj = nroObj
	return h.NroObj
}

func (h *HyperShiftNRO) Teardown(ctx context.Context, timeout time.Duration) {
	GinkgoHelper()
	h.teardownKcConfigMap(ctx, timeout)
	if h.NroObj != nil {
		_, err := deleteIfNeeded(ctx, e2eclient.Client, h.NroObj)
		Expect(err).ToNot(HaveOccurred())
	}
}

func (h *HyperShiftNRO) teardownKcConfigMap(ctx context.Context, timeout time.Duration) {
	GinkgoHelper()
	if h.KcConfigMapObj == nil {
		klog.Infof("KubeletConfig ConfigMap does not exist, skipping teardown of KubeletConfig ConfigMap")
		return
	}

	hostedClusterName, err := hypershift.GetHostedClusterName()
	Expect(err).To(Not(HaveOccurred()))
	np, err := nodepools.GetByClusterName(ctx, e2eclient.MNGClient, hostedClusterName)
	Expect(err).To(Not(HaveOccurred()))

	e2efixture.By("deataching KubeletConfig ConfigMap from nodepool %s", np.Name)
	detached, err := nodepools.DeAttachConfigObject(ctx, e2eclient.MNGClient, h.KcConfigMapObj)
	Expect(err).ToNot(HaveOccurred())

	if detached {
		e2efixture.By("waiting for nodepool %s transition to updating config", np.Name)
		Expect(wait.ForUpdatingConfig(ctx, e2eclient.MNGClient, np.Name, np.Namespace)).To(Succeed())
		e2efixture.By("waiting for nodepool %s transition to config ready", np.Name)
		Expect(wait.ForConfigToBeReady(ctx, e2eclient.MNGClient, np.Name, np.Namespace)).To(Succeed())
	}
	_, err = deleteIfNeeded(ctx, e2eclient.MNGClient, h.KcConfigMapObj)
	Expect(err).ToNot(HaveOccurred())

	if h.NroObj == nil || errors.IsNotFound(e2eclient.Client.Get(ctx, client.ObjectKeyFromObject(h.NroObj), h.NroObj)) {
		klog.Infof("NRO object does not exist, skipping deletion of generated configmap")
		return
	}

	By("checking that generated configmap has been deleted")
	Eventually(func() bool {
		Expect(e2eclient.Client.Get(ctx, client.ObjectKeyFromObject(h.NroObj), h.NroObj)).To(Succeed())
		nodeGroups := h.NroObj.Status.NodeGroups
		return len(nodeGroups) != 0 && nodeGroups[0].DaemonSet.Name != ""
	}).WithTimeout(time.Minute*3).WithPolling(10*time.Second).Should(BeTrue(), "NRO object does not have any DaemonSets status reported")

	generatedCM := &corev1.ConfigMap{}
	key := client.ObjectKey{
		Name:      objectnames.GetComponentName(h.NroObj.Name, np.Name),
		Namespace: h.NroObj.Status.NodeGroups[0].DaemonSet.Namespace,
	}
	Eventually(func() bool {
		if err := e2eclient.Client.Get(ctx, key, generatedCM); !errors.IsNotFound(err) {
			if err == nil {
				klog.Warningf("configmap %s still exists", key.String())
			} else {
				klog.Warningf("configmap %s return with error: %v", key.String(), err)
			}
			return false
		}
		return true
	}).WithTimeout(timeout).WithPolling(10 * time.Second).Should(BeTrue())
}

func encodeManifest(obj runtime.Object, scheme *runtime.Scheme) ([]byte, error) {
	yamlSerializer := serializer.NewSerializerWithOptions(
		serializer.DefaultMetaFactory, scheme, scheme,
		serializer.SerializerOptions{Yaml: true, Pretty: true, Strict: true},
	)
	buff := bytes.Buffer{}
	err := yamlSerializer.Encode(obj, &buff)
	return buff.Bytes(), err
}
