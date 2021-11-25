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
	metricsPort = 2112
)

func UpdateRoleBinding(rb *rbacv1.RoleBinding, serviceAccount, namespace string) *rbacv1.RoleBinding {
	for idx := 0; idx < len(rb.Subjects); idx++ {
		if serviceAccount != "" {
			rb.Subjects[idx].Name = serviceAccount
		}
		rb.Subjects[idx].Namespace = namespace
	}
	return rb
}

func UpdateClusterRoleBinding(crb *rbacv1.ClusterRoleBinding, serviceAccount, namespace string) *rbacv1.ClusterRoleBinding {
	for idx := 0; idx < len(crb.Subjects); idx++ {
		if serviceAccount != "" {
			crb.Subjects[idx].Name = serviceAccount
		}
		crb.Subjects[idx].Namespace = namespace
	}
	return crb
}

func UpdateSchedulerPluginSchedulerDeployment(dp *appsv1.Deployment, pullIfNotPresent bool) *appsv1.Deployment {
	dp.Spec.Template.Spec.Containers[0].Image = images.SchedulerPluginSchedulerImage
	dp.Spec.Template.Spec.Containers[0].ImagePullPolicy = pullPolicy(pullIfNotPresent)
	return dp
}

func UpdateSchedulerPluginControllerDeployment(dp *appsv1.Deployment, pullIfNotPresent bool) *appsv1.Deployment {
	dp.Spec.Template.Spec.Containers[0].Image = images.SchedulerPluginControllerImage
	dp.Spec.Template.Spec.Containers[0].ImagePullPolicy = pullPolicy(pullIfNotPresent)
	return dp
}

func UpdateResourceTopologyExporterDaemonSet(ds *appsv1.DaemonSet, cm *corev1.ConfigMap, pullIfNotPresent bool, nodeSelector *metav1.LabelSelector) {
	for i := range ds.Spec.Template.Spec.Containers {
		c := &ds.Spec.Template.Spec.Containers[i]
		if c.Name == containerNameRTE {
			if cm != nil {
				c.VolumeMounts = append(c.VolumeMounts,
					corev1.VolumeMount{
						Name:      "rte-config",
						MountPath: "/etc/resource-topology-exporter/",
					},
				)
				ds.Spec.Template.Spec.Volumes = append(ds.Spec.Template.Spec.Volumes,
					corev1.Volume{
						Name: "rte-config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "rte-config",
								},
								Optional: newBool(true),
							},
						},
					},
				)
			}
		}

		c.ImagePullPolicy = pullPolicy(pullIfNotPresent)
	}

	if nodeSelector != nil {
		ds.Spec.Template.Spec.NodeSelector = nodeSelector.MatchLabels
	}
	UpdateMetricsPort(ds, metricsPort)
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
