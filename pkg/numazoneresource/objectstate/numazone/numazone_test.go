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

package numazone

import (
	"context"
	"reflect"
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcov1 "github.com/openshift/api/machineconfiguration/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	securityv1 "github.com/openshift/api/security/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/pkg/numazoneresource"
	numazonemanifests "github.com/openshift-kni/numaresources-operator/pkg/numazoneresource/manifests/numazone"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
)

func TestPerTree(t *testing.T) {
	for _, plat := range []platform.Platform{platform.OpenShift, platform.HyperShift} {
		for _, mode := range []nropv1.NumazoneMode{nropv1.NumazoneEnabled, nropv1.NumazonePassthrough} {
			for _, logLevel := range []operatorv1.LogLevel{operatorv1.Normal, operatorv1.Trace} {
				t.Run(string(plat)+"/"+string(mode)+"/"+string(logLevel), func(t *testing.T) {
					mf, err := numazonemanifests.GetManifests(plat, "test")
					if err != nil {
						t.Fatal(err)
					}
					tolerations := []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}}
					tree := nodegroupv1.Tree{NodeGroup: &nropv1.NodeGroup{
						PoolName: ptr.To("worker"),
						Numazone: &nropv1.NumazoneConfig{Mode: ptr.To(mode), LogLevel: logLevel},
						Config:   &nropv1.NodeGroupConfig{Tolerations: tolerations},
					}}
					wantSelector := map[string]string{"hypershift.openshift.io/nodePool": "worker"}
					if plat == platform.OpenShift {
						wantSelector = map[string]string{"node-role.kubernetes.io/worker": ""}
						tree.NodeGroup.PoolName = nil
						tree.NodeGroup.MachineConfigPoolSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "worker"}}
						tree.MachineConfigPools = []*mcov1.MachineConfigPool{{ObjectMeta: metav1.ObjectMeta{Name: "worker"}, Spec: mcov1.MachineConfigPoolSpec{
							NodeSelector: &metav1.LabelSelector{MatchLabels: wantSelector},
						}}}
					}
					cli := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
					states := PerTree(context.Background(), cli, mf, plat, "nro", tree, "operator-image", corev1.PullAlways)
					if len(states) != 1 || !states[0].IsNotFoundError() || states[0].UpdateError != nil {
						t.Fatalf("unexpected states: %+v", states)
					}
					ds := states[0].Desired.(*appsv1.DaemonSet)
					if ds.Name != numazoneresource.DaemonSetName("nro", "worker") || !reflect.DeepEqual(ds.Spec.Template.Spec.NodeSelector, wantSelector) {
						t.Fatalf("unexpected daemonset placement: %s %v", ds.Name, ds.Spec.Template.Spec.NodeSelector)
					}
					if !reflect.DeepEqual(ds.Spec.Template.Spec.Tolerations, tolerations) {
						t.Fatal("nodegroup tolerations are not applied")
					}
					if ds.Spec.Selector.MatchLabels["daemonset"] != ds.Name || ds.Spec.Template.Labels["daemonset"] != ds.Name {
						t.Fatal("daemonset selectors overlap across nodegroups")
					}
					if ds.Spec.Template.Spec.Containers[0].Image != "operator-image" || ds.Spec.Template.Spec.Containers[0].ImagePullPolicy != corev1.PullAlways {
						t.Fatal("incorrect image settings")
					}
					wantVerbosity := "-v=2"
					if logLevel == operatorv1.Trace {
						wantVerbosity = "-v=6"
					}
					if !slices.Contains(ds.Spec.Template.Spec.Containers[0].Args, wantVerbosity) {
						t.Fatalf("expected %s in daemonset arguments: %v", wantVerbosity, ds.Spec.Template.Spec.Containers[0].Args)
					}
					if err := cli.Create(context.Background(), ds); err != nil {
						t.Fatal(err)
					}
					states = PerTree(context.Background(), cli, mf, plat, "nro", tree, "operator-image", corev1.PullAlways)
					if states[0].Error != nil {
						t.Fatal(states[0].Error)
					}
					merged, err := states[0].Merge(states[0].Existing, states[0].Desired)
					if err != nil {
						t.Fatal(err)
					}
					equal, err := compare.Object(states[0].Existing, merged)
					if err != nil || !equal {
						t.Fatalf("unchanged configuration is not idempotent: %v\n%s", err, diff.Diff(states[0].Existing, merged))
					}
				})
			}
		}
	}
}

func TestSharedStatePreservesPullSecrets(t *testing.T) {
	if err := securityv1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatal(err)
	}
	mf, err := numazonemanifests.GetManifests(platform.OpenShift, "test")
	if err != nil {
		t.Fatal(err)
	}
	sa := mf.ServiceAccount.DeepCopy()
	sa.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "injected"}}
	cli := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(sa).Build()
	states := TreeAgnostic(context.Background(), cli, mf)
	if len(states) != 2 || states[0].Error != nil || !states[1].IsNotFoundError() {
		t.Fatalf("unexpected shared states: %+v", states)
	}
	merged, err := states[0].Merge(states[0].Existing, states[0].Desired)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(merged.(*corev1.ServiceAccount).ImagePullSecrets, sa.ImagePullSecrets) {
		t.Fatal("injected pull secrets lost")
	}
}
