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

package tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2/textlogger"
	"sigs.k8s.io/controller-runtime/pkg/client"

	depwait "github.com/k8stopologyawareschedwg/deployer/pkg/deployer/wait"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/wait"

	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/images"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
)

var _ = Describe("[serial][disruptive][scheduler][schedrst] numaresources scheduler removal on a live cluster", Serial, Label("disruptive", "scheduler", "schedrst"), Label("feature:schedrst"), func() {
	var fxt *e2efixture.Fixture
	var nroSchedObj *nropv1.NUMAResourcesScheduler
	var config *textlogger.Config
	var dpNName nropv1.NamespacedName

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		config = textlogger.NewConfig(textlogger.Verbosity(1))

		var err error
		fxt, err = e2efixture.Setup("e2e-test-sched-remove", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		nroSchedObj = nrosched.CheckNROSchedulerAvailable(fxt.Client, serialconfig.Config.NROSchedObj.Name)
		Expect(nroSchedObj).ToNot(BeNil())
		dpNName = nroSchedObj.Status.Deployment

	})

	AfterEach(func() {
		err := e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	When("removing the topology aware scheduler from a live cluster", func() {
		AfterEach(func() {
			restoreScheduler(fxt, serialconfig.Config.NROSchedObj)
			nroSchedObj = nrosched.CheckNROSchedulerAvailable(fxt.Client, serialconfig.Config.NROSchedObj.Name)
			Expect(nroSchedObj).ToNot(BeNil())
		})

		It("[case:1][test_id:47593] should keep existing workloads running", Label(label.Tier1), func() {
			var err error

			dp := createDeploymentSync(fxt, "testdp", serialconfig.Config.SchedulerName)

			By(fmt.Sprintf("deleting the NRO Scheduler object: %s", serialconfig.Config.NROSchedObj.Name))
			err = fxt.Client.Delete(context.TODO(), serialconfig.Config.NROSchedObj)
			Expect(err).ToNot(HaveOccurred())

			// make sure scheduler deployment is gone
			err = depwait.With(fxt.Client, textlogger.NewLogger(config)).Interval(10*time.Second).Timeout(time.Minute).ForDeploymentDeleted(context.TODO(), dpNName.Namespace, dpNName.Name)
			Expect(err).ToNot(HaveOccurred())

			maxStep := 3
			for step := 0; step < maxStep; step++ {
				time.Sleep(10 * time.Second)

				By(fmt.Sprintf("ensuring the deployment %q keep being ready %d/%d", dp.Name, step+1, maxStep))

				updatedDp := &appsv1.Deployment{}
				err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(dp), updatedDp)
				Expect(err).ToNot(HaveOccurred())

				Expect(wait.IsDeploymentComplete(dp, &updatedDp.Status)).To(BeTrue(), "deployment %q become unready", dp.Name)
			}
		})

		It("[case:2][test_id:49093]should keep new scheduled workloads pending", Label(label.Tier1, "unsched", "feature:unsched"), func() {
			var err error

			By(fmt.Sprintf("deleting the NRO Scheduler object: %s", serialconfig.Config.NROSchedObj.Name))
			err = fxt.Client.Delete(context.TODO(), serialconfig.Config.NROSchedObj)
			Expect(err).ToNot(HaveOccurred())

			// make sure scheduler deployment is gone
			err = depwait.With(fxt.Client, textlogger.NewLogger(config)).Interval(10*time.Second).Timeout(time.Minute).ForDeploymentDeleted(context.TODO(), dpNName.Namespace, dpNName.Name)
			Expect(err).ToNot(HaveOccurred())

			dp := createDeployment(fxt, "testdp", serialconfig.Config.SchedulerName)

			maxStep := 3
			for step := 0; step < maxStep; step++ {
				time.Sleep(10 * time.Second)

				By(fmt.Sprintf("ensuring the deployment %q keep being pending %d/%d", dp.Name, step+1, maxStep))

				updatedDp := &appsv1.Deployment{}
				err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(dp), updatedDp)
				Expect(err).ToNot(HaveOccurred())

				Expect(wait.IsDeploymentComplete(dp, &updatedDp.Status)).To(BeFalse(), "deployment %q become ready", dp.Name)
			}
		})
	})

	When("restarting the topology aware scheduler in a live cluster", func() {
		It("[case:1][test_id:48069] should schedule any pending workloads submitted while the scheduler was unavailable", Label(label.Tier2), func() {
			var err error

			By(fmt.Sprintf("deleting the NRO Scheduler object: %s", nroSchedObj.Name))
			err = fxt.Client.Delete(context.TODO(), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())

			// make sure scheduler deployment is gone
			err = depwait.With(fxt.Client, textlogger.NewLogger(config)).Interval(10*time.Second).Timeout(time.Minute).ForDeploymentDeleted(context.TODO(), dpNName.Namespace, dpNName.Name)
			Expect(err).ToNot(HaveOccurred())

			dp := createDeployment(fxt, "testdp", nroSchedObj.Status.SchedulerName)

			updatedDp := &appsv1.Deployment{}
			maxStep := 3
			for step := 0; step < maxStep; step++ {
				time.Sleep(10 * time.Second)

				By(fmt.Sprintf("ensuring the deployment %q keep being pending %d/%d", dp.Name, step+1, maxStep))

				err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(dp), updatedDp)
				Expect(err).ToNot(HaveOccurred())

				Expect(wait.IsDeploymentComplete(dp, &updatedDp.Status)).To(BeFalse(), "deployment %q become ready", dp.Name)
			}

			restoreScheduler(fxt, nroSchedObj)
			nroSchedObj = nrosched.CheckNROSchedulerAvailable(fxt.Client, nroSchedObj.Name)
			Expect(nroSchedObj).ToNot(BeNil())

			By(fmt.Sprintf("waiting for the test deployment %q to become complete and ready", updatedDp.Name))
			_, err = wait.With(fxt.Client).Interval(2*time.Second).Interval(30*time.Second).ForDeploymentComplete(context.TODO(), updatedDp)
			Expect(err).ToNot(HaveOccurred())
		})
	})

})

func restoreScheduler(fxt *e2efixture.Fixture, nroSchedObj *nropv1.NUMAResourcesScheduler) {
	By(fmt.Sprintf("re-creating the NRO Scheduler object: %s", nroSchedObj.Name))
	nroSched := &nropv1.NUMAResourcesScheduler{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NUMAResourcesScheduler",
			APIVersion: nropv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: nroSchedObj.Name,
		},
		Spec: nroSchedObj.Spec,
	}

	err := fxt.Client.Create(context.TODO(), nroSched)
	Expect(err).NotTo(HaveOccurred())
}

func createDeployment(fxt *e2efixture.Fixture, name, schedulerName string) *appsv1.Deployment {
	var err error
	var replicas int32 = 2

	podLabels := map[string]string{
		"test": "test-dp",
	}
	nodeSelector := map[string]string{}
	dp := objects.NewTestDeployment(replicas, podLabels, nodeSelector, fxt.Namespace.Name, name, images.GetPauseImage(), []string{images.PauseCommand}, []string{})
	dp.Spec.Template.Spec.SchedulerName = schedulerName

	By(fmt.Sprintf("creating a test deployment %q", name))
	err = fxt.Client.Create(context.TODO(), dp)
	Expect(err).ToNot(HaveOccurred())

	return dp
}

func createDeploymentSync(fxt *e2efixture.Fixture, name, schedulerName string) *appsv1.Deployment {
	dp := createDeployment(fxt, name, schedulerName)
	By(fmt.Sprintf("waiting for the test deployment %q to be complete and ready", name))
	_, err := wait.With(fxt.Client).Interval(10*time.Second).Timeout(time.Minute).ForDeploymentComplete(context.TODO(), dp)
	Expect(err).ToNot(HaveOccurred(), "Deployment %q is not up & running after %v", dp.Name, time.Minute)
	return dp
}
