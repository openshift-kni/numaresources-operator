package manifests

import (
	"fmt"
	"strconv"
	"time"

	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	pluginconfig "sigs.k8s.io/scheduler-plugins/apis/config"

	"github.com/k8stopologyawareschedwg/deployer/pkg/images"
)

const (
	metricsPort             = 2112
	rteConfigMountName      = "rte-config-volume"
	RTEConfigMapName        = "rte-config"
	SchedulerConfigFileName = "scheduler-config.yaml" // TODO duplicate from yaml
	schedulerPluginName     = "NodeResourceTopologyMatch"
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

func UpdateSchedulerConfig(cm *corev1.ConfigMap, schedulerName string, cacheResyncPeriod time.Duration) error {
	if cm.Data == nil {
		return fmt.Errorf("no data found in ConfigMap: %s/%s", cm.Namespace, cm.Name)
	}

	data, ok := cm.Data[SchedulerConfigFileName]
	if !ok {
		return fmt.Errorf("no data key named: %s found in ConfigMap: %s/%s", SchedulerConfigFileName, cm.Namespace, cm.Name)
	}

	newData, err := RenderSchedulerConfig(data, schedulerName, cacheResyncPeriod)
	if err != nil {
		return err
	}

	cm.Data[SchedulerConfigFileName] = string(newData)
	return nil
}

func RenderSchedulerConfig(data, schedulerName string, cacheResyncPeriod time.Duration) (string, error) {
	schedCfg, err := DecodeSchedulerConfigFromData([]byte(data))
	if err != nil {
		return data, err
	}

	schedProf, pluginConf := findKubeSchedulerProfileByName(schedCfg, schedulerPluginName)
	if schedProf == nil || pluginConf == nil {
		return data, fmt.Errorf("no profile or plugin configuration found for %q", schedulerPluginName)
	}

	if schedulerName != "" {
		schedProf.SchedulerName = schedulerName
	}

	confObj := pluginConf.Args.DeepCopyObject()
	cfg, ok := confObj.(*pluginconfig.NodeResourceTopologyMatchArgs)
	if !ok {
		return data, fmt.Errorf("unsupported plugin config type: %T", confObj)
	}

	period := int64(cacheResyncPeriod.Seconds())
	cfg.CacheResyncPeriodSeconds = period

	pluginConf.Args = cfg

	newData, err := EncodeSchedulerConfigToData(schedCfg)
	return string(newData), err
}

func findKubeSchedulerProfileByName(sc *schedconfig.KubeSchedulerConfiguration, name string) (*schedconfig.KubeSchedulerProfile, *schedconfig.PluginConfig) {
	for i := range sc.Profiles {
		// if we have a configuration for the NodeResourceTopologyMatch
		// this is a valid profile
		for j := range sc.Profiles[i].PluginConfig {
			if sc.Profiles[i].PluginConfig[j].Name == name {
				return &sc.Profiles[i], &sc.Profiles[i].PluginConfig[j]
			}
		}
	}

	return nil, nil
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
