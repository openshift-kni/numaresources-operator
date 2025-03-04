package nodepools

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/numaresources-operator/test/internal/hypershift"
	hypershiftv1beta1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
)

func GetByClusterName(ctx context.Context, c client.Client, hostedClusterName string) (*hypershiftv1beta1.NodePool, error) {
	npList := &hypershiftv1beta1.NodePoolList{}
	if err := c.List(ctx, npList); err != nil {
		return nil, err
	}
	var np *hypershiftv1beta1.NodePool
	for i := 0; i < len(npList.Items); i++ {
		if npList.Items[i].Spec.ClusterName == hostedClusterName {
			np = &npList.Items[i]
			break
		}
	}
	if np == nil {
		return nil, fmt.Errorf("failed to find nodePool associated with cluster %q; existing nodePools are: %+v", hostedClusterName, npList.Items)
	}
	return np, nil
}

// AttachConfigObject is attaches a tuning object into the nodepool associated with the hosted-cluster
// The function is idempotent
func AttachConfigObject(ctx context.Context, cli client.Client, object client.Object) error {
	hostedClusterName, err := hypershift.GetHostedClusterName()
	if err != nil {
		return err
	}
	np, err := GetByClusterName(ctx, cli, hostedClusterName)
	if err != nil {
		return err
	}
	np.Spec.Config = addObjectRef(object, np.Spec.Config)
	if cli.Update(ctx, np) != nil {
		return err
	}
	return nil
}

func addObjectRef(object client.Object, config []corev1.LocalObjectReference) []corev1.LocalObjectReference {
	updatedConfig := []corev1.LocalObjectReference{{Name: object.GetName()}}
	for i := range config {
		config := config[i]
		if config.Name != object.GetName() {
			updatedConfig = append(updatedConfig, config)
		}
	}
	return updatedConfig
}

func removeObjectRef(object client.Object, config []corev1.LocalObjectReference) []corev1.LocalObjectReference {
	var updatedConfig []corev1.LocalObjectReference
	for i := range config {
		if config[i].Name != object.GetName() {
			updatedConfig = append(updatedConfig, config[i])
		}
	}
	return updatedConfig
}

func DeAttachConfigObject(ctx context.Context, cli client.Client, object client.Object) error {
	hostedClusterName, err := hypershift.GetHostedClusterName()
	if err != nil {
		return err
	}
	np, err := GetByClusterName(ctx, cli, hostedClusterName)
	if err != nil {
		return err
	}
	np.Spec.Config = removeObjectRef(object, np.Spec.Config)
	if cli.Update(ctx, np) != nil {
		return err
	}
	return nil
}
