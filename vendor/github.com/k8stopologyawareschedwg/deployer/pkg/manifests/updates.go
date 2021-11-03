package manifests

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/drone/envsubst"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/images"
	"github.com/k8stopologyawareschedwg/deployer/pkg/tlog"
)

func UpdateRoleBinding(rb *rbacv1.RoleBinding, serviceAccount, namespace string) *rbacv1.RoleBinding {
	rb.Namespace = namespace // TODO
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

func UpdateSchedulerConfigNamespaces(logger tlog.Logger, cm *corev1.ConfigMap, NodeResourcesNamespace string) *corev1.ConfigMap {
	confData, ok := cm.Data["scheduler-config.yaml"]
	if !ok {
		logger.Debugf("missing data for scheduler-config.yaml")
		return cm
	}
	kc, err := KubeSchedulerConfigurationFromData([]byte(confData))
	if err != nil {
		logger.Debugf("cannot decode the KubeSchedulerConfiguration: %v", err)
		return cm
	}

	for idx := 0; idx < len(kc.Profiles[0].PluginConfig); idx++ {
		if kc.Profiles[0].PluginConfig[idx].Name == "NodeResourceTopologyMatch" {
			tcfg, err := NodeResourceTopologyMatchArgsFromData(kc.Profiles[0].PluginConfig[idx].Args.Raw)
			if err != nil {
				logger.Debugf("failed to decode NodeResourceTopologyMatchArgs: %v", err)
				continue
			}

			namespaces := sets.NewString(tcfg.Namespaces...)
			namespaces.Insert(NodeResourcesNamespace)
			tcfg.Namespaces = namespaces.List()
			logger.Debugf("new namespace list: %v", tcfg.Namespaces)

			blob, err := NodeResourceTopologyMatchArgsToData(tcfg)
			if err != nil {
				logger.Debugf("failed to re-encode NodeResourceTopologyMatchArgs: %v", err)
				continue
			}
			kc.Profiles[0].PluginConfig[idx].Args.Raw = blob
		}
	}

	binData, err := KubeSchedulerConfigurationToData(kc)
	if err != nil {
		logger.Debugf("cannot encode the KubeSchedulerConfiguration: %v", err)
		return cm
	}
	cm.Data["scheduler-config.yaml"] = string(binData)
	return cm
}

func UpdateResourceTopologyExporterDaemonSet(plat platform.Platform, ds *appsv1.DaemonSet, cm *corev1.ConfigMap, pullIfNotPresent bool) *appsv1.DaemonSet {
	// TODO: better match by name than assume container#0 is RTE proper (not minion)
	ds.Spec.Template.Spec.Containers[0].Image = images.ResourceTopologyExporterImage
	ds.Spec.Template.Spec.Containers[0].ImagePullPolicy = pullPolicy(pullIfNotPresent)
	if len(ds.Spec.Template.Spec.Containers) > 1 {
		// TODO: more polite/proper iteration
		ds.Spec.Template.Spec.Containers[1].ImagePullPolicy = pullPolicy(pullIfNotPresent)
	}
	vars := map[string]string{
		"RTE_POLL_INTERVAL": "10s",
		"EXPORT_NAMESPACE":  ds.Namespace,
	}
	ds.Spec.Template.Spec.Containers[0].Command = UpdateResourceTopologyExporterCommand(ds.Spec.Template.Spec.Containers[0].Command, vars, plat)
	if plat == platform.OpenShift {
		// this is needed to put watches in the kubelet state dirs AND
		// to open the podresources socket in R/W mode
		if ds.Spec.Template.Spec.Containers[0].SecurityContext == nil {
			ds.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{}
		}
		ds.Spec.Template.Spec.Containers[0].SecurityContext.Privileged = newBool(true)
	}
	if cm != nil {
		ds.Spec.Template.Spec.Containers[0].VolumeMounts = append(ds.Spec.Template.Spec.Containers[0].VolumeMounts,
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
	return ds
}

func UpdateResourceTopologyExporterCommand(args []string, vars map[string]string, plat platform.Platform) []string {
	res := []string{}
	for _, arg := range args {
		newArg, err := envsubst.Eval(arg, func(key string) string { return vars[key] })
		if err != nil {
			// TODO log?
			continue
		}
		res = append(res, newArg)
	}
	if plat == platform.Kubernetes {
		res = append(res, "--kubelet-config-file=/host-var/lib/kubelet/config.yaml")
	}
	if plat == platform.OpenShift {
		// TODO
		res = append(res, "--topology-manager-policy=single-numa-node")
	}
	return res
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
