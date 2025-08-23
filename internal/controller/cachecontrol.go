/*
 * Copyright 2025 Red Hat, Inc.
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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/cache"

	nropcache "github.com/openshift-kni/numaresources-operator/internal/controller/cache"
)

type LocalManager struct {
	ns string
	od *nropcache.ObjectDeduper
}

func NewLocalManager(namespace string) *LocalManager {
	return &LocalManager{
		ns: namespace,
		od: nropcache.NewObjectDeduper(),
	}
}

func (lm *LocalManager) CacheOptions() cache.Options {
	namespace := lm.ns // shortcut
	byObjCache := nropcache.MakeByObject(lm.od).
		For(&corev1.Service{}, namespace).
		For(&corev1.ServiceAccount{}, namespace).
		For(&corev1.ConfigMap{}, namespace).
		For(&corev1.Pod{}, namespace).
		For(&rbacv1.Role{}, namespace).
		For(&rbacv1.RoleBinding{}, namespace).
		For(&appsv1.Deployment{}, namespace).
		For(&appsv1.DaemonSet{}, namespace).
		For(&networkingv1.NetworkPolicy{}, namespace).
		Done()

	return cache.Options{
		ByObject: byObjCache,
		DefaultNamespaces: map[string]cache.Config{
			metav1.NamespaceAll: {},
		},
	}
}
