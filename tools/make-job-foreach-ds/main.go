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

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/yaml"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	securityv1 "github.com/openshift/api/security/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
)

var (
	scheme = k8sruntime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(nropv1.AddToScheme(scheme))
	utilruntime.Must(machineconfigv1.Install(scheme))
	utilruntime.Must(securityv1.Install(scheme))
}

type ChrootJob struct {
	Name           string
	Namespace      string
	Script         string
	ServiceAccount string
}

func validate(cj ChrootJob, scriptPath string) {
	if cj.Name == "" {
		log.Fatalf("missing job name")
	}
	if cj.ServiceAccount == "" {
		log.Fatalf("missing service account")
	}
	if scriptPath == "" {
		log.Fatalf("missing script to run")
	}
}

func main() {
	scriptPath := ""
	cj := ChrootJob{}
	flag.StringVar(&cj.Name, "name", cj.Name, "task name")
	flag.StringVar(&cj.Namespace, "namespace", cj.Namespace, "override autodetected namespace - leave blank to autodetect from DaemonSets")
	flag.StringVar(&cj.ServiceAccount, "serviceaccount", cj.ServiceAccount, "serviceAccount to run jobs under")
	flag.StringVar(&scriptPath, "script", scriptPath, "path of script to run on each host")
	flag.Parse()

	validate(cj, scriptPath)

	data, err := os.ReadFile(scriptPath)
	if err != nil {
		log.Fatalf("cannot read script at %q: %v", scriptPath, err)
	}
	cj.Script = string(data)

	cli, err := NewClientWithScheme(scheme)
	if err != nil {
		log.Fatalf("creating the client: %v", err)
	}

	ctx := context.Background()
	dss, err := GetDaemonsetsByNRO(ctx, cli)
	if err != nil {
		log.Fatalf("getting daemonsets: %v", err)
	}

	for _, ds := range dss {
		log.Printf("DaemonSet: %s\n", ds.String())

		dsKey := client.ObjectKey{
			Namespace: ds.Namespace,
			Name:      ds.Name,
		}
		dsInstance := appsv1.DaemonSet{}
		err := cli.Get(ctx, dsKey, &dsInstance)
		if err != nil {
			log.Printf("getting daemonset %s: %v", ds.String(), err)
			continue
		}

		parallelism := dsInstance.Status.CurrentNumberScheduled
		log.Printf("DaemonSet: %s: pods=%d %v\n", ds.String(), parallelism, dsInstance.Spec.Template.Spec.NodeSelector)

		job := MakeChrootJob(cj, &dsInstance)
		data, err := yaml.Marshal(job)
		if err != nil {
			log.Printf("creating the job YAML to match daemonset %s", ds.String())
			continue
		}
		fmt.Println("---")
		fmt.Println(string(data))
	}
}

func NewClientWithScheme(scheme *k8sruntime.Scheme) (client.Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	return client.New(cfg, client.Options{Scheme: scheme})
}

func GetDaemonsetsByNRO(ctx context.Context, cli client.Client) ([]nropv1.NamespacedName, error) {
	nroNamespacedName := types.NamespacedName{
		Name: objectnames.DefaultNUMAResourcesOperatorCrName,
	}
	nroInstance := &nropv1.NUMAResourcesOperator{}
	err := cli.Get(ctx, nroNamespacedName, nroInstance)
	if err != nil {
		return nil, err
	}
	return nroInstance.Status.DaemonSets, nil
}

func MakeChrootJob(cj ChrootJob, ds *appsv1.DaemonSet) *batchv1.Job {
	namespace := ds.Namespace
	if cj.Namespace != "" {
		namespace = cj.Namespace
	}
	parallelism := ds.Status.CurrentNumberScheduled
	image := ds.Spec.Template.Spec.Containers[0].Image
	hostPathDirectory := corev1.HostPathDirectory
	root_ := int64(0)
	true_ := true
	jobLabels := map[string]string{
		"app": cj.Name,
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: cj.Name + "-",
		},
		Spec: batchv1.JobSpec{
			Parallelism: &parallelism,
			Completions: &parallelism,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: jobLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "chroot-task",
							Image: image,
							Command: []string{
								"/usr/bin/chroot",
								"/host",
								"/bin/sh",
								"-c",
								cj.Script,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &true_,
								RunAsUser:  &root_,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/host",
									Name:      "host",
								},
							},
						},
					},
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: cj.ServiceAccount,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser: &root_,
					},
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							WhenUnsatisfiable: corev1.DoNotSchedule,
							TopologyKey:       "kubernetes.io/hostname",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: jobLabels,
							},
							MatchLabelKeys: []string{
								"pod-template-hash",
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "host",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/",
									Type: &hostPathDirectory,
								},
							},
						},
					},
				},
			},
		},
	}
	return job
}
