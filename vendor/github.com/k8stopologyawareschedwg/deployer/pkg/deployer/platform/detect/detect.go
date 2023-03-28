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
 * Copyright 2021 Red Hat, Inc.
 */

package detect

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

func Platform(ctx context.Context) (platform.Platform, error) {
	ocpCli, err := clientutil.NewOCPClientSet()
	if err != nil {
		return platform.Unknown, err
	}
	sccs, err := ocpCli.ConfigV1.ClusterVersions().List(ctx, metav1.ListOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return platform.Kubernetes, nil
		}
		return platform.Unknown, err
	}
	if len(sccs.Items) > 0 {
		return platform.OpenShift, nil
	}
	return platform.Kubernetes, nil
}

func Version(ctx context.Context, plat platform.Platform) (platform.Version, error) {
	if plat == platform.OpenShift {
		return OpenshiftVersion(ctx)
	}
	return KubernetesVersion(ctx)
}

// TODO: we need to wait for the client-go to be fixed to accept a context
func KubernetesVersion(_ context.Context) (platform.Version, error) {
	cli, err := clientutil.NewDiscoveryClient()
	if err != nil {
		return "", err
	}
	ver, err := cli.ServerVersion()
	if err != nil {
		return "", err
	}
	return platform.ParseVersion(ver.GitVersion)
}

func OpenshiftVersion(ctx context.Context) (platform.Version, error) {
	ocpCli, err := clientutil.NewOCPClientSet()
	if err != nil {
		return platform.MissingVersion, err
	}
	ocpApi, err := ocpCli.ConfigV1.ClusterOperators().Get(ctx, "openshift-apiserver", metav1.GetOptions{})
	if err != nil {
		return platform.MissingVersion, err
	}
	if len(ocpApi.Status.Versions) == 0 {
		return platform.MissingVersion, fmt.Errorf("unexpected amount of operands: %d", len(ocpApi.Status.Versions))
	}
	return platform.ParseVersion(ocpApi.Status.Versions[0].Version)
}
