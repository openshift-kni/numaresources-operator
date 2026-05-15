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

package wait

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDeploymentExpectedReplicaCount(t *testing.T) {
	want := int32(3)
	got := deploymentExpectedReplicaCount(&want, 1)
	if got != want {
		t.Fatalf("expected %d, got %d", want, got)
	}

	got = deploymentExpectedReplicaCount(nil, 2)
	if got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
}

func TestIsDeploymentComplete(t *testing.T) {
	status := appsv1.DeploymentStatus{
		Replicas:           3,
		UpdatedReplicas:    3,
		AvailableReplicas:  3,
		ObservedGeneration: 5,
	}
	if !IsDeploymentComplete(5, ptr.To(int32(3)), &status) {
		t.Fatal("expected deployment to be complete")
	}
	if IsDeploymentComplete(6, ptr.To(int32(3)), &status) {
		t.Fatal("expected deployment to be incomplete when generation not observed")
	}
	if IsDeploymentComplete(5, ptr.To(int32(2)), &status) {
		t.Fatal("expected deployment to be incomplete when replica count mismatches")
	}
}

func TestIsDeploymentRolloutComplete(t *testing.T) {
	const (
		ns       = "test-ns"
		depName  = "scheduler"
		revision = "2"
		replicas = int32(3)
	)

	labelSel := map[string]string{"app": depName}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      depName,
			Namespace: ns,
			UID:       "dep-uid",
			Annotations: map[string]string{
				deploymentRevisionAnnotation: revision,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labelSel},
			Replicas: ptr.To(replicas),
		},
		Status: appsv1.DeploymentStatus{Replicas: replicas},
	}

	currentRS := newControlledReplicaSet(dep, "scheduler-2", revision, replicas, replicas)
	currentRS.UID = "rs-current-uid"
	oldRSWithPods := newControlledReplicaSet(dep, "scheduler-1", "1", replicas, replicas)
	oldRSWithPods.UID = "rs-old-uid"
	oldRSWithPods.Status.Replicas = 1
	oldRSWithPods.Status.ReadyReplicas = 1
	oldRSWithPods.Status.AvailableReplicas = 1

	tests := []struct {
		name    string
		objects []runtime.Object
		want    bool
	}{
		{
			name:    "current revision replicaset ready",
			objects: []runtime.Object{dep, currentRS},
			want:    true,
		},
		{
			name:    "old replicaset still has replicas",
			objects: []runtime.Object{dep, currentRS, oldRSWithPods},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cli := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(tt.objects...).Build()
			wt := Waiter{Cli: cli}

			got, err := wt.isDeploymentRolloutComplete(context.TODO(), dep, dep.Spec.Replicas)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("isDeploymentRolloutComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func newControlledReplicaSet(dep *appsv1.Deployment, name, revision string, ready, total int32) *appsv1.ReplicaSet {
	controller := true
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  dep.Namespace,
			Labels:     dep.Spec.Selector.MatchLabels,
			Generation: 1,
			Annotations: map[string]string{
				deploymentRevisionAnnotation: revision,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "Deployment",
				Name:       dep.Name,
				UID:        dep.UID,
				Controller: &controller,
			}},
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: dep.Spec.Selector,
			Replicas: ptr.To(total),
		},
		Status: appsv1.ReplicaSetStatus{
			Replicas:           total,
			ReadyReplicas:      ready,
			AvailableReplicas:  ready,
			ObservedGeneration: 1,
		},
	}
	return rs
}
