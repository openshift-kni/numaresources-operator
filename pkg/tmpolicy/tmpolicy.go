package tmpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

const (
	PolicyEnv = "TOPOLOGY_MANAGER_POLICY"
	ScopeEnv  = "TOPOLOGY_MANAGER_SCOPE"
)

func AutoDetectPolicy(ctx context.Context, cli client.Client, mcpLabels map[string]string) (string, error) {
	kcs := &mcov1.KubeletConfigList{}
	if err := cli.List(ctx, kcs); err != nil {
		return "", err
	}

	kc, err := getKubeletConfigByMCP(kcs, mcpLabels)
	if err != nil {
		return "", err
	}
	if kc == nil {
		return "", fmt.Errorf("no matching kubeletconfig found")
	}

	kubeconfig, err := mcoKubeletConfToKubeletConf(kc)
	if err != nil {
		return "", err
	}

	return kubeconfig.TopologyManagerPolicy, nil
}

// AutoDetectScope will try to detect the topology manager scope configured for the nodes with the given MCP selector
func AutoDetectScope(ctx context.Context, cli client.Client, mcpLabels map[string]string) (string, error) {
	kcs := &mcov1.KubeletConfigList{}
	if err := cli.List(ctx, kcs); err != nil {
		return "", err
	}

	kc, err := getKubeletConfigByMCP(kcs, mcpLabels)
	if err != nil {
		return "", err
	}
	if kc == nil {
		return "", fmt.Errorf("no matching kubeletconfig found")
	}

	kubeconfig, err := mcoKubeletConfToKubeletConf(kc)
	if err != nil {
		return "", err
	}

	return kubeconfig.TopologyManagerScope, nil
}

func mcoKubeletConfToKubeletConf(mcoKc *mcov1.KubeletConfig) (*kubeletconfigv1beta1.KubeletConfiguration, error) {
	kc := &kubeletconfigv1beta1.KubeletConfiguration{}
	err := json.Unmarshal(mcoKc.Spec.KubeletConfig.Raw, kc)
	return kc, err
}

func getKubeletConfigByMCP(kcs *mcov1.KubeletConfigList, mcpLabels map[string]string) (*mcov1.KubeletConfig, error) {
	var selectedKc *mcov1.KubeletConfig
	for _, kc := range kcs.Items {
		if kc.Spec.MachineConfigPoolSelector == nil {
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(kc.Spec.MachineConfigPoolSelector)
		if err != nil {
			return selectedKc, err
		}

		if selector.Matches(labels.Set(mcpLabels)) {
			selectedKc = &kc
			break
		}
	}
	return selectedKc, nil
}
