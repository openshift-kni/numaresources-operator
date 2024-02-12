/*
 * Copyright 2021 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controllers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pkg/errors"

	k8swgmanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/internal/relatedobjects"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	schedupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

const (
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

	instance := &nropv1.NUMAResourcesScheduler{}
	err := r.Get(ctx, req.NamespacedName, instance)
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

	if req.Name != objectnames.DefaultNUMAResourcesSchedulerCrName {
		message := fmt.Sprintf("incorrect NUMAResourcesScheduler resource name: %s", instance.Name)
		return ctrl.Result{}, r.updateStatus(ctx, instance, status.ConditionDegraded, conditionTypeIncorrectNUMAResourcesSchedulerResourceName, message)
	}

	result, condition, err := r.reconcileResource(ctx, instance)

	reason := condition // TODO: use proper reason
	if err := r.updateStatus(ctx, instance, condition, reason, messageFromError(err)); err != nil {
		klog.InfoS("Failed to update numaresourcesscheduler status", "Desired condition", condition, "error", err)
	}

	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *NUMAResourcesSchedulerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// we want to initiate reconcile loop only on changes under labels, annotations or spec of the object
	p := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration() ||
				!apiequality.Semantic.DeepEqual(e.ObjectNew.GetLabels(), e.ObjectOld.GetLabels()) ||
				!apiequality.Semantic.DeepEqual(e.ObjectNew.GetAnnotations(), e.ObjectOld.GetAnnotations())
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&nropv1.NUMAResourcesScheduler{}).
		Owns(&rbacv1.ClusterRole{}, builder.WithPredicates(p)).
		Owns(&rbacv1.ClusterRoleBinding{}, builder.WithPredicates(p)).
		Owns(&corev1.ServiceAccount{}, builder.WithPredicates(p)).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(p)).
		Owns(&appsv1.Deployment{}, builder.WithPredicates(p)).
		Complete(r)
}

func (r *NUMAResourcesSchedulerReconciler) reconcileResource(ctx context.Context, instance *nropv1.NUMAResourcesScheduler) (reconcile.Result, string, error) {
	schedStatus, err := r.syncNUMASchedulerResources(ctx, instance)
	if err != nil {
		return ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "FailedSchedulerSync")
	}

	instance.Status = schedStatus
	instance.Status.RelatedObjects = relatedobjects.Scheduler(r.Namespace, instance.Status.Deployment)

	ok, err := isDeploymentRunning(ctx, r.Client, schedStatus.Deployment)
	if err != nil {
		return ctrl.Result{}, status.ConditionDegraded, err
	}
	if !ok {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, status.ConditionProgressing, nil
	}

	return ctrl.Result{}, status.ConditionAvailable, nil
}

func isDeploymentRunning(ctx context.Context, c client.Client, key nropv1.NamespacedName) (bool, error) {
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

func (r *NUMAResourcesSchedulerReconciler) syncNUMASchedulerResources(ctx context.Context, instance *nropv1.NUMAResourcesScheduler) (nropv1.NUMAResourcesSchedulerStatus, error) {
	klog.V(4).Info("SchedulerSync start")
	defer klog.V(4).Info("SchedulerSync stop")

	schedSpec := instance.Spec.Normalize()
	cacheResyncPeriod := unpackAPIResyncPeriod(schedSpec.CacheResyncPeriod)
	params := configParamsFromSchedSpec(schedSpec, cacheResyncPeriod)

	schedName, ok := schedstate.SchedulerNameFromObject(r.SchedulerManifests.ConfigMap)
	if !ok {
		err := fmt.Errorf("missing scheduler name in builtin config map")
		klog.V(2).ErrorS(err, "cannot find the scheduler profile name")
		return nropv1.NUMAResourcesSchedulerStatus{}, err
	}
	klog.V(4).InfoS("detected scheduler profile", "profileName", schedName)

	if err := schedupdate.SchedulerConfig(r.SchedulerManifests.ConfigMap, schedName, &params); err != nil {
		return nropv1.NUMAResourcesSchedulerStatus{}, err
	}

	schedStatus := nropv1.NUMAResourcesSchedulerStatus{
		SchedulerName: schedSpec.SchedulerName,
		CacheResyncPeriod: &metav1.Duration{
			Duration: cacheResyncPeriod,
		},
	}

	cmHash := hash.ConfigMapData(r.SchedulerManifests.ConfigMap)
	schedupdate.DeploymentImageSettings(r.SchedulerManifests.Deployment, schedSpec.SchedulerImage)
	schedupdate.DeploymentConfigMapSettings(r.SchedulerManifests.Deployment, r.SchedulerManifests.ConfigMap.Name, cmHash)
	if err := loglevel.UpdatePodSpec(&r.SchedulerManifests.Deployment.Spec.Template.Spec, schedSpec.LogLevel); err != nil {
		return schedStatus, err
	}

	schedupdate.DeploymentEnvVarSettings(r.SchedulerManifests.Deployment, schedSpec)

	existing := schedstate.FromClient(ctx, r.Client, r.SchedulerManifests)
	for _, objState := range existing.State(r.SchedulerManifests) {
		if err := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme); err != nil {
			return schedStatus, errors.Wrapf(err, "Failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
		obj, err := apply.ApplyObject(ctx, r.Client, objState)
		if err != nil {
			return schedStatus, errors.Wrapf(err, "could not apply (%s) %s/%s", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName())
		}

		if nname, ok := schedstate.DeploymentNamespacedNameFromObject(obj); ok {
			schedStatus.Deployment = nname
		}
		if schedName, ok := schedstate.SchedulerNameFromObject(obj); ok {
			schedStatus.SchedulerName = schedName
		}
	}
	return schedStatus, nil
}

func (r *NUMAResourcesSchedulerReconciler) updateStatus(ctx context.Context, sched *nropv1.NUMAResourcesScheduler, condition string, reason string, message string) error {
	sched.Status.Conditions, _ = status.GetUpdatedConditions(sched.Status.Conditions, condition, reason, message)
	if err := r.Client.Status().Update(ctx, sched); err != nil {
		return errors.Wrapf(err, "could not update status for object %s", client.ObjectKeyFromObject(sched))
	}
	return nil
}

func unpackAPIResyncPeriod(reconcilePeriod *metav1.Duration) time.Duration {
	period := reconcilePeriod.Round(time.Second)
	klog.InfoS("setting reconcile period", "computed", period, "supplied", reconcilePeriod)
	return period
}

func configParamsFromSchedSpec(schedSpec nropv1.NUMAResourcesSchedulerSpec, cacheResyncPeriod time.Duration) k8swgmanifests.ConfigParams {
	resyncPeriod := int64(cacheResyncPeriod.Seconds())

	params := k8swgmanifests.ConfigParams{
		ProfileName: schedSpec.SchedulerName,
		Cache: &k8swgmanifests.ConfigCacheParams{
			ResyncPeriodSeconds: &resyncPeriod,
		},
	}

	var foreignPodsDetect string
	var resyncMethod string = k8swgmanifests.CacheResyncAutodetect
	var informerMode string
	if *schedSpec.CacheResyncDetection == nropv1.CacheResyncDetectionRelaxed {
		foreignPodsDetect = k8swgmanifests.ForeignPodsDetectOnlyExclusiveResources
	} else {
		foreignPodsDetect = k8swgmanifests.ForeignPodsDetectAll
	}
	if *schedSpec.SchedulerInformer == k8swgmanifests.CacheInformerDedicated {
		informerMode = k8swgmanifests.CacheInformerDedicated
	} else {
		informerMode = k8swgmanifests.CacheInformerShared
	}
	params.Cache.ResyncMethod = &resyncMethod
	params.Cache.ForeignPodsDetectMode = &foreignPodsDetect
	params.Cache.InformerMode = &informerMode
	klog.InfoS("setting cache parameters", dumpConfigCacheParams(params.Cache)...)

	return params
}

func dumpConfigCacheParams(ccp *k8swgmanifests.ConfigCacheParams) []interface{} {
	return []interface{}{
		"resyncPeriod", strInt64Ptr(ccp.ResyncPeriodSeconds),
		"resyncMethod", strStringPtr(ccp.ResyncMethod),
		"foreignPodsDetectMode", strStringPtr(ccp.ForeignPodsDetectMode),
		"informerMode", strStringPtr(ccp.InformerMode),
	}
}

func strInt64Ptr(ip *int64) string {
	if ip == nil {
		return "N/A"
	}
	return fmt.Sprintf("%d", *ip)
}

func strStringPtr(sp *string) string {
	if sp == nil {
		return "N/A"
	}
	return *sp
}
