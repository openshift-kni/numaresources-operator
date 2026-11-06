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

package nodegroup

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	intreconcile "github.com/openshift-kni/numaresources-operator/internal/reconcile"
)

// PoolDaemonSet a struct to hold the target MCP of a configured node group and its created respective RTE daemonset
type PoolDaemonSet struct {
	PoolName  string
	DaemonSet nropv1.NamespacedName
}

func CollectDaemonSets(dsInfos []PoolDaemonSet) []nropv1.NamespacedName {
	dssReady := make([]nropv1.NamespacedName, 0, len(dsInfos))
	for _, dsInfo := range dsInfos {
		dssReady = append(dssReady, dsInfo.DaemonSet)
	}
	return dssReady
}

type PerTreeResult struct {
	DSInfo         []PoolDaemonSet
	NROMCPs        []nropv1.MachineConfigPool
	PausedMCPNames sets.Set[string]
	Step           intreconcile.Step
}

func ReducePerTreeResults(results []PerTreeResult) PerTreeResult {
	acc := PerTreeResult{
		PausedMCPNames: sets.New[string](),
		Step:           intreconcile.StepSuccess(),
	}

	var errorCount, ongoingCount int
	for _, result := range results {
		acc.NROMCPs = append(acc.NROMCPs, result.NROMCPs...)
		acc.PausedMCPNames = acc.PausedMCPNames.Union(result.PausedMCPNames)

		if result.Step.Done() {
			acc.DSInfo = append(acc.DSInfo, result.DSInfo...)
			continue
		}

		if result.Step.Failed() {
			errorCount++
		} else if result.Step.Ongoing() {
			ongoingCount++
		}

		if ShouldReplaceStep(acc.Step, result.Step) {
			acc.Step = result.Step
		}
	}
	if !acc.Step.Done() {
		acc.Step = acc.Step.UpdateMessage(treeSummaryMessage(len(results), errorCount, ongoingCount))
	}
	return acc
}

func ShouldReplaceStep(current, candidate intreconcile.Step) bool {
	if current.Done() {
		return true
	}
	if candidate.Failed() && !current.Failed() {
		return true
	}
	if candidate.Ongoing() && current.Ongoing() {
		if candidate.Result.RequeueAfter != current.Result.RequeueAfter {
			return candidate.Result.RequeueAfter > 0 && candidate.Result.RequeueAfter < current.Result.RequeueAfter
		}
		if candidate.ConditionInfo.Message != "" && candidate.ConditionInfo.Message != current.ConditionInfo.Message {
			return true
		}
	}
	return false
}

func treeSummaryMessage(total, errors, ongoing int) string {
	done := total - errors - ongoing
	parts := make([]string, 0, 3)
	if done > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d completed", done, total))
	}
	if ongoing > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d updating", ongoing, total))
	}
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d failed", errors, total))
	}
	return strings.Join(parts, ", ")
}
