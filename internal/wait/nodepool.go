package wait

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"sigs.k8s.io/controller-runtime/pkg/client"

	hypershiftv1beta1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
)

func ForUpdatingConfig(ctx context.Context, c client.Client, npName, namespace string) error {
	return waitForCondition(ctx, c, npName, namespace, func(conds []hypershiftv1beta1.NodePoolCondition) bool {
		for _, cond := range conds {
			if cond.Type == hypershiftv1beta1.NodePoolUpdatingConfigConditionType {
				return cond.Status == corev1.ConditionTrue
			}
		}
		return false
	})
}

func ForConfigToBeReady(ctx context.Context, c client.Client, npName, namespace string) error {
	return waitForCondition(ctx, c, npName, namespace, func(conds []hypershiftv1beta1.NodePoolCondition) bool {
		for _, cond := range conds {
			if cond.Type == hypershiftv1beta1.NodePoolUpdatingConfigConditionType {
				return cond.Status == corev1.ConditionFalse
			}
		}
		return false
	})
}

func waitForCondition(ctx context.Context, c client.Client, npName, namespace string, conditionFunc func([]hypershiftv1beta1.NodePoolCondition) bool) error {
	return wait.PollUntilContextTimeout(ctx, time.Second*10, time.Minute*60, false, func(ctx context.Context) (done bool, err error) {
		np := &hypershiftv1beta1.NodePool{}
		key := client.ObjectKey{Name: npName, Namespace: namespace}
		err = c.Get(ctx, key, np)
		if err != nil {
			return false, err
		}
		return conditionFunc(np.Status.Conditions), nil
	})
}
