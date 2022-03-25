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

package config

import (
	"context"

	. "github.com/onsi/gomega"

	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

type E2EConfig struct {
	Fixture       *e2efixture.Fixture
	NRTList       nrtv1alpha1.NodeResourceTopologyList
	NROOperObj    *nropv1alpha1.NUMAResourcesOperator
	NROSchedObj   *nropv1alpha1.NUMAResourcesScheduler
	SchedulerName string
}

func SetupFixture() *E2EConfig {
	return SetupFixtureWithOptions("e2e-test-infra", e2efixture.OptionRandomizeName)
}

func SetupFixtureWithOptions(nsName string, options e2efixture.Options) *E2EConfig {
	var err error
	cfg := E2EConfig{
		NROOperObj:  &nropv1alpha1.NUMAResourcesOperator{},
		NROSchedObj: &nropv1alpha1.NUMAResourcesScheduler{},
	}

	cfg.Fixture, err = e2efixture.SetupWithOptions(nsName, options)
	Expect(err).ToNot(HaveOccurred(), "unable to setup infra test fixture")

	err = cfg.Fixture.Client.List(context.TODO(), &cfg.NRTList)
	Expect(err).ToNot(HaveOccurred())

	err = cfg.Fixture.Client.Get(context.TODO(), client.ObjectKey{Name: nrosched.NROSchedObjectName}, cfg.NROSchedObj)
	Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nrosched.NROSchedObjectName)

	err = cfg.Fixture.Client.Get(context.TODO(), client.ObjectKey{Name: objects.NROName()}, cfg.NROOperObj)
	Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", objects.NROName())

	Expect(cfg.NROOperObj.Spec.NodeGroups).ToNot(BeEmpty(), "cannot autodetect the TAS node groups from the cluster")

	cfg.SchedulerName = cfg.NROSchedObj.Status.SchedulerName
	Expect(cfg.SchedulerName).ToNot(BeEmpty(), "cannot autodetect the TAS scheduler name from the cluster")
	klog.Infof("scheduler name: %q", cfg.SchedulerName)

	return &cfg
}

func TeardownFixture(cfg *E2EConfig) {
	err := e2efixture.Teardown(cfg.Fixture)
	Expect(err).NotTo(HaveOccurred())
}
