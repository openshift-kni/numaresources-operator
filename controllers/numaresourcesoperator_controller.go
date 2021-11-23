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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	apistate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/api"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	rtestate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

const (
	defaultNUMAResourcesOperatorCrName = "numaresourcesoperator"
)

// NUMAResourcesOperatorReconciler reconciles a NUMAResourcesOperator object
type NUMAResourcesOperatorReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Platform     platform.Platform
	APIManifests apimanifests.Manifests
	RTEManifests rtemanifests.Manifests
	Helper       *deployer.Helper
	Namespace    string
	ImageSpec    string
}

// TODO: narrow down

// Namespace Scoped
// TODO

// Cluster Scoped
//+kubebuilder:rbac:groups=topology.node.k8s.io,resources=noderesourcetopologies,verbs=get;list;create;update
//+kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=list
//+kubebuilder:rbac:groups=machineconfiguration.openshift.io,resources=machineconfigs,verbs=*
//+kubebuilder:rbac:groups=machineconfiguration.openshift.io,resources=machineconfigpools,verbs=get;list;watch
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=*
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=*
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=*
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=*
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=*
//+kubebuilder:rbac:groups=nodetopology.openshift.io,resources=numaresourcesoperators,verbs=*
//+kubebuilder:rbac:groups=nodetopology.openshift.io,resources=numaresourcesoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nodetopology.openshift.io,resources=numaresourcesoperators/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *NUMAResourcesOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()

	instance := &nropv1alpha1.NUMAResourcesOperator{}
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

	if req.Name != defaultNUMAResourcesOperatorCrName {
		err := fmt.Errorf("NUMAResourcesOperator resource name must be %q", defaultNUMAResourcesOperatorCrName)
		klog.ErrorS(err, "Incorrect NUMAResourcesOperator resource name", "name", req.Name)
		if err := status.Update(ctx, r.Client, instance, status.ConditionDegraded, "IncorrectNUMAResourcesOperatorResourceName", fmt.Sprintf("Incorrect NUMAResourcesOperator resource name: %s", req.Name)); err != nil {
			klog.ErrorS(err, "Failed to update numaresourcesoperator status", "Desired status", status.ConditionDegraded)
		}
		return ctrl.Result{}, nil // Return success to avoid requeue
	}

	result, condition, err := r.reconcileResource(ctx, instance)
	if condition != "" {
		// TODO: use proper reason
		reason, message := condition, messageFromError(err)
		if err := status.Update(context.TODO(), r.Client, instance, condition, reason, message); err != nil {
			klog.InfoS("Failed to update numaresourcesoperator status", "Desired condition", condition, "error", err)
		}
	}
	return result, err
}

func messageFromError(err error) string {
	if err == nil {
		return ""
	}
	unwErr := errors.Unwrap(err)
	if unwErr == nil {
		return ""
	}
	return unwErr.Error()
}

func (r *NUMAResourcesOperatorReconciler) reconcileResource(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator) (ctrl.Result, string, error) {
	var err error
	err = r.syncNodeResourceTopologyAPI()
	if err != nil {
		return ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "FailedAPISync")
	}

	daemonSetsInfo, err := r.syncNUMAResourcesOperatorResources(ctx, instance)
	if err != nil {
		return ctrl.Result{}, status.ConditionDegraded, errors.Wrapf(err, "FailedRTESync")
	}

	instance.Status.DaemonSets = []nropv1alpha1.NamespacedName{}
	for _, nname := range daemonSetsInfo {
		ok, err := r.Helper.IsDaemonSetRunning(nname.Namespace, nname.Name)
		if err != nil {
			return ctrl.Result{}, status.ConditionDegraded, err
		}
		if !ok {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, status.ConditionProgressing, nil
		}

		instance.Status.DaemonSets = append(instance.Status.DaemonSets, nname)
	}

	return ctrl.Result{}, status.ConditionAvailable, nil
}

func (r *NUMAResourcesOperatorReconciler) syncNodeResourceTopologyAPI() error {
	klog.Info("APISync start")

	existing := apistate.FromClient(context.TODO(), r.Client, r.Platform, r.APIManifests)

	for _, objState := range existing.State(r.APIManifests) {
		if _, err := apply.ApplyObject(context.TODO(), r.Client, objState); err != nil {
			return errors.Wrapf(err, "could not create %s", objState.Desired.GetObjectKind().GroupVersionKind().String())
		}
	}
	return nil
}

func (r *NUMAResourcesOperatorReconciler) syncNUMAResourcesOperatorResources(ctx context.Context, instance *nropv1alpha1.NUMAResourcesOperator) ([]nropv1alpha1.NamespacedName, error) {
	klog.Info("RTESync start")

	mcps, err := getNodeGroupsMCPs(ctx, r.Client, instance.Spec.NodeGroups)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get machine config pools related to instance node groups")
	}

	rtestate.UpdateDaemonSetImage(r.RTEManifests.DaemonSet, r.getExporterImage(instance.Spec.ExporterImage))

	var daemonSetsNName []nropv1alpha1.NamespacedName
	existing := rtestate.FromClient(ctx, r.Client, r.Platform, r.RTEManifests, instance, mcps, r.Namespace)
	for _, objState := range existing.State(r.RTEManifests, instance, mcps) {
		if err := controllerutil.SetControllerReference(instance, objState.Desired, r.Scheme); err != nil {
			return nil, errors.Wrapf(err, "Failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
		obj, err := apply.ApplyObject(ctx, r.Client, objState)
		if err != nil {
			return nil, errors.Wrapf(err, "could not apply (%s) %s/%s", objState.Desired.GetObjectKind().GroupVersionKind(), objState.Desired.GetNamespace(), objState.Desired.GetName())
		}

		if nname, ok := rte.DaemonSetNamespacedNameFromObject(obj); ok {
			daemonSetsNName = append(daemonSetsNName, nname)
		}
	}
	return daemonSetsNName, nil
}

func (r *NUMAResourcesOperatorReconciler) getExporterImage(imgSpec string) string {
	reason := "user-provided"
	imageSpec := imgSpec
	if imageSpec == "" {
		reason = "builtin"
		imageSpec = r.ImageSpec
	}
	klog.V(3).InfoS("Exporter image", "reason", reason, "pullSpec", imageSpec)
	return imageSpec
}

// SetupWithManager sets up the controller with the Manager.
func (r *NUMAResourcesOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// we want to initate reconcile loop only on change under labels or spec of the object
	p := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if !validateUpdateEvent(&e) {
				return false
			}

			return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration() ||
				!apiequality.Semantic.DeepEqual(e.ObjectNew.GetLabels(), e.ObjectOld.GetLabels())
		},
	}

	b := ctrl.NewControllerManagedBy(mgr).For(&nropv1alpha1.NUMAResourcesOperator{})
	if r.Platform == platform.OpenShift {
		b = b.Owns(&securityv1.SecurityContextConstraints{}).
			Owns(&machineconfigv1.MachineConfig{}, builder.WithPredicates(p))
	}
	return b.Owns(&apiextensionv1.CustomResourceDefinition{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.Role{}).
		Owns(&appsv1.DaemonSet{}, builder.WithPredicates(p)).
		Complete(r)
}

func validateUpdateEvent(e *event.UpdateEvent) bool {
	if e.ObjectOld == nil {
		klog.Error("Update event has no old runtime object to update")
		return false
	}
	if e.ObjectNew == nil {
		klog.Error("Update event has no new runtime object for update")
		return false
	}

	return true
}

func getNodeGroupsMCPs(ctx context.Context, cli client.Client, nodeGroups []nropv1alpha1.NodeGroup) ([]*machineconfigv1.MachineConfigPool, error) {
	mcps := &machineconfigv1.MachineConfigPoolList{}
	if err := cli.List(ctx, mcps); err != nil {
		return nil, err
	}

	var result []*machineconfigv1.MachineConfigPool
	for i := range mcps.Items {
		mcp := &mcps.Items[i]

		for _, nodeGroup := range nodeGroups {
			if nodeGroup.MachineConfigPoolSelector == nil {
				continue
			}

			selector, err := metav1.LabelSelectorAsSelector(nodeGroup.MachineConfigPoolSelector)
			if err != nil {
				return nil, err
			}

			mcpLabels := labels.Set(mcp.Labels)
			if selector.Matches(mcpLabels) {
				result = append(result, mcp)
			}
		}
	}

	return result, nil
}
