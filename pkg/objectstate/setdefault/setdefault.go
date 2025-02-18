package setdefault

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"sigs.k8s.io/controller-runtime/pkg/client"

	securityv1 "github.com/openshift/api/security/v1"
	"github.com/openshift/apiserver-library-go/pkg/securitycontextconstraints/sccdefaults"
)

func Service(original, mutated client.Object) client.Object {
	service := mutated.(*corev1.Service)
	defaultServiceSpec(&original.(*corev1.Service).Spec, &service.Spec)
	return service
}

func CRD(original, mutated client.Object) client.Object {
	customResourceDefinition := mutated.(*apiextensionsv1.CustomResourceDefinition)
	defaultCRDSpec(&customResourceDefinition.Spec)
	return customResourceDefinition
}

func Deployment(original, mutated client.Object) client.Object {
	deployment := mutated.(*appsv1.Deployment)
	defaultDeploymentSpec(&original.(*appsv1.Deployment).Spec, &deployment.Spec)
	return deployment
}

func DaemonSet(original, mutated client.Object) client.Object {
	daemonSet := mutated.(*appsv1.DaemonSet)
	defaultDaemonSetSpec(&original.(*appsv1.DaemonSet).Spec, &daemonSet.Spec)
	return daemonSet
}

func ClusterRoleBinding(original, mutated client.Object) client.Object {
	clusterRoleBinding := mutated.(*rbacv1.ClusterRoleBinding)
	for i := range clusterRoleBinding.Subjects {
		subject := &clusterRoleBinding.Subjects[i]
		defaultSubject(subject)
	}
	return clusterRoleBinding
}

func RoleBinding(original, mutated client.Object) client.Object {
	roleBinding := mutated.(*rbacv1.RoleBinding)
	for i := range roleBinding.Subjects {
		subject := &roleBinding.Subjects[i]
		defaultSubject(subject)
	}
	return roleBinding
}

func SecurityContextConstraints(original, mutated client.Object) client.Object {
	securityContextConstraints := mutated.(*securityv1.SecurityContextConstraints)
	sccdefaults.SetDefaults_SCC(securityContextConstraints)
	return securityContextConstraints
}

func None(original, mutated client.Object) client.Object {
	return mutated
}

// Below defaulting funcs. Their code is based on upstream code that is
// not in staging unfortunately so we can't import it:
// * https://github.com/kubernetes/kubernetes/blob/e5976909c6fb129228a67515e0f86336a53884f0/pkg/apis/core/v1/zz_generated.defaults.go
// * https://github.com/kubernetes/kubernetes/blob/e5976909c6fb129228a67515e0f86336a53884f0/pkg/apis/apps/v1/zz_generated.defaults.go
// * https://github.com/kubernetes/kubernetes/blob/e5976909c6fb129228a67515e0f86336a53884f0/pkg/apis/batch/v1/zz_generated.defaults.go
// * https://github.com/kubernetes/kubernetes/blob/e5976909c6fb129228a67515e0f86336a53884f0/pkg/apis/rbac/v1/zz_generated.defaults.go

func defaultServiceSpec(original, mutated *corev1.ServiceSpec) {
	if mutated.ClusterIP == "" {
		mutated.ClusterIP = original.ClusterIP
	}
	if mutated.ClusterIPs == nil {
		mutated.ClusterIPs = original.ClusterIPs
	}
	if mutated.IPFamilies == nil {
		mutated.IPFamilies = original.IPFamilies
	}
	if mutated.IPFamilyPolicy == nil {
		mutated.IPFamilyPolicy = original.IPFamilyPolicy
	}
	for i := range original.Ports {
		if i >= len(mutated.Ports) {
			break
		}
		if mutated.Ports[i].Protocol == "" {
			mutated.Ports[i].Protocol = original.Ports[i].Protocol
		}
		if mutated.Ports[i].TargetPort.String() == "0" {
			mutated.Ports[i].TargetPort = intstr.FromInt32(mutated.Ports[i].Port)
		}
	}
	if mutated.SessionAffinity == "" {
		mutated.SessionAffinity = original.SessionAffinity
	}
	if mutated.Type == "" {
		mutated.Type = original.Type
	}
	if mutated.InternalTrafficPolicy == nil {
		mutated.InternalTrafficPolicy = original.InternalTrafficPolicy
	}
	if mutated.Type == corev1.ServiceTypeLoadBalancer {
		if mutated.AllocateLoadBalancerNodePorts == nil {
			mutated.AllocateLoadBalancerNodePorts = original.AllocateLoadBalancerNodePorts
		}
	}
}

func defaultDeploymentSpec(original, mutated *appsv1.DeploymentSpec) {
	if mutated.ProgressDeadlineSeconds == nil {
		mutated.ProgressDeadlineSeconds = original.ProgressDeadlineSeconds
	}
	if mutated.Replicas == nil {
		mutated.Replicas = original.Replicas
	}
	if mutated.Strategy.Type == "" {
		mutated.Strategy.Type = original.Strategy.Type
	}
	if mutated.Strategy.RollingUpdate == nil && original.Strategy.RollingUpdate != nil {
		mutated.Strategy.RollingUpdate = original.Strategy.RollingUpdate
	}
	if mutated.Strategy.RollingUpdate != nil && original.Strategy.RollingUpdate != nil {
		if mutated.Strategy.RollingUpdate.MaxSurge == nil {
			mutated.Strategy.RollingUpdate.MaxSurge = original.Strategy.RollingUpdate.MaxSurge
		}
		if mutated.Strategy.RollingUpdate.MaxUnavailable == nil {
			mutated.Strategy.RollingUpdate.MaxUnavailable = original.Strategy.RollingUpdate.MaxUnavailable
		}
	}
	if mutated.RevisionHistoryLimit == nil {
		mutated.RevisionHistoryLimit = original.RevisionHistoryLimit
	}

	defaultPodSpec(&original.Template.Spec, &mutated.Template.Spec)
}

func defaultDaemonSetSpec(original, mutated *appsv1.DaemonSetSpec) {
	if mutated.RevisionHistoryLimit == nil {
		mutated.RevisionHistoryLimit = original.RevisionHistoryLimit
	}
	if mutated.UpdateStrategy.Type == "" {
		mutated.UpdateStrategy.Type = original.UpdateStrategy.Type
	}
	if mutated.UpdateStrategy.RollingUpdate == nil && original.UpdateStrategy.RollingUpdate != nil {
		mutated.UpdateStrategy.RollingUpdate = original.UpdateStrategy.RollingUpdate
	}
	defaultPodSpec(&original.Template.Spec, &mutated.Template.Spec)
}

func defaultPodSpec(original, mutated *corev1.PodSpec) {
	for i := range original.InitContainers {
		if i >= len(mutated.InitContainers) {
			break
		}
		defaultContainer(&original.InitContainers[i], &mutated.InitContainers[i])
	}
	for i := range original.Containers {
		if i >= len(mutated.Containers) {
			break
		}
		defaultContainer(&original.Containers[i], &mutated.Containers[i])
	}
	if mutated.DNSPolicy == "" {
		mutated.DNSPolicy = original.DNSPolicy
	}
	if mutated.ServiceAccountName == "" {
		mutated.ServiceAccountName = original.ServiceAccountName
	}
	if mutated.DeprecatedServiceAccount == "" {
		mutated.DeprecatedServiceAccount = original.DeprecatedServiceAccount
	}
	if mutated.RestartPolicy == "" {
		mutated.RestartPolicy = original.RestartPolicy
	}
	if mutated.SchedulerName == "" {
		mutated.SchedulerName = original.SchedulerName
	}
	if mutated.TerminationGracePeriodSeconds == nil {
		mutated.TerminationGracePeriodSeconds = original.TerminationGracePeriodSeconds
	}

	for i := range original.Volumes {
		if i >= len(mutated.Volumes) {
			break
		}
		defaultVolume(&original.Volumes[i], &mutated.Volumes[i])
	}

	if mutated.SecurityContext == nil {
		mutated.SecurityContext = original.SecurityContext
	}
}

func defaultContainer(original, mutated *corev1.Container) {
	if mutated.ImagePullPolicy == "" {
		mutated.ImagePullPolicy = original.ImagePullPolicy
	}
	if mutated.TerminationMessagePath == "" {
		mutated.TerminationMessagePath = original.TerminationMessagePath
	}
	if mutated.TerminationMessagePolicy == "" {
		mutated.TerminationMessagePolicy = original.TerminationMessagePolicy
	}

	if original.LivenessProbe != nil && mutated.LivenessProbe != nil {
		defaultProbe(original.LivenessProbe, mutated.LivenessProbe)
	}

	if original.ReadinessProbe != nil && mutated.ReadinessProbe != nil {
		defaultProbe(original.ReadinessProbe, mutated.ReadinessProbe)
	}

	for i := range original.Env {
		if i >= len(mutated.Env) {
			break
		}
		defaultEnv(&original.Env[i], &mutated.Env[i])
	}

	for i := range original.Ports {
		if i >= len(mutated.Ports) {
			break
		}
		defaultContainerPort(&original.Ports[i], &mutated.Ports[i])
	}

	if original.SecurityContext != nil && mutated.SecurityContext == nil {
		mutated.SecurityContext = original.SecurityContext
	}
	if original.SecurityContext != nil && mutated.SecurityContext != nil {
		if mutated.SecurityContext.RunAsUser == nil && original.SecurityContext.RunAsUser != nil {
			mutated.SecurityContext.RunAsUser = original.SecurityContext.RunAsUser
		}
	}
}

func defaultProbe(original, mutated *corev1.Probe) {
	if mutated.TimeoutSeconds == 0 {
		mutated.TimeoutSeconds = original.TimeoutSeconds
	}
	if mutated.PeriodSeconds == 0 {
		mutated.PeriodSeconds = original.PeriodSeconds
	}
	if mutated.SuccessThreshold == 0 {
		mutated.SuccessThreshold = original.SuccessThreshold
	}
	if mutated.FailureThreshold == 0 {
		mutated.FailureThreshold = original.FailureThreshold
	}
	if mutated.HTTPGet != nil && original.HTTPGet != nil && mutated.HTTPGet.Scheme == "" {
		mutated.HTTPGet.Scheme = original.HTTPGet.Scheme
	}
}

func defaultVolume(original, mutated *corev1.Volume) {
	if mutated.VolumeSource.Secret != nil && original.VolumeSource.Secret != nil && mutated.VolumeSource.Secret.DefaultMode == nil {
		mutated.VolumeSource.Secret.DefaultMode = original.VolumeSource.Secret.DefaultMode
	}
	if mutated.VolumeSource.ConfigMap != nil && original.VolumeSource.ConfigMap != nil && mutated.VolumeSource.ConfigMap.DefaultMode == nil {
		mutated.VolumeSource.ConfigMap.DefaultMode = original.VolumeSource.ConfigMap.DefaultMode
	}
}

func defaultVolumeClaim(original, mutated *corev1.PersistentVolumeClaimSpec) {
	if original.VolumeMode != nil && mutated.VolumeMode == nil {
		mutated.VolumeMode = original.VolumeMode
	}
	if original.StorageClassName != nil && mutated.StorageClassName == nil {
		mutated.StorageClassName = original.StorageClassName
	}
}

func defaultEnv(original, mutated *corev1.EnvVar) {
	if mutated.ValueFrom != nil && original.ValueFrom != nil && mutated.ValueFrom.FieldRef != nil && original.ValueFrom.FieldRef != nil && mutated.ValueFrom.FieldRef.APIVersion == "" {
		mutated.ValueFrom.FieldRef.APIVersion = original.ValueFrom.FieldRef.APIVersion
	}
}

func defaultContainerPort(original, mutated *corev1.ContainerPort) {
	if mutated.Protocol == "" {
		mutated.Protocol = original.Protocol
	}
}

func defaultCRDSpec(mutated *apiextensionsv1.CustomResourceDefinitionSpec) {
	if mutated.Conversion == nil {
		mutated.Conversion = &apiextensionsv1.CustomResourceConversion{
			Strategy: apiextensionsv1.NoneConverter,
		}
	}
}

func defaultSubject(mutated *rbacv1.Subject) {
	if len(mutated.APIGroup) == 0 {
		switch mutated.Kind {
		case rbacv1.ServiceAccountKind:
			mutated.APIGroup = ""
		case rbacv1.UserKind:
			mutated.APIGroup = rbacv1.GroupName
		case rbacv1.GroupKind:
			mutated.APIGroup = rbacv1.GroupName
		}
	}
}
