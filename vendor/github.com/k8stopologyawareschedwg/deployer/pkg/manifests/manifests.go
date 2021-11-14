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
	"io"
	"path/filepath"
	"text/template"

	igntypes "github.com/coreos/ignition/v2/config/v3_2/types"
	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"
	kubeschedulerconfigv1beta1 "k8s.io/kube-scheduler/config/v1beta1"
	"k8s.io/utils/pointer"
	apiconfig "sigs.k8s.io/scheduler-plugins/pkg/apis/config"

	rteassets "github.com/k8stopologyawareschedwg/deployer/pkg/assets/rte"
)

const (
	ComponentAPI                      = "api"
	ComponentSchedulerPlugin          = "sched"
	ComponentResourceTopologyExporter = "rte"
)

const (
	SubComponentSchedulerPluginScheduler  = "scheduler"
	SubComponentSchedulerPluginController = "controller"
)

//go:embed yaml
var src embed.FS

func init() {
	apiextensionv1.AddToScheme(scheme.Scheme)
	apiconfig.AddToScheme(scheme.Scheme)
	kubeschedulerconfigv1beta1.AddToScheme(scheme.Scheme)
	machineconfigv1.Install(scheme.Scheme)
	securityv1.Install(scheme.Scheme)
}

func Namespace(component string) (*corev1.Namespace, error) {
	if err := validateComponent(component); err != nil {
		return nil, err
	}

	obj, err := loadObject(filepath.Join("yaml", component, "namespace.yaml"))
	if err != nil {
		return nil, err
	}

	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return ns, nil
}

func ServiceAccount(component, subComponent string) (*corev1.ServiceAccount, error) {
	if err := validateComponent(component); err != nil {
		return nil, err
	}
	if err := validateSubComponent(component, subComponent); err != nil {
		return nil, err
	}

	obj, err := loadObject(filepath.Join("yaml", component, subComponent, "serviceaccount.yaml"))
	if err != nil {
		return nil, err
	}

	sa, ok := obj.(*corev1.ServiceAccount)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return sa, nil
}

func Role(component, subComponent string) (*rbacv1.Role, error) {
	if err := validateComponent(component); err != nil {
		return nil, err
	}
	if err := validateSubComponent(component, subComponent); err != nil {
		return nil, err
	}

	obj, err := loadObject(filepath.Join("yaml", component, subComponent, "role.yaml"))
	if err != nil {
		return nil, err
	}

	role, ok := obj.(*rbacv1.Role)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return role, nil
}

func RoleBinding(component, subComponent string) (*rbacv1.RoleBinding, error) {
	if err := validateComponent(component); err != nil {
		return nil, err
	}
	if err := validateSubComponent(component, subComponent); err != nil {
		return nil, err
	}

	obj, err := loadObject(filepath.Join("yaml", component, subComponent, "rolebinding.yaml"))
	if err != nil {
		return nil, err
	}

	rb, ok := obj.(*rbacv1.RoleBinding)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return rb, nil
}

func ClusterRole(component, subComponent string) (*rbacv1.ClusterRole, error) {
	if err := validateComponent(component); err != nil {
		return nil, err
	}
	if err := validateSubComponent(component, subComponent); err != nil {
		return nil, err
	}

	obj, err := loadObject(filepath.Join("yaml", component, subComponent, "clusterrole.yaml"))
	if err != nil {
		return nil, err
	}

	cr, ok := obj.(*rbacv1.ClusterRole)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return cr, nil
}

func ClusterRoleBinding(component, subComponent string) (*rbacv1.ClusterRoleBinding, error) {
	if err := validateComponent(component); err != nil {
		return nil, err
	}
	if err := validateSubComponent(component, subComponent); err != nil {
		return nil, err
	}

	obj, err := loadObject(filepath.Join("yaml", component, subComponent, "clusterrolebinding.yaml"))
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
	obj, err := loadObject("yaml/api/crd.yaml")
	if err != nil {
		return nil, err
	}

	crd, ok := obj.(*apiextensionv1.CustomResourceDefinition)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return crd, nil
}

func SchedulerCRD() (*apiextensionv1.CustomResourceDefinition, error) {
	obj, err := loadObject("yaml/sched/podgroup.crd.yaml")
	if err != nil {
		return nil, err
	}

	crd, ok := obj.(*apiextensionv1.CustomResourceDefinition)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return crd, nil
}

func ConfigMap(component, subComponent string) (*corev1.ConfigMap, error) {
	if err := validateComponent(component); err != nil {
		return nil, err
	}
	if err := validateSubComponent(component, subComponent); err != nil {
		return nil, err
	}
	obj, err := loadObject(filepath.Join("yaml", component, subComponent, "configmap.yaml"))
	if err != nil {
		return nil, err
	}

	crd, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return crd, nil
}

func Deployment(component, subComponent string) (*appsv1.Deployment, error) {
	if err := validateComponent(component); err != nil {
		return nil, err
	}
	if err := validateSubComponent(component, subComponent); err != nil {
		return nil, err
	}
	obj, err := loadObject(filepath.Join("yaml", "sched", subComponent, "deployment.yaml"))
	if err != nil {
		return nil, err
	}

	dp, ok := obj.(*appsv1.Deployment)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return dp, nil
}

func DaemonSet(component string) (*appsv1.DaemonSet, error) {
	if err := validateComponent(component); err != nil {
		return nil, err
	}
	obj, err := loadObject(filepath.Join("yaml", component, "daemonset.yaml"))
	if err != nil {
		return nil, err
	}

	ds, ok := obj.(*appsv1.DaemonSet)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}
	return ds, nil
}

func MachineConfig(component string) (*machineconfigv1.MachineConfig, error) {
	if component != ComponentResourceTopologyExporter {
		return nil, fmt.Errorf("component %q is not an %q component", component, ComponentResourceTopologyExporter)
	}

	obj, err := loadObject(filepath.Join("yaml", component, "machineconfig.yaml"))
	if err != nil {
		return nil, err
	}

	mc, ok := obj.(*machineconfigv1.MachineConfig)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}

	ignitionConfig, err := getIgnitionConfig()
	if err != nil {
		return nil, err
	}

	mc.Spec.Config = runtime.RawExtension{Raw: ignitionConfig}
	return mc, nil
}

func getIgnitionConfig() ([]byte, error) {
	// load SELinux policy
	seLinuxPolicyContent := base64.StdEncoding.EncodeToString(rteassets.SELinuxPolicy)

	// load systemd service to install SELinux policy
	systemdServiceContent, err := getSELinuxInstallSystemdServiceContent()
	if err != nil {
		return nil, err
	}

	ignitionConfig := &igntypes.Config{
		Ignition: igntypes.Ignition{
			Version: defaultIgnitionVersion,
		},
		Storage: igntypes.Storage{
			Files: []igntypes.File{
				{
					Node: igntypes.Node{
						Path: seLinuxRTEPolicyDst,
					},
					FileEmbedded1: igntypes.FileEmbedded1{
						Contents: igntypes.Resource{
							Source: pointer.StringPtr(fmt.Sprintf("%s,%s", defaultIgnitionContentSource, seLinuxPolicyContent)),
						},
						Mode: pointer.IntPtr(644),
					},
				},
			},
		},
		Systemd: igntypes.Systemd{
			Units: []igntypes.Unit{
				{
					Contents: systemdServiceContent,
					Enabled:  pointer.BoolPtr(true),
					Name:     "rte-selinux-policy-install.service",
				},
			},
		},
	}

	rawIgnition, err := json.Marshal(ignitionConfig)
	if err != nil {
		return nil, err
	}

	return rawIgnition, nil
}

// getSELinuxInstallSystemdServiceContent returns the content of the systemd service that installs the SELinux policy.
// It returns the string pointer, because the ignition config is expecting to get the string pointer.
func getSELinuxInstallSystemdServiceContent() (*string, error) {
	// load systemd service to install SELinux policy
	templateArgs := map[string]string{templateSELinuxPolicyDst: seLinuxRTEPolicyDst}
	systemdServiceContent := &bytes.Buffer{}
	systemdServiceTemplate, err := template.New("selinuxinstall.service").Parse(string(rteassets.SELinuxInstallSystemdServiceTemplate))
	if err != nil {
		return nil, err
	}

	if err := systemdServiceTemplate.Execute(systemdServiceContent, templateArgs); err != nil {
		return nil, err
	}

	return pointer.StringPtr(systemdServiceContent.String()), nil
}

func SecurityContextConstraint(component string) (*securityv1.SecurityContextConstraints, error) {
	if component != ComponentResourceTopologyExporter {
		return nil, fmt.Errorf("component %q is not an %q component", component, ComponentResourceTopologyExporter)
	}

	obj, err := loadObject(filepath.Join("yaml", component, "securitycontextconstraint.yaml"))
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
			Type:  seLinuxRTEContextType,
			Level: seLinuxRTEContextLevel,
		},
	}

	return scc, nil
}

func KubeSchedulerConfigurationFromData(data []byte) (*kubeschedulerconfigv1beta1.KubeSchedulerConfiguration, error) {
	obj, err := deserializeObjectFromData(data)
	if err != nil {
		return nil, err
	}

	sc, ok := obj.(*kubeschedulerconfigv1beta1.KubeSchedulerConfiguration)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %T %v", obj, obj.GetObjectKind())
	}
	return sc, nil
}

func KubeSchedulerConfigurationToData(sc *kubeschedulerconfigv1beta1.KubeSchedulerConfiguration) ([]byte, error) {
	var buf bytes.Buffer
	err := SerializeObject(sc, &buf)
	return buf.Bytes(), err
}

func NodeResourceTopologyMatchArgsFromData(data []byte) (*apiconfig.NodeResourceTopologyMatchArgs, error) {
	sc := apiconfig.NodeResourceTopologyMatchArgs{}
	err := json.Unmarshal(data, &sc)
	return &sc, err
}

// helper type to marshal the right names (forcing lowercase)
type nodeResourceTopologyMatchArgs struct {
	KubeConfigPath string   `json:"kubeconfigpath"`
	MasterOverride string   `json:"masteroverride"`
	Namespaces     []string `json:"namespaces"`
}

func NodeResourceTopologyMatchArgsToData(ma *apiconfig.NodeResourceTopologyMatchArgs) ([]byte, error) {
	cfg := nodeResourceTopologyMatchArgs{
		KubeConfigPath: ma.KubeConfigPath,
		MasterOverride: ma.MasterOverride,
		Namespaces:     ma.Namespaces,
	}
	return json.Marshal(cfg)
}

func SerializeObject(obj runtime.Object, out io.Writer) error {
	srz := k8sjson.NewYAMLSerializer(k8sjson.DefaultMetaFactory, scheme.Scheme, scheme.Scheme)
	return srz.Encode(obj, out)
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

func validateComponent(component string) error {
	if component == "api" || component == "rte" || component == "sched" {
		return nil
	}
	return fmt.Errorf("unknown component: %q", component)
}

func validateSubComponent(component, subComponent string) error {
	if subComponent == "" {
		return nil
	}
	if component == "sched" && (subComponent == "controller" || subComponent == "scheduler") {
		return nil
	}
	return fmt.Errorf("unknown subComponent %q for component: %q", subComponent, component)
}
