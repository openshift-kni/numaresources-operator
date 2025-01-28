package hypershift

import (
	"fmt"
	"os"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
)

var isHypershiftCluster bool

func init() {
	if configuration.Plat == platform.HyperShift {
		klog.Infof("hypershift cluster detected")
		isHypershiftCluster = true
	}
}

const (
	ManagementClusterKubeConfigEnv = "HYPERSHIFT_MANAGEMENT_CLUSTER_KUBECONFIG"
	HostedControlPlaneNamespaceEnv = "HYPERSHIFT_HOSTED_CONTROL_PLANE_NAMESPACE"
	HostedClusterNameEnv           = "CLUSTER_NAME"
)

func BuildControlPlaneClient() (client.Client, error) {
	kcPath, ok := os.LookupEnv(ManagementClusterKubeConfigEnv)
	if !ok {
		return nil, fmt.Errorf("failed to build management-cluster client for hypershift, environment variable %q is not defined", ManagementClusterKubeConfigEnv)
	}
	c, err := buildClient(kcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build management-cluster client for hypershift; err %v", err)
	}
	return c, nil
}

func GetHostedClusterName() (string, error) {
	v, ok := os.LookupEnv(HostedClusterNameEnv)
	if !ok {
		return "", fmt.Errorf("failed to retrieve hosted cluster name; %q environment var is not set", HostedClusterNameEnv)
	}
	return v, nil
}

func GetManagementClusterNamespace() (string, error) {
	ns, ok := os.LookupEnv(HostedControlPlaneNamespaceEnv)
	if !ok {
		return "", fmt.Errorf("failed to retrieve management cluster namespace; %q environment var is not set", HostedControlPlaneNamespaceEnv)
	}
	return ns, nil
}

// IsHypershiftCluster should be used only on CI environment
func IsHypershiftCluster() bool {
	return isHypershiftCluster
}

func buildClient(kubeConfigPath string) (client.Client, error) {
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		return nil, err
	}
	c, err := client.New(restConfig, client.Options{})
	if err != nil {
		return nil, err
	}
	return c, nil
}
