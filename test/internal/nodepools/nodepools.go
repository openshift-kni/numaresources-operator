package nodepools

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"

	"sigs.k8s.io/controller-runtime/pkg/client"

	hypershiftv1beta1 "github.com/openshift/hypershift/api/hypershift/v1beta1"

	"github.com/openshift-kni/numaresources-operator/test/internal/hypershift"
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

// AttachConfigObject attaches a tuning object into the nodepool associated with the hosted-cluster.
// Returns true if the object was actually added, false if it was already referenced.
func AttachConfigObject(ctx context.Context, cli client.Client, object client.Object) (bool, error) {
	hostedClusterName, err := hypershift.GetHostedClusterName()
	if err != nil {
		return false, err
	}
	var changed bool
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		np, err := GetByClusterName(ctx, cli, hostedClusterName)
		if err != nil {
			return err
		}
		np.Spec.Config, changed = addObjectRef(object, np.Spec.Config)
		if !changed {
			return nil
		}
		return cli.Update(ctx, np)
	})
	return changed, err
}

func addObjectRef(object client.Object, config []corev1.LocalObjectReference) ([]corev1.LocalObjectReference, bool) {
	updatedConfig := []corev1.LocalObjectReference{{Name: object.GetName()}}
	for i := range config {
		config := config[i]
		if config.Name != object.GetName() {
			updatedConfig = append(updatedConfig, config)
		}
	}
	return updatedConfig, len(updatedConfig) != len(config)
}

func removeObjectRef(object client.Object, config []corev1.LocalObjectReference) ([]corev1.LocalObjectReference, bool) {
	var updatedConfig []corev1.LocalObjectReference
	for i := range config {
		if config[i].Name != object.GetName() {
			updatedConfig = append(updatedConfig, config[i])
		}
	}
	return updatedConfig, len(updatedConfig) != len(config)
}

// DeAttachConfigObject removes a tuning object from the nodepool associated with the hosted-cluster.
// Returns true if the object was actually removed, false if it was not referenced.
func DeAttachConfigObject(ctx context.Context, cli client.Client, object client.Object) (bool, error) {
	hostedClusterName, err := hypershift.GetHostedClusterName()
	if err != nil {
		return false, err
	}
	var changed bool
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		np, err := GetByClusterName(ctx, cli, hostedClusterName)
		if err != nil {
			return err
		}
		np.Spec.Config, changed = removeObjectRef(object, np.Spec.Config)
		if !changed {
			return nil
		}
		return cli.Update(ctx, np)
	})
	return changed, err
}
