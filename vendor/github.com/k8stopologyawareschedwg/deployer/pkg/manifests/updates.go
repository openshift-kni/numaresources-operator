package manifests

import (
	"fmt"
	"strconv"

	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/images"
)

const (
	metricsPort        = 2112
	rteConfigMountName = "rte-config-volume"
	RTEConfigMapName   = "rte-config"
)

func UpdateRoleBinding(rb *rbacv1.RoleBinding, serviceAccount, namespace string) {
	for idx := 0; idx < len(rb.Subjects); idx++ {
		if serviceAccount != "" {
			rb.Subjects[idx].Name = serviceAccount
		}
		rb.Subjects[idx].Namespace = namespace
	}
}

func UpdateClusterRoleBinding(crb *rbacv1.ClusterRoleBinding, serviceAccount, namespace string) {
	for idx := 0; idx < len(crb.Subjects); idx++ {
		if serviceAccount != "" {
			crb.Subjects[idx].Name = serviceAccount
		}
		crb.Subjects[idx].Namespace = namespace
	}
}

func UpdateSchedulerPluginSchedulerDeployment(dp *appsv1.Deployment, pullIfNotPresent bool) {
	dp.Spec.Template.Spec.Containers[0].Image = images.SchedulerPluginSchedulerImage
	dp.Spec.Template.Spec.Containers[0].ImagePullPolicy = pullPolicy(pullIfNotPresent)
}

func UpdateSchedulerPluginControllerDeployment(dp *appsv1.Deployment, pullIfNotPresent bool) {
	dp.Spec.Template.Spec.Containers[0].Image = images.SchedulerPluginControllerImage
	dp.Spec.Template.Spec.Containers[0].ImagePullPolicy = pullPolicy(pullIfNotPresent)
}

func UpdateResourceTopologyExporterContainerConfig(podSpec *corev1.PodSpec, cnt *corev1.Container, configMapName string) {
	cnt.VolumeMounts = append(cnt.VolumeMounts,
		corev1.VolumeMount{
			Name:      rteConfigMountName,
			MountPath: "/etc/resource-topology-exporter/",
		},
	)
	podSpec.Volumes = append(podSpec.Volumes,
		corev1.Volume{
			Name: rteConfigMountName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
					Optional: newBool(true),
				},
			},
		},
	)
}

func UpdateResourceTopologyExporterDaemonSet(ds *appsv1.DaemonSet, configMapName string, pullIfNotPresent bool, nodeSelector *metav1.LabelSelector) {
	for i := range ds.Spec.Template.Spec.Containers {
		c := &ds.Spec.Template.Spec.Containers[i]
		if c.Name != containerNameRTE {
			continue
		}
		c.ImagePullPolicy = pullPolicy(pullIfNotPresent)

		if configMapName != "" {
			UpdateResourceTopologyExporterContainerConfig(&ds.Spec.Template.Spec, c, configMapName)
		}
	}

	if nodeSelector != nil {
		ds.Spec.Template.Spec.NodeSelector = nodeSelector.MatchLabels
	}
	UpdateMetricsPort(ds, metricsPort)
}

func UpdateNFDTopologyUpdaterDaemonSet(ds *appsv1.DaemonSet, pullIfNotPresent bool, nodeSelector *metav1.LabelSelector) {
	for i := range ds.Spec.Template.Spec.Containers {
		c := &ds.Spec.Template.Spec.Containers[i]
		if c.Name != containerNameNFDTopologyUpdater {
			continue
		}
		c.ImagePullPolicy = pullPolicy(pullIfNotPresent)
		c.Image = images.NodeFeatureDiscoveryImage

	}
	if nodeSelector != nil {
		ds.Spec.Template.Spec.NodeSelector = nodeSelector.MatchLabels
	}
}

func UpdateNFDMasterDeployment(ds *appsv1.Deployment, pullIfNotPresent bool) {
	for i := range ds.Spec.Template.Spec.Containers {
		c := &ds.Spec.Template.Spec.Containers[i]
		if c.Name != containerNameNFDMaster {
			continue
		}
		c.ImagePullPolicy = pullPolicy(pullIfNotPresent)
		c.Image = images.NodeFeatureDiscoveryImage
	}
}

func UpdateMachineConfig(mc *machineconfigv1.MachineConfig, name string, mcpSelector *metav1.LabelSelector) {
	if name != "" {
		mc.Name = fmt.Sprintf("51-%s", name)
	}

	if mcpSelector != nil {
		mc.Labels = mcpSelector.MatchLabels
	}
}

func UpdateSecurityContextConstraint(scc *securityv1.SecurityContextConstraints, sa *corev1.ServiceAccount) {
	serviceAccountName := fmt.Sprintf("system:serviceaccount:%s:%s", sa.Namespace, sa.Name)
	for _, u := range scc.Users {
		// Service account user already exists under scc users
		if u == serviceAccountName {
			return
		}
	}
	scc.Users = append(scc.Users, serviceAccountName)
}

func UpdateMetricsPort(ds *appsv1.DaemonSet, pNum int) {
	pNumAsStr := strconv.Itoa(pNum)

	for idx, env := range ds.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "METRICS_PORT" {
			ds.Spec.Template.Spec.Containers[0].Env[idx].Value = pNumAsStr
		}
	}

	cp := []corev1.ContainerPort{{
		Name:          "metrics-port",
		ContainerPort: int32(pNum),
	},
	}
	ds.Spec.Template.Spec.Containers[0].Ports = cp
}

func pullPolicy(pullIfNotPresent bool) corev1.PullPolicy {
	if pullIfNotPresent {
		return corev1.PullIfNotPresent
	}
	return corev1.PullAlways
}

func newBool(val bool) *bool {
	return &val
}
