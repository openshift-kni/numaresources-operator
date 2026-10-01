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
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"text/template"

	igntypes "github.com/coreos/ignition/v2/config/v3_2/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	securityv1 "github.com/openshift/api/security/v1"

	rteassets "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/assets/rte"
	selinuxassets "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/assets/selinux"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/platform"
)

const (
	ContainerNameRTE             = "resource-topology-exporter"
	defaultIgnitionVersion       = "3.2.0"
	defaultIgnitionContentSource = "data:text/plain;charset=utf-8;base64"
	defaultOCIHooksDir           = "/etc/containers/oci/hooks.d"
	defaultScriptsDir            = "/usr/local/bin"
	templateSELinuxPolicyDst     = "selinuxPolicyDst"
	templateNotifierBinaryDst    = "notifierScriptPath"
	templateNotifierFilePath     = "notifierFilePath"
	DefaultNetworkPolicy         = "default"
	APIServerNetworkPolicy       = "apiserver"
	MetricsServerNetworkPolicy   = "metrics"
)

//go:embed yaml
var src embed.FS

func init() {
	apiextensionv1.AddToScheme(scheme.Scheme) //nolint:errcheck
	machineconfigv1.Install(scheme.Scheme)    //nolint:errcheck
	securityv1.Install(scheme.Scheme)         //nolint:errcheck
}

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

func Role(namespace string) (*rbacv1.Role, error) {
	obj, err := loadObject(filepath.Join("yaml", "role.yaml"))
	if err != nil {
		return nil, err
	}

	role, ok := obj.(*rbacv1.Role)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}

	if namespace != "" {
		role.Namespace = namespace
	}
	return role, nil
}

func RoleBinding(namespace string) (*rbacv1.RoleBinding, error) {
	obj, err := loadObject(filepath.Join("yaml", "rolebinding.yaml"))
	if err != nil {
		return nil, err
	}

	rb, ok := obj.(*rbacv1.RoleBinding)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}

	if namespace != "" {
		rb.Namespace = namespace
	}
	return rb, nil
}

func ClusterRole() (*rbacv1.ClusterRole, error) {
	obj, err := loadObject(filepath.Join("yaml", "clusterrole.yaml"))
	if err != nil {
		return nil, err
	}

	cr, ok := obj.(*rbacv1.ClusterRole)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return cr, nil
}

func ClusterRoleBinding() (*rbacv1.ClusterRoleBinding, error) {
	obj, err := loadObject(filepath.Join("yaml", "clusterrolebinding.yaml"))
	if err != nil {
		return nil, err
	}

	crb, ok := obj.(*rbacv1.ClusterRoleBinding)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return crb, nil
}

func APICRD() (*apiextensionv1.CustomResourceDefinition, error) {
	obj, err := loadObject("yaml/crd.yaml")
	if err != nil {
		return nil, err
	}

	crd, ok := obj.(*apiextensionv1.CustomResourceDefinition)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return crd, nil
}

func DaemonSet(namespace string) (*appsv1.DaemonSet, error) {
	obj, err := loadObject(filepath.Join("yaml", "daemonset.yaml"))
	if err != nil {
		return nil, err
	}

	ds, ok := obj.(*appsv1.DaemonSet)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}

	ds.Namespace = namespace
	return ds, nil
}

func MachineConfig(ver platform.Version, withCRIHooks bool) (*machineconfigv1.MachineConfig, error) {
	obj, err := loadObject(filepath.Join("yaml", "machineconfig.yaml"))
	if err != nil {
		return nil, err
	}

	mc, ok := obj.(*machineconfigv1.MachineConfig)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}

	ignitionConfig, err := makeIgnitionConfig(ver, withCRIHooks)
	if err != nil {
		return nil, err
	}

	mc.Spec.Config = runtime.RawExtension{Raw: ignitionConfig}
	return mc, nil
}

func NetworkPolicy(policyType, namespace string) (*networkingv1.NetworkPolicy, error) {
	var fileName string

	if policyType == "" || policyType == DefaultNetworkPolicy {
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

func makeIgnitionConfig(ver platform.Version, withCRIHooks bool) ([]byte, error) {
	var files []igntypes.File

	if withCRIHooks {
		// load RTE notifier OCI hook config
		notifierHookConfigContent, err := getTemplateContent(rteassets.HookConfigRTENotifier, map[string]string{
			templateNotifierBinaryDst: filepath.Join(defaultScriptsDir, rteassets.NotifierScriptName),
			templateNotifierFilePath:  filepath.Join(rteassets.HostNotifierDir, rteassets.NotifierFileName),
		})
		if err != nil {
			return nil, err
		}
		files = addFileToIgnitionConfig(
			files,
			notifierHookConfigContent,
			0644,
			filepath.Join(defaultOCIHooksDir, rteassets.NotifierOCIHookConfig),
		)

		// load RTE notifier script
		files = addFileToIgnitionConfig(
			files,
			rteassets.NotifierScript,
			0755,
			filepath.Join(defaultScriptsDir, rteassets.NotifierScriptName),
		)
	}

	// we always need the SELinux policy
	selinuxPolicy, err := selinuxassets.GetPolicy(ver)
	if err != nil {
		return nil, err
	}

	files = addFileToIgnitionConfig(files, selinuxPolicy, 0644, selinuxassets.RTEPolicyFileName)

	// and while we (always) need the SELinuc policy, we also need to make sure it's installed
	systemdServiceContent, err := getTemplateContent(
		selinuxassets.InstallSystemdServiceTemplate,
		map[string]string{
			templateSELinuxPolicyDst: selinuxassets.RTEPolicyFileName,
		},
	)
	if err != nil {
		return nil, err
	}

	ignitionConfig := &igntypes.Config{
		Ignition: igntypes.Ignition{
			Version: defaultIgnitionVersion,
		},
		Storage: igntypes.Storage{Files: files},
		Systemd: igntypes.Systemd{
			Units: []igntypes.Unit{
				{
					Contents: pointer.String(string(systemdServiceContent)),
					Enabled:  pointer.Bool(true),
					Name:     selinuxassets.RTEPolicyInstallServiceName,
				},
			},
		},
	}

	return json.Marshal(ignitionConfig)
}

func addFileToIgnitionConfig(files []igntypes.File, fileContent []byte, mode int, fileDst string) []igntypes.File {
	base64FileContent := base64.StdEncoding.EncodeToString(fileContent)
	files = append(files, igntypes.File{
		Node: igntypes.Node{
			Path: fileDst,
		},
		FileEmbedded1: igntypes.FileEmbedded1{
			Contents: igntypes.Resource{
				Source: pointer.String(fmt.Sprintf("%s,%s", defaultIgnitionContentSource, base64FileContent)),
			},
			Mode: pointer.Int(mode),
		},
	})

	return files
}

// getTemplateContent returns the content of the template after the parsing.

func getTemplateContent(templateContent []byte, templateArgs map[string]string) ([]byte, error) {
	fileContent := &bytes.Buffer{}
	newTemplate, err := template.New("template").Parse(string(templateContent))
	if err != nil {
		return nil, err
	}

	if err := newTemplate.Execute(fileContent, templateArgs); err != nil {
		return nil, err
	}

	return fileContent.Bytes(), nil
}

func SecurityContextConstraint(withCustomSELinuxPolicy bool) (*securityv1.SecurityContextConstraints, error) {
	obj, err := loadObject(filepath.Join("yaml", "securitycontextconstraint.yaml"))
	if err != nil {
		return nil, err
	}

	scc, ok := obj.(*securityv1.SecurityContextConstraints)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}

	scc.SELinuxContext = securityv1.SELinuxContextStrategyOptions{
		Type: securityv1.SELinuxStrategyMustRunAs,
		SELinuxOptions: &corev1.SELinuxOptions{
			Type:  selinuxassets.RTEContextType,
			Level: selinuxassets.RTEContextLevel,
		},
	}
	if withCustomSELinuxPolicy {
		scc.SELinuxContext.SELinuxOptions.Type = selinuxassets.RTEContextTypeLegacy
	}

	return scc, nil
}

func SecurityContextConstraintV2() (*securityv1.SecurityContextConstraints, error) {
	obj, err := loadObject(filepath.Join("yaml", "securitycontextconstraintv2.yaml"))
	if err != nil {
		return nil, err
	}

	scc, ok := obj.(*securityv1.SecurityContextConstraints)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}

	return scc, nil
}
