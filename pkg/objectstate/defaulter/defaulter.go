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

package defaulter

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/defaulter/generated"
	securityv1 "github.com/openshift/api/security/v1"
	"github.com/openshift/apiserver-library-go/pkg/securitycontextconstraints/sccdefaults"
)

func Service(mutated client.Object) client.Object {
	service, ok := mutated.(*corev1.Service)
	if !ok {
		klog.InfoS("object is not a Service", "type", fmt.Sprintf("%T", mutated))
	}
	generated.SetObjectDefaults_Service(service)
	return service
}

func CRD(mutated client.Object) client.Object {
	customResourceDefinition := mutated.(*apiextensionsv1.CustomResourceDefinition)
	apiextensionsv1.SetDefaults_CustomResourceDefinition(customResourceDefinition)
	return customResourceDefinition
}

func Deployment(mutated client.Object) client.Object {
	deployment := mutated.(*appsv1.Deployment)
	generated.SetObjectDefaults_Deployment(deployment)
	return deployment
}

func DaemonSet(mutated client.Object) client.Object {
	daemonSet := mutated.(*appsv1.DaemonSet)
	generated.SetObjectDefaults_DaemonSet(daemonSet)
	// for backport compatibility
	daemonSet.Spec.Template.Spec.DeprecatedServiceAccount = daemonSet.Spec.Template.Spec.ServiceAccountName
	return daemonSet
}

func ClusterRoleBinding(mutated client.Object) client.Object {
	clusterRoleBinding := mutated.(*rbacv1.ClusterRoleBinding)
	generated.SetObjectDefaults_ClusterRoleBinding(clusterRoleBinding)
	return clusterRoleBinding
}

func RoleBinding(mutated client.Object) client.Object {
	roleBinding := mutated.(*rbacv1.RoleBinding)
	generated.SetObjectDefaults_RoleBinding(roleBinding)
	return roleBinding
}

func SecurityContextConstraints(mutated client.Object) client.Object {
	securityContextConstraints := mutated.(*securityv1.SecurityContextConstraints)
	sccdefaults.SetDefaults_SCC(securityContextConstraints)
	return securityContextConstraints
}

func None(mutated client.Object) client.Object {
	return mutated
}
