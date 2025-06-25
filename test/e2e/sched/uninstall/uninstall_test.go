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

package uninstall

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[Scheduler] uninstall", func() {
	Context("with a running cluster with all the components", func() {
		It("should delete all components after NROScheduler deletion", func() {
			By("deleting the NROScheduler object")
			nroSchedObj := objects.TestNROScheduler()

			// failed to get the NROScheduler object, nothing else we can do
			if err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj); err != nil {
				if !errors.IsNotFound(err) {
					klog.ErrorS(err, "failed to get the NUMA resource scheduler", "name", nroSchedObj.Name)
				}
				return
			}

			deploy, err := podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), nroSchedObj.GetUID())
			Expect(err).ToNot(HaveOccurred())

			err = e2eclient.Client.Delete(context.TODO(), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())

			By("checking there are no leftovers")
			// by taking the ns from the deployment we're avoiding the need to figure out in advanced
			// at which ns we should look for the resources
			mf, err := sched.GetManifests(deploy.Namespace)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				objs := mf.ToObjects()
				for _, obj := range objs {
					key := client.ObjectKeyFromObject(obj)
					if err := e2eclient.Client.Get(context.TODO(), key, obj); !errors.IsNotFound(err) {
						if err == nil {
							klog.Infof("obj %s still exists", key.String())
						} else {
							klog.ErrorS(err, "obj return with error", "key", key.String())
						}
						return false
					}
				}
				return true
			}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
		})
	})
})
