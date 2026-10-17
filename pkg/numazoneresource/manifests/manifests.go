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

package manifests

import (
	"embed"
	"fmt"
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	securityv1 "github.com/openshift/api/security/v1"

	"github.com/openshift-kni/numaresources-operator/pkg/numazoneresource"
)

//go:embed yaml
var src embed.FS

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(securityv1.AddToScheme(scheme))
}

func DaemonSet(namespace string) (*appsv1.DaemonSet, error) {
	obj, err := loadObject("daemonset.yaml")
	if err != nil {
		return nil, err
	}
	ds, ok := obj.(*appsv1.DaemonSet)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %T", obj)
	}
	ds.Namespace = namespace
	return ds, nil
}

func ServiceAccount(namespace string) (*corev1.ServiceAccount, error) {
	obj, err := loadObject("serviceaccount.yaml")
	if err != nil {
		return nil, err
	}
	sa, ok := obj.(*corev1.ServiceAccount)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %T", obj)
	}
	sa.Namespace = namespace
	return sa, nil
}

func SecurityContextConstraint(namespace string) (*securityv1.SecurityContextConstraints, error) {
	obj, err := loadObject("securitycontextconstraint.yaml")
	if err != nil {
		return nil, err
	}
	scc, ok := obj.(*securityv1.SecurityContextConstraints)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %T", obj)
	}
	scc.Users = []string{"system:serviceaccount:" + namespace + ":" + numazoneresource.ServiceAccountName}
	return scc, nil
}

func loadObject(name string) (runtime.Object, error) {
	data, err := src.ReadFile(filepath.Join("yaml", name))
	if err != nil {
		return nil, err
	}
	obj, _, err := serializer.NewCodecFactory(scheme).UniversalDeserializer().Decode(data, nil, nil)
	return obj, err
}
