package version

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/lib/version"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
)

var defaultNamespace = "numaresources-operator"

func OfOperator(ctx context.Context, cli client.Client) (*version.OperatorVersion, error) {
	csvList := &operatorsv1alpha1.ClusterServiceVersionList{}

	err := cli.List(context.TODO(), csvList, &client.ListOptions{
		Namespace: defaultNamespace,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to fetch ClusterServiceVersion")
	}
	if len(csvList.Items) != 1 {
		return nil, fmt.Errorf("expect only one CSV object under %s namespace", defaultNamespace)
	}
	return &csvList.Items[0].Spec.Version, nil
}
