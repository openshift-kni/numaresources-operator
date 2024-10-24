package api

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
)

func AsObjectKey(nn nropv1.NamespacedName) client.ObjectKey {
	return client.ObjectKey{
		Namespace: nn.Namespace,
		Name:      nn.Name,
	}
}

func NamespacedNameFromObject(obj metav1.Object) nropv1.NamespacedName {
	return nropv1.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}
