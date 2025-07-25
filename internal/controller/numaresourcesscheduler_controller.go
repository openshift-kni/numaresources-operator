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

package controller

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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	k8swgmanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	k8swgrbacupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate/rbac"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	"github.com/openshift-kni/numaresources-operator/internal/relatedobjects"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
	nrosched "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	schedupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

const (
	// ActivePodsResourcesSupportSince defines the OCP version which started to support the fixed kubelet
	// in which the PodResourcesAPI lists the active pods by default
	activePodsResourcesSupportSince = "4.20.999"
)

type PlatformInfo struct {
	Platform platform.Platform
	Version  platform.Version
}

// NUMAResourcesSchedulerReconciler reconciles a NUMAResourcesScheduler object
type NUMAResourcesSchedulerReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	SchedulerManifests schedmanifests.Manifests
	Namespace          string
	PlatformInfo       PlatformInfo
}

//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=*
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=*
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=*
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=*
//+kubebuilder:rbac:groups="",resources=nodes,verbs=list;watch
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
		return ctrl.Result{}, r.updateStatus(ctx, instance, status.ConditionDegraded, status.ConditionTypeIncorrectNUMAResourcesSchedulerResourceName, message)
	}

	if annotations.IsPauseReconciliationEnabled(instance.Annotations) {
		klog.InfoS("Pause reconciliation enabled", "object", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	result, condition, err := r.reconcileResource(ctx, instance)
	if err := r.updateStatus(ctx, instance, condition, status.ReasonFromError(err), status.MessageFromError(err)); err != nil {
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
	nodesPredicate := predicate.Funcs{
		// we only care about cases when nodes are getting created or deleted
		CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
			return true
		},
		DeleteFunc: func(e event.TypedDeleteEvent[client.Object]) bool {
			return true
		},
		UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool {
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&nropv1.NUMAResourcesScheduler{}).
		Owns(&rbacv1.ClusterRole{}, builder.WithPredicates(p)).
		Owns(&rbacv1.ClusterRoleBinding{}, builder.WithPredicates(p)).
		Owns(&corev1.ServiceAccount{}, builder.WithPredicates(p)).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(p)).
		Owns(&appsv1.Deployment{}, builder.WithPredicates(p)).
		Watches(&corev1.Node{}, handler.EnqueueRequestsFromMapFunc(r.nodeToNUMAResourcesScheduler),
			builder.WithPredicates(nodesPredicate)).
		Complete(r)
}

func (r *NUMAResourcesSchedulerReconciler) nodeToNUMAResourcesScheduler(ctx context.Context, object client.Object) []reconcile.Request {
	var requests []reconcile.Request
	nross := &nropv1.NUMAResourcesSchedulerList{}
	if err := r.List(ctx, nross); err != nil {
		klog.ErrorS(err, "failed to List NUMAResourcesScheduler")
	}
	for _, instance := range nross.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKey{
			Name: instance.Name,
		}})
	}
	return requests
}

func (r *NUMAResourcesSchedulerReconciler) reconcileResource(ctx context.Context, instance *nropv1.NUMAResourcesScheduler) (reconcile.Result, string, error) {
	schedStatus, err := r.syncNUMASchedulerResources(ctx, instance)
	if err != nil {
		return ctrl.Result{}, status.ConditionDegraded, fmt.Errorf("FailedSchedulerSync: %w", err)
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

func (r *NUMAResourcesSchedulerReconciler) computeSchedulerReplicas(ctx context.Context, schedSpec nropv1.NUMAResourcesSchedulerSpec) (*int32, error) {
	// do not autodetect if explicitly set by the user
	if schedSpec.Replicas != nil {
		return schedSpec.Replicas, nil
	}
	nodeList := &corev1.NodeList{}
	if err := r.Client.List(ctx, nodeList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"node-role.kubernetes.io/control-plane": "",
		}),
	}); err != nil {
		return schedSpec.Replicas, err
	}
	replicas := ptr.To(int32(len(nodeList.Items)))
	klog.InfoS("autodetect scheduler replicas", "replicas", *replicas)
	return replicas, nil
}

func (r *NUMAResourcesSchedulerReconciler) syncNUMASchedulerResources(ctx context.Context, instance *nropv1.NUMAResourcesScheduler) (nropv1.NUMAResourcesSchedulerStatus, error) {
	klog.V(4).Info("SchedulerSync start")
	defer klog.V(4).Info("SchedulerSync stop")

	platformNormalize(&instance.Spec, r.PlatformInfo)

	schedSpec := instance.Spec.Normalize()
	cacheResyncPeriod := unpackAPIResyncPeriod(schedSpec.CacheResyncPeriod)
	replicas, err := r.computeSchedulerReplicas(ctx, schedSpec)
	if err != nil {
		return nropv1.NUMAResourcesSchedulerStatus{}, fmt.Errorf("failed to compute scheduler replicas: %w", err)
	}
	schedSpec.Replicas = replicas
	params := configParamsFromSchedSpec(schedSpec, cacheResyncPeriod, r.Namespace)

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

	r.SchedulerManifests.Deployment.Spec.Replicas = schedSpec.Replicas
	klog.V(4).InfoS("using scheduler replicas", "replicas", *r.SchedulerManifests.Deployment.Spec.Replicas)
	// TODO: if replicas doesn't make sense (autodetect disabled and user set impossible value) then we
	// should set a degraded state

	// node-critical so the pod won't be preempted by pods having the most critical priority class
	r.SchedulerManifests.Deployment.Spec.Template.Spec.PriorityClassName = nrosched.SchedulerPriorityClassName
	if err := schedupdate.SchedulerResourcesRequest(r.SchedulerManifests.Deployment, instance); err != nil {
		// NOT CRITICAL! should never happen. Trust the existing manifests and move on
		// TODO: this becomes critical once we remove the defaults from the manifests
		klog.ErrorS(err, "failed to enforce the scheduler resources, continuing with manifests defaults")
	}

	schedupdate.DeploymentImageSettings(r.SchedulerManifests.Deployment, schedSpec.SchedulerImage)
	cmHash := hash.ConfigMapData(r.SchedulerManifests.ConfigMap)
	schedupdate.DeploymentConfigMapSettings(r.SchedulerManifests.Deployment, r.SchedulerManifests.ConfigMap.Name, cmHash)
	if err := loglevel.UpdatePodSpec(&r.SchedulerManifests.Deployment.Spec.Template.Spec, "", schedSpec.LogLevel); err != nil {
		return schedStatus, err
	}

	schedupdate.DeploymentEnvVarSettings(r.SchedulerManifests.Deployment, schedSpec)

	k8swgrbacupdate.RoleForLeaderElection(r.SchedulerManifests.Role, r.Namespace, nrosched.LeaderElectionResourceName)
	k8swgrbacupdate.RoleBinding(r.SchedulerManifests.RoleBinding, r.SchedulerManifests.ServiceAccount.Name, r.Namespace)

	existing := schedstate.FromClient(ctx, r.Client, r.SchedulerManifests)
	for _, objState := range existing.State(r.SchedulerManifests) {
		if err := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme); err != nil {
			return schedStatus, fmt.Errorf("failed to set controller reference to %s %s: %w", objState.Desired.GetNamespace(), objState.Desired.GetName(), err)
		}
		obj, _, err := apply.ApplyObject(ctx, r.Client, objState)
		if err != nil {
			return schedStatus, fmt.Errorf("could not apply (%s) %s/%s: %w", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName(), err)
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

func platformNormalize(spec *nropv1.NUMAResourcesSchedulerSpec, platInfo PlatformInfo) {
	if platInfo.Platform != platform.OpenShift && platInfo.Platform != platform.HyperShift {
		return
	}

	parsedVersion, _ := platform.ParseVersion(activePodsResourcesSupportSince)
	ok, err := platInfo.Version.AtLeast(parsedVersion)
	if err != nil {
		klog.Infof("failed to compare version %v with %v, err %v", parsedVersion, platInfo.Version, err)
		return
	}

	if !ok {
		return
	}

	if spec.SchedulerInformer == nil {
		spec.SchedulerInformer = ptr.To(nropv1.SchedulerInformerShared)
		klog.V(4).InfoS("SchedulerInformer default is overridden", "Platform", platInfo.Platform, "PlatformVersion", platInfo.Version.String(), "SchedulerInformer", &spec.SchedulerInformer)
	}
}
func (r *NUMAResourcesSchedulerReconciler) updateStatus(ctx context.Context, sched *nropv1.NUMAResourcesScheduler, condition string, reason string, message string) error {
	sched.Status.Conditions, _ = status.UpdateConditions(sched.Status.Conditions, condition, reason, message)
	if err := r.Client.Status().Update(ctx, sched); err != nil {
		return fmt.Errorf("could not update status for object %s: %w", client.ObjectKeyFromObject(sched), err)
	}
	return nil
}

func unpackAPIResyncPeriod(reconcilePeriod *metav1.Duration) time.Duration {
	period := reconcilePeriod.Round(time.Second)
	klog.InfoS("setting reconcile period", "computed", period, "supplied", reconcilePeriod)
	return period
}

func configParamsFromSchedSpec(schedSpec nropv1.NUMAResourcesSchedulerSpec, cacheResyncPeriod time.Duration, namespace string) k8swgmanifests.ConfigParams {
	resyncPeriod := int64(cacheResyncPeriod.Seconds())
	// if no actual replicas are required, leader election is unnecessary, so
	// we force it to off to reduce the background noise.
	// note: the api validation/normalization layer must ensure this value is != nil
	leaderElect := (*schedSpec.Replicas > 1)

	params := k8swgmanifests.ConfigParams{
		ProfileName: schedSpec.SchedulerName,
		Cache: &k8swgmanifests.ConfigCacheParams{
			ResyncPeriodSeconds: &resyncPeriod,
		},
		ScoringStrategy: &k8swgmanifests.ScoringStrategyParams{},
		LeaderElection: &k8swgmanifests.LeaderElectionParams{
			// Make sure to always set explicitly the value and override the configmap defaults.
			LeaderElect: leaderElect,
			// unconditionally set those to make sure
			// to play nice with the cluster and the main scheduler
			ResourceNamespace: namespace,
			ResourceName:      nrosched.LeaderElectionResourceName,
		},
	}

	klog.V(2).InfoS("setting leader election parameters", dumpLeaderElectionParams(params.LeaderElection)...)

	var foreignPodsDetect string
	var resyncMethod string = k8swgmanifests.CacheResyncAutodetect
	var informerMode string
	var scoringStrategyType string
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

	switch sst := schedSpec.ScoringStrategy.Type; sst {
	case nropv1.LeastAllocated:
		scoringStrategyType = k8swgmanifests.ScoringStrategyLeastAllocated
	case nropv1.BalancedAllocation:
		scoringStrategyType = k8swgmanifests.ScoringStrategyBalancedAllocation
	case nropv1.MostAllocated:
		scoringStrategyType = k8swgmanifests.ScoringStrategyMostAllocated
	default:
		scoringStrategyType = k8swgmanifests.ScoringStrategyLeastAllocated
	}
	params.ScoringStrategy.Type = scoringStrategyType

	var resources []k8swgmanifests.ResourceSpecParams
	for _, resource := range schedSpec.ScoringStrategy.Resources {
		resources = append(resources, k8swgmanifests.ResourceSpecParams{
			Name:   resource.Name,
			Weight: resource.Weight,
		})
	}
	params.ScoringStrategy.Resources = resources
	params.Cache.ResyncMethod = &resyncMethod
	params.Cache.ForeignPodsDetectMode = &foreignPodsDetect
	params.Cache.InformerMode = &informerMode
	klog.V(2).InfoS("setting cache parameters", dumpConfigCacheParams(params.Cache)...)

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

func dumpLeaderElectionParams(lep *k8swgmanifests.LeaderElectionParams) []interface{} {
	return []interface{}{
		"leaderElect", lep.LeaderElect,
		"resourceNamespace", lep.ResourceNamespace,
		"resourceName", lep.ResourceName,
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
