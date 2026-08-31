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

package cluster

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"

	"github.com/openshift-kni/numaresources-operator/test/e2e/label"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func GetClusterType(ctx context.Context, cli client.Client) label.ClusterType {
	GinkgoHelper()

	allNodes := &corev1.NodeList{}
	Expect(cli.List(ctx, allNodes)).To(Succeed())

	mastersSchedulable := false
	schedulerCfg := &configv1.Scheduler{}
	err := cli.Get(ctx, client.ObjectKey{Name: "cluster"}, schedulerCfg)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).ToNot(HaveOccurred())
	}
	if err == nil {
		mastersSchedulable = schedulerCfg.Spec.MastersSchedulable
	}

	infraCfg := &configv1.Infrastructure{}
	err = cli.Get(ctx, client.ObjectKey{Name: "cluster"}, infraCfg)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).ToNot(HaveOccurred())
	}

	if err == nil && infraCfg.Status.ControlPlaneTopology == configv1.HighlyAvailableTopologyMode && mastersSchedulable {
		if len(allNodes.Items) == 3 {
			return label.Compact
		}
		return label.MNOMastersSchedulable
	}
	return label.MNO
}
