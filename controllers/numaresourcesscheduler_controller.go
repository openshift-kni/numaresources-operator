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

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/pkg/errors"
)

// NUMAResourcesSchedulerReconciler reconciles a NUMAResourcesScheduler object
type NUMAResourcesSchedulerReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	SchedManifests schedmanifests.Manifests
	Helper         *deployer.Helper
	Namespace      string
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=*
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=*
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=*
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

	instance := &nropv1alpha1.NUMAResourcesScheduler{}
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

	result, condition, err := r.reconcileResource(ctx, instance)
	if condition != "" {
		// TODO: use proper reason
		reason, message := condition, messageFromError(err)
		if err := r.updateStatus(ctx, instance, condition, reason, message); err != nil {
			klog.InfoS("Failed to update numaresourcesoperator status", "Desired condition", condition, "error", err)
		}
	}
	return result, err
}

func (r *NUMAResourcesSchedulerReconciler) reconcileResource(ctx context.Context, instance *nropv1alpha1.NUMAResourcesScheduler) (ctrl.Result, string, error) {
	err := r.syncNUMAResourcesSchedulerResources(ctx, instance)
	if err != nil {
		return ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "FailedSchedSync")
	}
	return ctrl.Result{}, status.ConditionAvailable, nil
}

func (r *NUMAResourcesSchedulerReconciler) syncNUMAResourcesSchedulerResources(ctx context.Context, instance *nropv1alpha1.NUMAResourcesScheduler) error {
	klog.Info("SchedSync start")

	schedstate.UpdateDeploymentImage(r.SchedManifests.Deployment, instance.Spec.SchedulerImage)
	schedstate.UpdateDeploymentArgs(r.SchedManifests.Deployment)
	schedstate.UpdateDeploymentConfigMap(r.SchedManifests.Deployment, r.SchedManifests.ConfigMap.Name)

	existing := schedstate.FromClient(ctx, r.Client, r.SchedManifests)
	for _, objState := range existing.State(r.SchedManifests) {
		if err := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme); err != nil {
			return errors.Wrapf(err, "Failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
		_, err := apply.ApplyObject(ctx, r.Client, objState)
		if err != nil {
			return errors.Wrapf(err, "could not apply (%s) %s/%s", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
	}
	return nil
}

func (r *NUMAResourcesSchedulerReconciler) updateStatus(ctx context.Context, scd *nropv1alpha1.NUMAResourcesScheduler, condition string, reason string, message string) error {
	conditions := status.NewConditions(condition, reason, message)
	if equality.Semantic.DeepEqual(conditions, scd.Status.Conditions) {
		return nil
	}
	scd.Status.Conditions = status.NewConditions(condition, reason, message)

	if err := r.Client.Status().Update(ctx, scd); err != nil {
		return errors.Wrapf(err, "could not update status for object %s", k8sclient.ObjectKeyFromObject(scd))
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NUMAResourcesSchedulerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// note no Owns/Watches. This was intentionally omitted because
	// this NUMAResourcesScheduler support is temporary until SSOP takes over,
	// which is expected to happen "soon". So it is not worthy the additional
	// refactoring needed to make Own work as intended.
	// We may want to re-evaluate this decision in the future depending on the
	// SSOP timeline.
	return ctrl.NewControllerManagedBy(mgr).
		For(&nropv1alpha1.NUMAResourcesScheduler{}).
		Complete(r)
}
