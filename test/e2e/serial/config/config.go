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
	"os"
)

const (
	MultiNUMALabel    = "numa.hardware.openshift-kni.io/cell-count"
	NropTestCIImage   = "quay.io/openshift-kni/resource-topology-exporter:test-ci"
	SchedulerTestName = "test-topology-scheduler"
)

// This suite holds the e2e tests which span across components,
// e.g. involve both the behaviour of RTE and the scheduler.
// These tests are almost always disruptive, meaning they significantly
// alter the cluster state and need a very specific cluster state (which
// is each test responsability to setup and cleanup).
// Hence we call this suite serial, implying each test should run alone
// and indisturbed on the cluster. No concurrency at all is possible,
// each test "owns" the cluster - but again, must leave no leftovers.

var Config *E2EConfig

func Setup() {
	Config = SetupFixture()
	SetupInfra(Config.Fixture, Config.NROOperObj, Config.NRTList)
}

func Teardown() {
	if _, ok := os.LookupEnv("E2E_INFRA_NO_TEARDOWN"); ok {
		return
	}
	TeardownInfra(Config.Fixture, Config.NRTList)
	// numacell daemonset automatically cleaned up when we remove the namespace
	TeardownFixture(Config)
}
