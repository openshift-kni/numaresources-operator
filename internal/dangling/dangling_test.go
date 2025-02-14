/*
 * Copyright 2025 Red Hat, Inc.
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

package dangling

import (
	"context"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

const (
	// the nro constants below are used to point to the same nro test object across all
	// the functions which is only legal to have a single object in a k8s cluster
	nrouid  = "6e662cb5-9264-494d-bed8-8ddb817317cc"
	nroName = "nro"
	nroNs   = "nro-ns"
)

func TestDeleteUnusedMachineConfigs(t *testing.T) {
	targetMCPName := "mcp1" // var because we need its pointer

	nro := nropv1.NUMAResourcesOperator{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nroName,
			Namespace: nroNs,
			UID:       nrouid,
		},
	}

	trees := []nodegroupv1.Tree{
		{
			NodeGroup: &nropv1.NodeGroup{
				PoolName: &targetMCPName,
			},
			MachineConfigPools: []*mcov1.MachineConfigPool{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: targetMCPName,
					},
				},
			},
		},
	}

	mcs := []mcov1.MachineConfig{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: objectnames.GetMachineConfigName(nroName, targetMCPName),
				OwnerReferences: []metav1.OwnerReference{
					{
						UID: nrouid,
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: objectnames.GetMachineConfigName(nroName, "mcp2"),
				OwnerReferences: []metav1.OwnerReference{
					{
						UID: nrouid, // owned by NRO object but dangling
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "51-XX-mcp1",
				OwnerReferences: []metav1.OwnerReference{
					{
						UID: "100", // not owned by NRO object
					},
				},
			},
		},
	}

	_ = mcov1.AddToScheme(scheme.Scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(&mcs[0], &mcs[1], &mcs[2]).Build()

	err := isMachineConfigFound(fakeClient, context.TODO(), mcs...)
	if err != nil {
		t.Errorf("failed to find machine configs: %v", err)
		return
	}

	err = DeleteUnusedMachineConfigs(fakeClient, context.TODO(), &nro, trees)
	if err != nil {
		t.Fatalf("failed to delete unused dangling machine configs: %v", err)
		return
	}

	err = isMachineConfigFound(fakeClient, context.TODO(), mcs[0], mcs[2])
	if err != nil {
		t.Errorf("failed to find machine config that is owned by the NRO object and not dangling: %v", err)
	}
	err = isMachineConfigFound(fakeClient, context.TODO(), mcs[1])
	if err == nil {
		t.Errorf("found dangling machine config %s from fake client which was expected to be deleted", mcs[1].Name)
	}
}

func TestDeleteUnusedDaemonSets(t *testing.T) {
	targetMCPName := "mcp1" // var because we need its pointer

	nro := nropv1.NUMAResourcesOperator{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nroName,
			Namespace: nroNs,
			UID:       nrouid,
		},
	}

	trees := []nodegroupv1.Tree{
		{
			NodeGroup: &nropv1.NodeGroup{
				PoolName: &targetMCPName,
			},
			MachineConfigPools: []*mcov1.MachineConfigPool{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: targetMCPName,
					},
				},
			},
		},
	}
	dss := []appsv1.DaemonSet{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      objectnames.GetComponentName(nroName, targetMCPName),
				Namespace: nroNs,
				OwnerReferences: []metav1.OwnerReference{
					{
						UID: nrouid,
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      objectnames.GetComponentName(nroName, "mcp2"),
				Namespace: nroNs,
				OwnerReferences: []metav1.OwnerReference{
					{
						UID: nrouid, // owned by NRO object but dangling
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ds100",
				Namespace: nroNs,
				OwnerReferences: []metav1.OwnerReference{
					{
						UID: "100", // not owned by NRO object
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(&dss[0], &dss[1], &dss[2]).Build()

	err := isDaemonSetFound(fakeClient, context.TODO(), dss...)
	if err != nil {
		t.Errorf("failed to find RTE daemonsets: %v", err)
	}

	err = DeleteUnusedDaemonSets(fakeClient, context.TODO(), &nro, trees)
	if err != nil {
		t.Errorf("failed to delete unused dangling daemonsets: %v", err)
		return
	}

	err = isDaemonSetFound(fakeClient, context.TODO(), dss[0], dss[2])
	if err != nil {
		t.Errorf("failed to find daemonset that is owned by the NRO object and not dangling: %v", err)
	}
	err = isDaemonSetFound(fakeClient, context.TODO(), dss[1])
	if err == nil {
		t.Errorf("found dangling daemonset %s/%s from fake client which was expected to be deleted", dss[1].Namespace, dss[1].Name)
	}
}

func isMachineConfigFound(cli client.Client, ctx context.Context, objs ...mcov1.MachineConfig) error {
	var mc mcov1.MachineConfig
	for _, obj := range objs {
		err := cli.Get(ctx, client.ObjectKeyFromObject(&obj), &mc)
		if err != nil {
			return fmt.Errorf("failed to get machine config object %s from fake client: %v", obj.Name, err)
		}
	}
	return nil
}

func isDaemonSetFound(cli client.Client, ctx context.Context, objs ...appsv1.DaemonSet) error {
	var ds appsv1.DaemonSet
	for _, obj := range objs {
		err := cli.Get(ctx, client.ObjectKeyFromObject(&obj), &ds)
		if err != nil {
			return fmt.Errorf("failed to get daemonset object %s from fake client: %v", obj.Name, err)
		}
	}
	return nil
}
