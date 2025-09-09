/*
 * Copyright 2022 Red Hat, Inc.
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

package config

import (
	"context"

	"github.com/go-logr/logr"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/fixture/dumpr"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
)

type E2EConfig struct {
	Fixture       *e2efixture.Fixture
	NRTList       nrtv1alpha2.NodeResourceTopologyList
	NROOperObj    *nropv1.NUMAResourcesOperator
	NROSchedObj   *nropv1.NUMAResourcesScheduler
	SchedulerName string
	infraNRTList  nrtv1alpha2.NodeResourceTopologyList
}

func (cfg *E2EConfig) Ready() bool {
	if cfg == nil {
		return false
	}
	if cfg.Fixture == nil || cfg.NROOperObj == nil || cfg.NROSchedObj == nil {
		return false
	}
	if cfg.SchedulerName == "" {
		return false
	}
	return true
}

func (cfg *E2EConfig) RecordNRTReference() error {
	err := cfg.Fixture.Client.List(context.TODO(), &cfg.NRTList)
	if err != nil {
		return err
	}
	// TODO: multi-line value in structured log
	cfg.Fixture.Log.Info("recorded reference NRT data", "data", intnrt.ListToString(cfg.NRTList.Items, " reference"))
	return nil
}

var Config *E2EConfig

func SetupFixture(lh logr.Logger, dp dumpr.Dumper) error {
	var err error
	Config, err = NewFixtureWithOptions(
		"e2e-test-infra",
		e2efixture.WithRandomizeName(),
		e2efixture.WithAvoidCooldown(),
		e2efixture.WithStaticClusterData(),
		e2efixture.WithLogger(lh),
		e2efixture.WithDumper(dp),
	)
	return err
}

func TeardownFixture() error {
	return e2efixture.Teardown(Config.Fixture)
}

func NewFixtureWithOptions(nsName string, options ...e2efixture.Option) (*E2EConfig, error) {
	var err error
	cfg := E2EConfig{
		NROOperObj:  &nropv1.NUMAResourcesOperator{},
		NROSchedObj: &nropv1.NUMAResourcesScheduler{},
	}

	cfg.Fixture, err = e2efixture.SetupWithOptions(nsName, nrtv1alpha2.NodeResourceTopologyList{}, options...)
	if err != nil {
		return nil, err
	}

	err = cfg.Fixture.Client.List(context.TODO(), &cfg.infraNRTList)
	if err != nil {
		return nil, err
	}

	err = cfg.Fixture.Client.Get(context.TODO(), objects.NROObjectKey(), cfg.NROOperObj)
	if err != nil {
		return nil, err
	}

	err = cfg.Fixture.Client.Get(context.TODO(), objects.NROSchedObjectKey(), cfg.NROSchedObj)
	if err != nil {
		return nil, err
	}

	cfg.SchedulerName = cfg.NROSchedObj.Status.SchedulerName
	cfg.Fixture.Log.Info("detected scheduler name", "schedulerName", cfg.SchedulerName)

	return &cfg, nil
}
