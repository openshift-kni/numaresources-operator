/*
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
 *
 * Copyright 2021 Red Hat, Inc.
 */

package manifests

import (
	"embed"
	"fmt"
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

//go:embed yaml
var src embed.FS

func ServiceAccount(namespace string) (*corev1.ServiceAccount, error) {
	obj, err := loadObject(filepath.Join("yaml", "serviceaccount.yaml"))
	if err != nil {
		return nil, err
	}

	sa, ok := obj.(*corev1.ServiceAccount)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}

	if namespace != "" {
		sa.Namespace = namespace
	}
	return sa, nil
}

func ClusterRole() (*rbacv1.ClusterRole, error) {
	obj, err := loadObject(filepath.Join("yaml", "clusterrole.nrt.yaml"))
	if err != nil {
		return nil, err
	}

	cr, ok := obj.(*rbacv1.ClusterRole)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return cr, nil
}

func ClusterRoleBindingK8S(namespace string) (*rbacv1.ClusterRoleBinding, error) {
	return loadClusterRoleBinding("clusterrolebinding.yaml", namespace)
}

func ClusterRoleBindingNRT(namespace string) (*rbacv1.ClusterRoleBinding, error) {
	return loadClusterRoleBinding("clusterrolebinding.nrt.yaml", namespace)
}

func loadClusterRoleBinding(crbName, namespace string) (*rbacv1.ClusterRoleBinding, error) {
	obj, err := loadObject(filepath.Join("yaml", crbName))
	if err != nil {
		return nil, err
	}

	crb, ok := obj.(*rbacv1.ClusterRoleBinding)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	for idx := 0; idx < len(crb.Subjects); idx++ {
		crb.Subjects[idx].Namespace = namespace
	}
	return crb, nil
}

func ConfigMap(namespace string) (*corev1.ConfigMap, error) {
	obj, err := loadObject(filepath.Join("yaml", "configmap.nrt.yaml"))
	if err != nil {
		return nil, err
	}

	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	if namespace != "" {
		cm.Namespace = namespace
	}
	return cm, nil
}

func Deployment(namespace string) (*appsv1.Deployment, error) {
	obj, err := loadObject(filepath.Join("yaml", "deployment.yaml"))
	if err != nil {
		return nil, err
	}

	dp, ok := obj.(*appsv1.Deployment)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	if namespace != "" {
		dp.Namespace = namespace
	}
	return dp, nil
}

func NetworkPolicy(policyType, namespace string) (*networkingv1.NetworkPolicy, error) {
	var fileName string

	if policyType == "" || policyType == "default" {
		fileName = "networkpolicy.yaml"
	} else {
		fileName = fmt.Sprintf("networkpolicy.%s.yaml", policyType)
	}

	obj, err := loadObject(filepath.Join("yaml", fileName))
	if err != nil {
		return nil, err
	}

	np, ok := obj.(*networkingv1.NetworkPolicy)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	if namespace != "" {
		np.Namespace = namespace
	}
	return np, nil
}

func deserializeObjectFromData(data []byte) (runtime.Object, error) {
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode(data, nil, nil)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func loadObject(path string) (runtime.Object, error) {
	data, err := src.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return deserializeObjectFromData(data)
}
