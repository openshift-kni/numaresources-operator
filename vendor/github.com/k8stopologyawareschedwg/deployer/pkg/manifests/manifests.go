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
	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	kubeschedulerconfigv1beta2 "k8s.io/kube-scheduler/config/v1beta2"
	"k8s.io/utils/pointer"
	apiconfigv1beta2 "sigs.k8s.io/scheduler-plugins/apis/config/v1beta2"

	k8sschedpluginsconf "sigs.k8s.io/scheduler-plugins/apis/config"
	k8sschedpluginsconfv1beta2 "sigs.k8s.io/scheduler-plugins/apis/config/v1beta2"
	k8sschedpluginsconfv1beta3 "sigs.k8s.io/scheduler-plugins/apis/config/v1beta3"

	rteassets "github.com/k8stopologyawareschedwg/deployer/pkg/assets/rte"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/images"
)

const (
	ComponentAPI                      = "api"
	ComponentSchedulerPlugin          = "sched"
	ComponentResourceTopologyExporter = "rte"
	ComponentNodeFeatureDiscovery     = "nfd"
)

const (
	SubComponentSchedulerPluginScheduler            = "scheduler"
	SubComponentSchedulerPluginController           = "controller"
	SubComponentNodeFeatureDiscoveryTopologyUpdater = "topologyupdater"
)

const (
	defaultIgnitionVersion       = "3.2.0"
	defaultIgnitionContentSource = "data:text/plain;charset=utf-8;base64"
	defaultOCIHooksDir           = "/etc/containers/oci/hooks.d"
	defaultScriptsDir            = "/usr/local/bin"
	seLinuxRTEPolicyDst          = "/etc/selinux/rte.cil"
	seLinuxRTEContextType        = "rte.process"
	seLinuxRTEContextLevel       = "s0"
	templateSELinuxPolicyDst     = "selinuxPolicyDst"
	templateNotifierBinaryDst    = "notifierScriptPath"
	templateNotifierFilePath     = "notifierFilePath"
)

const (
	containerNameRTE                = "resource-topology-exporter"
	containerNameNFDTopologyUpdater = "nfd-topology-updater"
	rteNotifierVolumeName           = "host-run-rte"
	rteSysVolumeName                = "host-sys"
	rtePodresourcesSocketVolumeName = "host-podresources-socket"
	rteKubeletDirVolumeName         = "host-var-lib-kubelet"
	rteNotifierFileName             = "notify"
	hostNotifierDir                 = "/run/rte"
)

//go:embed yaml
var src embed.FS

func init() {
	apiextensionv1.AddToScheme(scheme.Scheme)
	apiconfigv1beta2.AddToScheme(scheme.Scheme)
	kubeschedulerconfigv1beta2.AddToScheme(scheme.Scheme)
	k8sschedpluginsconf.AddToScheme(scheme.Scheme)
	k8sschedpluginsconfv1beta2.AddToScheme(scheme.Scheme)
	k8sschedpluginsconfv1beta3.AddToScheme(scheme.Scheme)
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

func ServiceAccount(component, subComponent, namespace string) (*corev1.ServiceAccount, error) {
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

	if namespace != "" {
		sa.Namespace = namespace
	}
	return sa, nil
}

func Role(component, subComponent, namespace string) (*rbacv1.Role, error) {
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

	if namespace != "" {
		role.Namespace = namespace
	}
	return role, nil
}

func RoleBinding(component, subComponent, namespace string) (*rbacv1.RoleBinding, error) {
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

	if namespace != "" {
		rb.Namespace = namespace
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

func Deployment(component, subComponent, namespace string) (*appsv1.Deployment, error) {
	if err := validateComponent(component); err != nil {
		return nil, err
	}
	if err := validateSubComponent(component, subComponent); err != nil {
		return nil, err
	}
	obj, err := loadObject(filepath.Join("yaml", component, subComponent, "deployment.yaml"))
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

func DaemonSet(component, subComponent string, plat platform.Platform, namespace string) (*appsv1.DaemonSet, error) {
	if err := validateComponent(component); err != nil {
		return nil, err
	}
	if err := validateSubComponent(component, subComponent); err != nil {
		return nil, err
	}
	obj, err := loadObject(filepath.Join("yaml", component, subComponent, "daemonset.yaml"))
	if err != nil {
		return nil, err
	}

	ds, ok := obj.(*appsv1.DaemonSet)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}

	if component == ComponentResourceTopologyExporter {
		hostPathDirectory := corev1.HostPathDirectory
		hostPathSocket := corev1.HostPathSocket
		hostPathDirectoryOrCreate := corev1.HostPathDirectoryOrCreate
		ds.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				// needed to get the CPU, PCI devices and memory information
				Name: rteSysVolumeName,
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/sys",
						Type: &hostPathDirectory,
					},
				},
			},
			{
				Name: rtePodresourcesSocketVolumeName,
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/var/lib/kubelet/pod-resources/kubelet.sock",
						Type: &hostPathSocket,
					},
				},
			},
			{
				// notifier file volume
				Name: rteNotifierVolumeName,
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: hostNotifierDir,
						Type: &hostPathDirectoryOrCreate,
					},
				},
			},
		}

		containerPodResourcesSocket := filepath.Join("/", rtePodresourcesSocketVolumeName, "kubelet.sock")
		containerHostSysDir := filepath.Join("/", rteSysVolumeName)
		rteContainerVolumeMounts := []corev1.VolumeMount{
			{
				Name:      rteSysVolumeName,
				ReadOnly:  true,
				MountPath: containerHostSysDir,
			},
			{
				Name:      rtePodresourcesSocketVolumeName,
				MountPath: containerPodResourcesSocket,
			},
			{
				Name:      rteNotifierVolumeName,
				MountPath: filepath.Join("/", rteNotifierVolumeName),
			},
		}
		if plat == platform.Kubernetes {
			ds.Spec.Template.Spec.Volumes = append(ds.Spec.Template.Spec.Volumes,
				corev1.Volume{
					Name: rteKubeletDirVolumeName,
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/var/lib/kubelet",
							Type: &hostPathDirectory,
						},
					},
				},
			)
			rteContainerVolumeMounts = append(rteContainerVolumeMounts, corev1.VolumeMount{
				Name:      rteKubeletDirVolumeName,
				ReadOnly:  true,
				MountPath: filepath.Join("/", rteKubeletDirVolumeName),
			})
		}

		for i := range ds.Spec.Template.Spec.Containers {
			c := &ds.Spec.Template.Spec.Containers[i]
			if c.Name == containerNameRTE {
				c.Image = images.ResourceTopologyExporterImage
				// we do this explicitely, but should be already OK from the YAML manifest
				c.Command = []string{
					"/bin/resource-topology-exporter",
				}
				c.Args = []string{
					"--sleep-interval=10s",
					fmt.Sprintf("--sysfs=%s", containerHostSysDir),
					fmt.Sprintf("--podresources-socket=unix://%s", containerPodResourcesSocket),
					fmt.Sprintf("--notify-file=/%s/%s", rteNotifierVolumeName, rteNotifierFileName),
				}

				if plat == platform.OpenShift {
					// this is needed to put watches in the kubelet state dirs AND
					// to open the podresources socket in R/W mode
					if c.SecurityContext == nil {
						c.SecurityContext = &corev1.SecurityContext{}
					}
					c.SecurityContext.SELinuxOptions = &corev1.SELinuxOptions{
						Type:  seLinuxRTEContextType,
						Level: seLinuxRTEContextLevel,
					}
				}

				if plat == platform.Kubernetes {
					c.Args = append(
						c.Args,
						fmt.Sprintf("--kubelet-config-file=/%s/config.yaml", rteKubeletDirVolumeName),
						fmt.Sprintf("--kubelet-state-dir=/%s", rteKubeletDirVolumeName),
					)
				}

				c.VolumeMounts = rteContainerVolumeMounts
			}
		}
	}

	ds.Namespace = namespace
	return ds, nil
}

func MachineConfig(component string, ver platform.Version) (*machineconfigv1.MachineConfig, error) {
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

	ignitionConfig, err := getIgnitionConfig(ver)
	if err != nil {
		return nil, err
	}

	mc.Spec.Config = runtime.RawExtension{Raw: ignitionConfig}
	return mc, nil
}

func getIgnitionConfig(ver platform.Version) ([]byte, error) {
	var files []igntypes.File

	// get SELinux policy
	selinuxPolicy, err := rteassets.GetSELinuxPolicy(ver)
	if err != nil {
		return nil, err
	}

	// load SELinux policy
	files = addFileToIgnitionConfig(files, selinuxPolicy, 0644, seLinuxRTEPolicyDst)

	// load RTE notifier OCI hook config
	notifierHookConfigContent, err := getTemplateContent(rteassets.HookConfigRTENotifier, map[string]string{
		templateNotifierBinaryDst: filepath.Join(defaultScriptsDir, "rte-notifier.sh"),
		templateNotifierFilePath:  filepath.Join(hostNotifierDir, rteNotifierFileName),
	})
	if err != nil {
		return nil, err
	}
	files = addFileToIgnitionConfig(
		files,
		notifierHookConfigContent,
		0644,
		filepath.Join(defaultOCIHooksDir, "rte-notifier.json"),
	)

	// load RTE notifier script
	files = addFileToIgnitionConfig(
		files,
		rteassets.NotifierScript,
		0755,
		filepath.Join(defaultScriptsDir, "rte-notifier.sh"),
	)

	// load systemd service to install SELinux policy
	systemdServiceContent, err := getTemplateContent(
		rteassets.SELinuxInstallSystemdServiceTemplate,
		map[string]string{
			templateSELinuxPolicyDst: seLinuxRTEPolicyDst,
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
					Contents: pointer.StringPtr(string(systemdServiceContent)),
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

func addFileToIgnitionConfig(files []igntypes.File, fileContent []byte, mode int, fileDst string) []igntypes.File {
	base64FileContent := base64.StdEncoding.EncodeToString(fileContent)
	files = append(files, igntypes.File{
		Node: igntypes.Node{
			Path: fileDst,
		},
		FileEmbedded1: igntypes.FileEmbedded1{
			Contents: igntypes.Resource{
				Source: pointer.StringPtr(fmt.Sprintf("%s,%s", defaultIgnitionContentSource, base64FileContent)),
			},
			Mode: pointer.IntPtr(mode),
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

func KubeSchedulerConfigurationFromData(data []byte) (*kubeschedulerconfigv1beta2.KubeSchedulerConfiguration, error) {
	obj, err := DeserializeObjectFromData(data)
	if err != nil {
		return nil, err
	}

	sc, ok := obj.(*kubeschedulerconfigv1beta2.KubeSchedulerConfiguration)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %T %v", obj, obj.GetObjectKind())
	}
	return sc, nil
}

func KubeSchedulerConfigurationToData(sc *kubeschedulerconfigv1beta2.KubeSchedulerConfiguration) ([]byte, error) {
	return SerializeObjectToData(sc)
}

func validateComponent(component string) error {
	if component == ComponentAPI || component == ComponentResourceTopologyExporter || component == ComponentNodeFeatureDiscovery || component == ComponentSchedulerPlugin {
		return nil
	}
	return fmt.Errorf("unknown component: %q", component)
}

func validateSubComponent(component, subComponent string) error {
	if subComponent == "" {
		return nil
	}
	if component == ComponentSchedulerPlugin && (subComponent == SubComponentSchedulerPluginController || subComponent == SubComponentSchedulerPluginScheduler) {
		return nil
	}
	if component == ComponentNodeFeatureDiscovery && (subComponent == SubComponentNodeFeatureDiscoveryTopologyUpdater) {
		return nil
	}
	return fmt.Errorf("unknown subComponent %q for component: %q", subComponent, component)
}

func Service(component, subComponent, namespace string) (*corev1.Service, error) {
	if err := validateComponent(component); err != nil {
		return nil, err
	}
	if err := validateSubComponent(component, subComponent); err != nil {
		return nil, err
	}

	obj, err := loadObject(filepath.Join("yaml", component, subComponent, "service.yaml"))
	if err != nil {
		return nil, err
	}

	sv, ok := obj.(*corev1.Service)
	if !ok {
		return nil, fmt.Errorf("unexpected type, got %t", obj)
	}

	if namespace != "" {
		sv.Namespace = namespace
	}
	return sv, nil
}
