/*
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
 *
 * Copyright 2022 Red Hat, Inc.
 */

package sched

import (
	"os"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
)

const (
	schedConfig = `apiVersion: kubescheduler.config.k8s.io/v1beta3
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: false
profiles:
- pluginConfig:
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta3
      cacheResyncPeriodSeconds: 3
      kind: NodeResourceTopologyMatchArgs
      scoringStrategy:
        resources:
          - name: cpu
            weight: 1
          - name: memory
            weight: 1
        type: LeastAllocated
    name: NodeResourceTopologyMatch
  plugins:
    filter:
      enabled:
      - name: NodeResourceTopologyMatch
    reserve:
      enabled:
      - name: NodeResourceTopologyMatch
    score:
      enabled:
      - name: NodeResourceTopologyMatch
  schedulerName: test-topo-aware-sched`

	schedConfigWithPeriod = `apiVersion: kubescheduler.config.k8s.io/v1beta3
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: false
profiles:
  - pluginConfig:
    - args:
        apiVersion: kubescheduler.config.k8s.io/v1beta3
        cacheResyncPeriodSeconds: 3
        kind: NodeResourceTopologyMatchArgs
        scoringStrategy:
          resources:
          - name: cpu
            weight: 1
          - name: memory
            weight: 1
          type: LeastAllocated
      name: NodeResourceTopologyMatch
    plugins:
      filter:
        enabled:
        - name: NodeResourceTopologyMatch
      reserve:
        enabled:
        - name: NodeResourceTopologyMatch
      score:
        enabled:
        - name: NodeResourceTopologyMatch
    schedulerName: test-topo-aware-sched`
)

const (
	expectedYAMLWithReconcilePeriod = `apiVersion: kubescheduler.config.k8s.io/v1beta3
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: false
profiles:
- pluginConfig:
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta3
      cacheResyncPeriodSeconds: 3
      kind: NodeResourceTopologyMatchArgs
      scoringStrategy:
        resources:
        - name: cpu
          weight: 1
        - name: memory
          weight: 1
        type: LeastAllocated
    name: NodeResourceTopologyMatch
  plugins:
    filter:
      enabled:
      - name: NodeResourceTopologyMatch
    reserve:
      enabled:
      - name: NodeResourceTopologyMatch
    score:
      enabled:
      - name: NodeResourceTopologyMatch
  schedulerName: test-topo-aware-sched
`

	expectedYAMLWithoutReconcile = `apiVersion: kubescheduler.config.k8s.io/v1beta3
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: false
profiles:
- pluginConfig:
  - args:
      apiVersion: kubescheduler.config.k8s.io/v1beta3
      kind: NodeResourceTopologyMatchArgs
      scoringStrategy:
        resources:
        - name: cpu
          weight: 1
        - name: memory
          weight: 1
        type: LeastAllocated
    name: NodeResourceTopologyMatch
  plugins:
    filter:
      enabled:
      - name: NodeResourceTopologyMatch
    reserve:
      enabled:
      - name: NodeResourceTopologyMatch
    score:
      enabled:
      - name: NodeResourceTopologyMatch
  schedulerName: test-topo-aware-sched
`
)

func yamlCompare(t *testing.T, testName, got, expected string) {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(got, expected, true)
	diffCount := 0
	for idx, hunk := range diffs {
		if hunk.Type == diffmatchpatch.DiffEqual {
			continue
		}
		t.Errorf("test %q diff %d: op=%s text=%q\n", testName, idx, hunk.Type.String(), hunk.Text)
		diffCount++
	}
	if diffCount > 0 {
		var err error
		err = os.WriteFile(testName+"-got.yaml", []byte(got), 0644)
		if err != nil {
			t.Fatalf("cannot write got.yaml")
		}
		err = os.WriteFile(testName+"-exp.yaml", []byte(expected), 0644)
		if err != nil {
			t.Fatalf("cannot write exp.yaml")
		}
	}
}
