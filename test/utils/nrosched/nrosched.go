/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nrosched

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

const (
	// TODO: fetch this from NRO scheduler status
	NROSchedulerName   = "topo-aware-scheduler"
	NROSchedObjectName = "numaresourcesscheduler"
)

func CheckNROSchedulerAvailable(cli client.Client, nroSchedName string) *nropv1alpha1.NUMAResourcesScheduler {
	nroSchedObj := &nropv1alpha1.NUMAResourcesScheduler{}
	Eventually(func() bool {
		By(fmt.Sprintf("checking %q for the condition Available=true", nroSchedName))

		err := cli.Get(context.TODO(), client.ObjectKey{Name: NROSchedObjectName}, nroSchedObj)
		if err != nil {
			klog.Warningf("failed to get the scheduler resource: %v", err)
			return false
		}

		cond := status.FindCondition(nroSchedObj.Status.Conditions, status.ConditionAvailable)
		if cond == nil {
			klog.Warningf("missing conditions in %v", nroSchedObj)
			return false
		}

		klog.Infof("condition: %v", cond)

		return cond.Status == metav1.ConditionTrue
	}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "Scheduler condition did not become available")
	return nroSchedObj
}
