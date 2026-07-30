/*
 * Copyright Red Hat, Inc.
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

package sched

import (
	"context"
	"testing"

	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TestSchedulerImageValidation tests the image validation logic for the scheduler with default validation behavior, which is
// enabled at the time of writing this test. We could have added this test under sched/sched_test.go, but because that suite
// is disabling the validation at suite level, we would need to enable that again via updating the subcription multiple
// consecutive times, which proved in prow and local CI runs to be flaky in terms of the time it takes for the updates of
// the subscription environment variables to be reflected on the operator deployment, after consecutive updates of the same
// variable.

func TestScheduler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scheduler")
}

var _ = BeforeSuite(func(ctx context.Context) {
	Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")
})
