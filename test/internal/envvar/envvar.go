/*
 * Copyright 2026 Red Hat, Inc.
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

package envvar

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/test/internal/consts"
)

var subscriptionGVK = schema.GroupVersionKind{
	Group:   "operators.coreos.com",
	Version: "v1alpha1",
	Kind:    "Subscription",
}

type restoreFunc func(ctx context.Context) error

var noOpRestoreFunc restoreFunc = func(ctx context.Context) error { return nil }

// SetOperatorEnvVar sets envName to envValue on the operator's OLM subscription when present,
// otherwise on the manager container in the operator deployment. It returns a restore function.
func SetOperatorEnvVar(ctx context.Context, cli client.Client, envName, envValue string) (restoreFunc, error) {
	dp, err := findOperatorDeployment(ctx, cli)
	if err != nil {
		return nil, err
	}

	sub, err := findOperatorSubscription(ctx, cli, dp.Namespace)
	if err == nil {
		klog.InfoS("setting operator env via OLM subscription", "name", sub.GetName(), "namespace", sub.GetNamespace(), "env", envName, "value", envValue)
		return setOperatorEnvViaSubscription(ctx, cli, sub, envName, envValue)
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	klog.InfoS("setting operator env via operator deployment", "name", dp.Name, "namespace", dp.Namespace, "env", envName, "value", envValue)
	return setOperatorEnvViaDeployment(ctx, cli, dp, envName, envValue)
}

func findOperatorSubscription(ctx context.Context, cli client.Client, fromNamespace string) (*unstructured.Unstructured, error) {
	subList := &unstructured.UnstructuredList{}
	subList.SetGroupVersionKind(subscriptionGVK.GroupVersion().WithKind("SubscriptionList"))

	if err := cli.List(ctx, subList, client.InNamespace(fromNamespace)); err != nil {
		return nil, err
	}

	// use the subscription name option if set for more flexxibility and debugging purposes
	subName := os.Getenv("E2E_OPERATOR_SUBSCRIPTION_NAME")
	if subName != "" {
		for i := range subList.Items {
			if subList.Items[i].GetName() == subName {
				return subList.Items[i].DeepCopy(), nil
			}
		}
	}

	// in case the subscription name is not set, we pick the subscription that has the correct package name based
	// on the subscription spec. Some OLM installations configures the only one of either the package or the name,
	// but either way they have similar value.
	for i := range subList.Items {
		pkg, pkgFound, pkgErr := unstructured.NestedString(subList.Items[i].Object, "spec", "package")
		name, nameFound, nameErr := unstructured.NestedString(subList.Items[i].Object, "spec", "name")
		if pkgErr != nil && nameErr != nil {
			return nil, fmt.Errorf("failed to get package or name from subscription: pkgErr=%v nameErr=%v sub=%v", pkgErr, nameErr, subList.Items[i].Object)
		}
		if pkgFound {
			if pkg == consts.SubscriptionSpecNamePackage {
				return subList.Items[i].DeepCopy(), nil
			}
		}
		if nameFound {
			if name == consts.SubscriptionSpecNamePackage {
				return subList.Items[i].DeepCopy(), nil
			}
		}
	}
	klog.InfoS("no subscription found with the correct package name", "subscriptions", subList.Items)
	return nil, errors.NewNotFound(schema.GroupResource{Group: subscriptionGVK.Group, Resource: "subscriptions"}, subName)
}

func findOperatorDeployment(ctx context.Context, cli client.Client) (*appsv1.Deployment, error) {
	dpList := &appsv1.DeploymentList{}
	if err := cli.List(ctx, dpList, client.MatchingLabels(consts.OperatorDeploymentLabels)); err != nil {
		return nil, err
	}

	for i := range dpList.Items {
		dp := &dpList.Items[i]
		if dp.Name != consts.OperatorDeploymentName {
			continue
		}

		return dp, nil
	}
	return nil, errors.NewNotFound(schema.GroupResource{Group: appsv1.SchemeGroupVersion.Group, Resource: "deployments"}, consts.OperatorDeploymentName)
}

func setOperatorEnvViaSubscription(ctx context.Context, cli client.Client, sub *unstructured.Unstructured, envName, envValue string) (restoreFunc, error) {
	dpKey := client.ObjectKey{Namespace: sub.GetNamespace(), Name: consts.OperatorDeploymentName}
	initialDp, initialPods, err := getPodsForDeployment(ctx, cli, dpKey)
	if err != nil {
		return nil, err
	}

	subKey := client.ObjectKeyFromObject(sub)
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(subscriptionGVK)
	if err := cli.Get(ctx, subKey, current); err != nil {
		return nil, err
	}
	originalEnv, err := getSubscriptionEnv(current)
	if err != nil {
		return nil, err
	}
	klog.InfoS("original env", "env", originalEnv)

	klog.InfoS("checking if the env is already set")
	originalEnvVars := toEnvVars(originalEnv)
	if slices.ContainsFunc(originalEnvVars, func(env corev1.EnvVar) bool {
		return env.Name == envName && env.Value == envValue
	}) {
		klog.InfoS("env is already set", "env", envName, "value", envValue)
		return noOpRestoreFunc, nil
	}

	updatedEnv := buildSubscriptionWithNewEnvVal(originalEnv, envName, envValue)
	klog.InfoS("updated env", "env", updatedEnv)
	// TODO: extract this to a function and use it in creating the restore function
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &unstructured.Unstructured{}
		current.SetGroupVersionKind(subscriptionGVK)
		if err := cli.Get(ctx, subKey, current); err != nil {
			return err
		}
		if err := setSubscriptionEnv(current, updatedEnv); err != nil {
			return err
		}
		return cli.Update(ctx, current)
	}); err != nil {
		return nil, err
	}
	if err := ensureSubscriptionIsUpdated(ctx, cli, subKey, updatedEnv); err != nil {
		return nil, err
	}
	if err := waitForDeploymentRolloutAfterSubscriptionUpdate(ctx, cli, initialDp, initialPods, []corev1.EnvVar{{Name: envName, Value: envValue}}); err != nil {
		return nil, err
	}

	return func(ctx context.Context) error {
		dp, pods, err := getPodsForDeployment(ctx, cli, dpKey)
		if err != nil {
			return fmt.Errorf("failed to get deployment details for restore: err=%v", err)
		}

		klog.InfoS("restoring subscription env", "env", originalEnv)
		current := &unstructured.Unstructured{}
		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current.SetGroupVersionKind(subscriptionGVK)
			if err := cli.Get(ctx, subKey, current); err != nil {
				return err
			}
			if err := setSubscriptionEnv(current, originalEnv); err != nil {
				return err
			}
			return cli.Update(ctx, current)
		}); err != nil {
			return fmt.Errorf("failed to update subscription during restore: err=%v", err)
		}
		if err := ensureSubscriptionIsUpdated(ctx, cli, subKey, originalEnv); err != nil {
			return err
		}
		return waitForDeploymentRolloutAfterSubscriptionUpdate(ctx, cli, dp, pods, originalEnvVars)
	}, nil
}

func setOperatorEnvViaDeployment(ctx context.Context, cli client.Client, dp *appsv1.Deployment, envName, envValue string) (restoreFunc, error) {
	dpKey := client.ObjectKeyFromObject(dp)
	current := &appsv1.Deployment{}
	if err := cli.Get(ctx, dpKey, current); err != nil {
		return nil, err
	}
	initialDp := current.DeepCopy()
	klog.InfoS("initial env", "env", toString(initialDp.Spec.Template.Spec.Containers[0].Env))
	mngContainerIdx := -1
	for idx := range dp.Spec.Template.Spec.Containers {
		if dp.Spec.Template.Spec.Containers[idx].Name == consts.OperatorManagerContainer {
			mngContainerIdx = idx
			break
		}
	}
	if mngContainerIdx == -1 {
		return nil, errors.NewNotFound(schema.GroupResource{Group: appsv1.SchemeGroupVersion.Group, Resource: "containers"}, consts.OperatorManagerContainer)
	}
	mngContainer := initialDp.Spec.Template.Spec.Containers[mngContainerIdx].DeepCopy()
	updated := false
	for i := range mngContainer.Env {
		if mngContainer.Env[i].Name == envName {
			if mngContainer.Env[i].Value == envValue {
				klog.InfoS("env is already set", "env", envName, "value", envValue)
				return noOpRestoreFunc, nil
			}
			mngContainer.Env[i].Value = envValue
			updated = true
			break
		}
	}
	if !updated {
		mngContainer.Env = append(mngContainer.Env, corev1.EnvVar{Name: envName, Value: envValue})
	}
	if err := updateDeploymentContainerAndWaitForRollout(ctx, cli, dpKey, mngContainerIdx, mngContainer); err != nil {
		return nil, err
	}

	return func(ctx context.Context) error {
		return updateDeploymentContainerAndWaitForRollout(ctx, cli, dpKey, mngContainerIdx, initialDp.Spec.Template.Spec.Containers[mngContainerIdx].DeepCopy())
	}, nil
}

func getPodsForDeployment(ctx context.Context, cli client.Client, dpKey client.ObjectKey) (*appsv1.Deployment, []corev1.Pod, error) {
	klog.InfoS("getting deployment pods", "key", dpKey.String())
	dp := &appsv1.Deployment{}
	if err := cli.Get(ctx, dpKey, dp); err != nil {
		return nil, nil, err
	}
	pods, err := podlist.With(cli).ByDeployment(ctx, *dp)
	if len(pods) == 0 || err != nil {
		return nil, nil, fmt.Errorf("failed to get deployment pods: err=%v pods=%v", err, pods)
	}
	return dp, pods, nil
}

func waitForDeploymentRolloutAfterSubscriptionUpdate(ctx context.Context, cli client.Client, dp *appsv1.Deployment, initialPods []corev1.Pod, expectedEnvVarValues []corev1.EnvVar) error {
	// Sometimes OLM fails to immediately update the deployment with the new env value,
	// so we workaround this by explicitly deleting the deployment. Note that recreating
	// the deployment can also take some time, being affected by the OLM rollout behavior,
	// hence we give long enough time for its recreation.
	klog.Info("deleting deployment")
	if err := cli.Delete(ctx, dp); err != nil {
		return err
	}

	err := k8swait.PollUntilContextTimeout(ctx, 10*time.Second, 15*time.Minute, true, func(aContext context.Context) (bool, error) {
		if err := cli.Get(aContext, client.ObjectKeyFromObject(dp), dp); err != nil {
			klog.Warningf("failed getting the deployment %s: %v", dp.Name, err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for deployment to be recreated: err=%v", err)
	}
	return waitForDeploymentRollout(ctx, cli, dp, initialPods, expectedEnvVarValues)
}

func waitForDeploymentRollout(ctx context.Context, cli client.Client, dp *appsv1.Deployment, initialPods []corev1.Pod, expectedEnvVarValues []corev1.EnvVar) error {
	if len(initialPods) == 0 {
		klog.Info("no initial pods to wait for to be terminated")
	} else {
		klog.Info("waiting for initial pods to be terminated")
		for _, pod := range initialPods {
			if err := cli.Delete(ctx, &pod); err != nil {
				if !errors.IsNotFound(err) {
					return err
				}
				continue
			}
		}

		err := wait.With(cli).Interval(30*time.Second).Timeout(5*time.Minute).ForPodsAllDeleted(ctx, podlist.ToPods(initialPods))
		if err != nil {
			return fmt.Errorf("failed to delete initial pods: err=%v", err)
		}
	}

	klog.InfoS("polling deployment until it's updated with the new env value", "expected", toString(expectedEnvVarValues))
	expectedEnvsSorted := make([]corev1.EnvVar, len(expectedEnvVarValues))
	copy(expectedEnvsSorted, expectedEnvVarValues)
	sort.Slice(expectedEnvsSorted, func(i, j int) bool {
		return expectedEnvsSorted[i].Name < expectedEnvsSorted[j].Name
	})
	dpKey := client.ObjectKeyFromObject(dp)
	updatedDp := &appsv1.Deployment{}
	err := k8swait.PollUntilContextTimeout(ctx, 10*time.Second, 15*time.Minute, true, func(aContext context.Context) (bool, error) {
		err := cli.Get(aContext, dpKey, updatedDp)
		if err != nil {
			klog.Warningf("failed getting the deployment %s: %v", dpKey.String(), err)
			return false, err
		}

		for _, expectedEnv := range expectedEnvVarValues {
			found := false
			for _, env := range updatedDp.Spec.Template.Spec.Containers[0].Env {
				if env.Name == expectedEnv.Name {
					if env.Value == expectedEnv.Value {
						found = true
						break
					}
				}
			}
			if !found {
				klog.InfoS("deployment not yet updated with the new env value",
					"varname", expectedEnv.Name,
					"varvalue", expectedEnv.Value,
					"found", toString(updatedDp.Spec.Template.Spec.Containers[0].Env))
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("deployment was not updated with the new env value: err=%v", err)
	}

	klog.InfoS("deployment updated as expected", "env", toString(updatedDp.Spec.Template.Spec.Containers[0].Env))
	klog.Info("waiting for updated deployment to be complete")
	_, err = wait.With(cli).Interval(30*time.Second).Timeout(5*time.Minute).ForDeploymentComplete(ctx, dp)
	return err
}

func updateDeploymentContainerAndWaitForRollout(ctx context.Context, cli client.Client, key types.NamespacedName, containerIdx int, newContainer *corev1.Container) error {
	updatedSuccessfully := false
	retries := 10
	interval := 2 * time.Second
	dp, initialPods, err := getPodsForDeployment(ctx, cli, key)
	if err != nil {
		return fmt.Errorf("failed to get deployment details: err=%v", err)
	}

	klog.Info("updated deployment container")
	updated := &appsv1.Deployment{}
	for i := 0; i < retries; i++ {
		if err := cli.Get(ctx, key, updated); err != nil {
			continue
		}

		updated.Spec.Template.Spec.Containers[containerIdx] = *newContainer
		if err := cli.Update(ctx, updated); err == nil {
			updatedSuccessfully = true
			break
		}
		time.Sleep(interval)
	}

	if !updatedSuccessfully {
		return fmt.Errorf("failed to update deployment %q", key.String())
	}

	return waitForDeploymentRollout(ctx, cli, dp, initialPods, newContainer.Env)
}

func getSubscriptionEnv(sub *unstructured.Unstructured) ([]any, error) {
	env, found, err := unstructured.NestedSlice(sub.Object, "spec", "config", "env")
	if err != nil {
		return nil, err
	}
	if !found {
		return []any{}, nil
	}

	if len(env) == 0 {
		return []any{}, nil
	}
	cloned := make([]any, len(env))
	for i, item := range env {
		if entry, ok := item.(map[string]any); ok {
			entryCopy := make(map[string]any, len(entry))
			for k, v := range entry {
				entryCopy[k] = v
			}
			cloned[i] = entryCopy
			continue
		}
		cloned[i] = item
	}
	return cloned, nil
}

func setSubscriptionEnv(sub *unstructured.Unstructured, env []any) error {
	return unstructured.SetNestedSlice(sub.Object, env, "spec", "config", "env")
}

func buildSubscriptionWithNewEnvVal(env []any, name, value string) []any {
	for i, item := range env {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entryName, _ := entry["name"].(string)
		if entryName != name {
			continue
		}
		entry["value"] = value
		env[i] = entry
		return env
	}
	return append(env, map[string]any{
		"name":  name,
		"value": value,
	})
}

func ensureSubscriptionIsUpdated(ctx context.Context, cli client.Client, subKey client.ObjectKey, expectedEnv []any) error {
	klog.InfoS("ensuring subscription is updated with the correct env value")
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(subscriptionGVK)
	if err := cli.Get(ctx, subKey, current); err != nil {
		return err
	}
	foundEnv, err := getSubscriptionEnv(current)
	if err != nil {
		return err
	}
	foundEnvVars := toEnvVars(foundEnv)
	expectedEnvVars := toEnvVars(expectedEnv)
	if !reflect.DeepEqual(foundEnvVars, expectedEnvVars) {
		return fmt.Errorf("subscription env is not updated with the correct env value: found=%v expected=%v", foundEnvVars, expectedEnvVars)
	}
	return nil
}

func toString(envs []corev1.EnvVar) string {
	sb := strings.Builder{}
	for _, env := range envs {
		sb.WriteString(fmt.Sprintf("%s=%s, ", env.Name, env.Value))
	}
	return sb.String()
}

func toEnvVars(envs []any) []corev1.EnvVar {
	envVars := []corev1.EnvVar{}
	for _, env := range envs {
		entry, ok := env.(map[string]any)
		if !ok {
			continue
		}
		envVars = append(envVars, corev1.EnvVar{
			Name:  entry["name"].(string),
			Value: entry["value"].(string),
		})
	}
	return envVars
}
