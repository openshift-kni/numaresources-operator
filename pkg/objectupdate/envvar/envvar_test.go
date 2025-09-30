package envvar

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestFindByNameMutablePod(t *testing.T) {
	testEnvVarName := "TEST_FOO_BAR"

	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "foo",
					Env: []corev1.EnvVar{
						{
							Name:  testEnvVarName,
							Value: "33",
						},
					},
				},
			},
		},
	}

	got := FindByName(pod.Spec.Containers[0].Env, testEnvVarName)
	if got == nil {
		t.Fatalf("missing container env var")
	}

	newValue := "42"
	got.Value = newValue
	if pod.Spec.Containers[0].Env[0].Value != newValue {
		t.Fatalf("failed to mutate through the FindByName reference")
	}
}

func TestFindByNameMutableDeployment(t *testing.T) {
	testEnvVarName := "FIZZBUZZ"

	dp := appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							Env: []corev1.EnvVar{
								{
									Name:  testEnvVarName,
									Value: "27",
								},
							},
						},
					},
				},
			},
		},
	}

	got := FindByName(dp.Spec.Template.Spec.Containers[0].Env, testEnvVarName)
	if got == nil {
		t.Fatalf("missing container env var")
	}

	newValue := "42"
	got.Value = newValue
	if dp.Spec.Template.Spec.Containers[0].Env[0].Value != newValue {
		t.Fatalf("failed to mutate through the FindByName reference")
	}
}

func TestSetForContainerMutableDaemonset(t *testing.T) {
	testEnvVarName := "answer"
	testEnvVarValue := "42"

	ds := appsv1.DaemonSet{
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							Env: []corev1.EnvVar{
								{
									Name:  "something",
									Value: "27",
								},
							},
						},
					},
				},
			},
		},
	}

	SetForContainer(&ds.Spec.Template.Spec.Containers[0], testEnvVarName, testEnvVarValue)

	if len(ds.Spec.Template.Spec.Containers[0].Env) != 2 {
		t.Errorf("unexpected env var count")
	}

	got := FindByName(ds.Spec.Template.Spec.Containers[0].Env, testEnvVarName)
	if got == nil {
		t.Fatalf("missing container env var")
	}
}

func TestSetForContainerDoesNotDuplicate(t *testing.T) {
	testEnvVarName := "answer"
	testEnvVarValue := "42"

	ds := appsv1.DaemonSet{
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							Env: []corev1.EnvVar{
								{
									Name:  testEnvVarName,
									Value: testEnvVarValue,
								},
							},
						},
					},
				},
			},
		},
	}

	SetForContainer(&ds.Spec.Template.Spec.Containers[0], testEnvVarName, "27")

	if len(ds.Spec.Template.Spec.Containers[0].Env) != 1 {
		t.Errorf("unexpected env var count")
	}

	got := FindByName(ds.Spec.Template.Spec.Containers[0].Env, testEnvVarName)
	if got == nil {
		t.Fatalf("missing container env var")
	}
}

func TestDeleteFromContainer(t *testing.T) {
	testEnvVarName := "answer"
	testEnvVarValue := "42"

	ds := appsv1.DaemonSet{
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							Env: []corev1.EnvVar{
								{
									Name:  testEnvVarName,
									Value: testEnvVarValue,
								},
							},
						},
					},
				},
			},
		},
	}

	DeleteFromContainer(&ds.Spec.Template.Spec.Containers[0], testEnvVarName)

	if len(ds.Spec.Template.Spec.Containers[0].Env) != 0 {
		t.Errorf("unexpected env var count")
	}

	got := FindByName(ds.Spec.Template.Spec.Containers[0].Env, testEnvVarName)
	if got != nil {
		t.Fatalf("missing container env var")
	}
}

func TestDeleteFromContainerMissing(t *testing.T) {
	testEnvVarName := "answer"

	ds := appsv1.DaemonSet{
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							Env: []corev1.EnvVar{
								{
									Name:  "quux",
									Value: "bazz",
								},
							},
						},
					},
				},
			},
		},
	}

	DeleteFromContainer(&ds.Spec.Template.Spec.Containers[0], testEnvVarName)

	if len(ds.Spec.Template.Spec.Containers[0].Env) != 1 {
		t.Errorf("unexpected env var count")
	}

	got := FindByName(ds.Spec.Template.Spec.Containers[0].Env, "quux")
	if got == nil {
		t.Fatalf("missing unrelated container env var")
	}
}
