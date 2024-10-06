/*
 * Copyright 2024 Red Hat, Inc.
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

package manifests

import (
	"embed"
	"fmt"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

//go:embed yaml
var src embed.FS

func Service(namespace string) (*corev1.Service, error) {
	obj, err := loadObject(filepath.Join("yaml", "service.yaml"))
	if err != nil {
		return nil, err
	}

	service, ok := obj.(*corev1.Service)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}

	if namespace != "" {
		service.Namespace = namespace
	}
	return service, nil
}

func ClusterRoleBinding(namespace string) (*rbacv1.ClusterRoleBinding, error) {
	return loadClusterRoleBinding("clusterrolebinding.yaml", namespace)
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
