/*
Copyright 2021.

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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pkg/errors"

	nrsv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

const (
	defaultNUMAResourcesSchedulerCrName                      = "numaresourcesscheduler"
	conditionTypeIncorrectNUMAResourcesSchedulerResourceName = "IncorrectNUMAResourcesSchedulerResourceName"
)

// NUMAResourcesSchedulerReconciler reconciles a NUMAResourcesScheduler object
type NUMAResourcesSchedulerReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	SchedulerManifests schedmanifests.Manifests
	Namespace          string
}

//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=*
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=*
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=*
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=*
//+kubebuilder:rbac:groups=nodetopology.openshift.io,resources=numaresourcesschedulers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nodetopology.openshift.io,resources=numaresourcesschedulers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nodetopology.openshift.io,resources=numaresourcesschedulers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NUMAResourcesScheduler object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *NUMAResourcesSchedulerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.V(3).InfoS("Starting NUMAResourcesScheduler reconcile loop", "object", req.NamespacedName)
	defer klog.V(3).InfoS("Finish NUMAResourcesScheduler reconcile loop", "object", req.NamespacedName)

	instance := &nrsv1alpha1.NUMAResourcesScheduler{}
	err := r.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	if req.Name != defaultNUMAResourcesSchedulerCrName {
		message := fmt.Sprintf("incorrect NUMAResourcesScheduler resource name: %s", instance.Name)
		return ctrl.Result{}, r.updateStatus(ctx, instance, status.ConditionDegraded, conditionTypeIncorrectNUMAResourcesSchedulerResourceName, message)
	}

	result, condition, err := r.reconcileResource(ctx, instance)
	if condition != "" {
		// TODO: use proper reason
		reason, message := condition, messageFromError(err)
		if err := r.updateStatus(ctx, instance, condition, reason, message); err != nil {
			klog.InfoS("Failed to update numaresourcesscheduler status", "Desired condition", condition, "error", err)
		}
	}
	return result, err

}

// SetupWithManager sets up the controller with the Manager.
func (r *NUMAResourcesSchedulerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nrsv1alpha1.NUMAResourcesScheduler{}).
		Complete(r)
}

func (r *NUMAResourcesSchedulerReconciler) reconcileResource(ctx context.Context, instance *nrsv1alpha1.NUMAResourcesScheduler) (reconcile.Result, string, error) {
	klog.Info("SchedulerSync start")

	deploymentInfo, schedulerName, err := r.syncNUMASchedulerResources(ctx, instance)
	if err != nil {
		return ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "FailedSchedulerSync")
	}

	instance.Status.Deployment = nrsv1alpha1.NamespacedName{}
	ok, err := isDeploymentRunning(ctx, r.Client, deploymentInfo)
	if err != nil {
		return ctrl.Result{}, status.ConditionDegraded, err
	}
	if !ok {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, status.ConditionProgressing, nil
	}

	instance.Status.Deployment = deploymentInfo
	instance.Status.SchedulerName = schedulerName

	return ctrl.Result{}, status.ConditionAvailable, nil

}

func isDeploymentRunning(ctx context.Context, c client.Client, key nrsv1alpha1.NamespacedName) (bool, error) {
	dp := &appsv1.Deployment{}
	if err := c.Get(ctx, client.ObjectKey(key), dp); err != nil {
		return false, err
	}

	for _, cond := range dp.Status.Conditions {
		if cond.Type == appsv1.DeploymentAvailable {
			return cond.Status == corev1.ConditionTrue, nil
		}
	}
	return false, nil
}

func (r *NUMAResourcesSchedulerReconciler) syncNUMASchedulerResources(ctx context.Context, instance *nrsv1alpha1.NUMAResourcesScheduler) (nrsv1alpha1.NamespacedName, string, error) {
	var deploymentNName nrsv1alpha1.NamespacedName
	schedulerName := instance.Spec.SchedulerName

	schedstate.UpdateDeploymentImageSettings(r.SchedulerManifests.Deployment, instance.Spec.SchedulerImage)
	schedstate.UpdateDeploymentConfigMapSettings(r.SchedulerManifests.Deployment, r.SchedulerManifests.ConfigMap.Name)
	if schedulerName != "" {
		err := schedstate.UpdateSchedulerName(r.SchedulerManifests.ConfigMap, instance.Spec.SchedulerName)
		if err != nil {
			return nrsv1alpha1.NamespacedName{}, schedulerName, err
		}
	}
	if err := loglevel.UpdatePodSpec(&r.SchedulerManifests.Deployment.Spec.Template.Spec, instance.Spec.LogLevel); err != nil {
		return deploymentNName, schedulerName, err
	}

	existing := schedstate.FromClient(ctx, r.Client, r.SchedulerManifests)
	for _, objState := range existing.State(r.SchedulerManifests) {
		if err := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme); err != nil {
			return deploymentNName, schedulerName, errors.Wrapf(err, "Failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
		obj, err := apply.ApplyObject(ctx, r.Client, objState)
		if err != nil {
			return deploymentNName, schedulerName, errors.Wrapf(err, "could not apply (%s) %s/%s", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName())
		}

		if nname, ok := schedstate.DeploymentNamespacedNameFromObject(obj); ok {
			deploymentNName = nname
		}
		if schedName, ok := schedstate.SchedulerNameFromObject(obj); ok {
			schedulerName = schedName
		}
	}
	return deploymentNName, schedulerName, nil
}

func (r *NUMAResourcesSchedulerReconciler) updateStatus(ctx context.Context, sched *nrsv1alpha1.NUMAResourcesScheduler, condition string, reason string, message string) error {
	conditions := status.NewConditions(condition, reason, message)
	if apiequality.Semantic.DeepEqual(conditions, sched.Status.Conditions) {
		return nil
	}
	sched.Status.Conditions = status.NewConditions(condition, reason, message)

	if err := r.Client.Status().Update(ctx, sched); err != nil {
		return errors.Wrapf(err, "could not update status for object %s", client.ObjectKeyFromObject(sched))
	}
	return nil
}
