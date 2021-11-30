/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

// KubeletConfigReconciler reconciles a KubeletConfig object
type KubeletConfigReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	Namespace string
}

//+kubebuilder:rbac:groups="",resources=configmaps,verbs=*
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=machineconfiguration.openshift.io,resources=kubeletconfigs,verbs=get;list;watch

func (r *KubeletConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.V(3).InfoS("Starting KubeletConfig reconcile loop", "object", req.NamespacedName)
	defer klog.V(3).InfoS("Finish KubeletConfig reconcile loop", "object", req.NamespacedName)

	nname := types.NamespacedName{
		Name: defaultNUMAResourcesOperatorCrName,
	}
	instance := &nropv1alpha1.NUMAResourcesOperator{}
	err := r.Get(ctx, nname, instance)
	if err != nil {
		// TODO: review
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// KubeletConfig changes are expected to be sporadic, yet are important enough
	// to be made visible at kubernetes level. So we generate events to handle them

	helper := configMapHelper{
		cli:       r.Client,
		scm:       r.Scheme,
		namespace: r.Namespace,
		instance:  instance,
	}
	cm, err := helper.reconcileConfigMap(ctx, req.NamespacedName)
	if err != nil {
		klog.ErrorS(err, "failed to reconcile configmap", "controller", "kubeletconfig")

		msg := fmt.Sprintf("Failed to update RTE config from kubelet config %s/%s", req.NamespacedName.Namespace, req.NamespacedName.Name)
		r.Recorder.Event(instance, "Warning", "ProcessFailed", msg)
		return ctrl.Result{RequeueAfter: RetryPeriod}, err
	}

	msg := fmt.Sprintf("Updated RTE config %s/%s from kubelet config %s/%s", cm.Namespace, cm.Name, req.NamespacedName.Namespace, req.NamespacedName.Name)
	r.Recorder.Event(instance, "Normal", "ProcessOK", msg)
	return ctrl.Result{}, nil
}

func (r *KubeletConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&mcov1.KubeletConfig{})
	return b.Owns(&corev1.ConfigMap{}).
		Complete(r)
}
