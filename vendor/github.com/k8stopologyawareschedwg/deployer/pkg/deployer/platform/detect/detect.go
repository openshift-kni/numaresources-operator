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

	"github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil/nodes"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	ocpconfigv1 "github.com/openshift/api/config/v1"
)

func ControlPlane(ctx context.Context) (ControlPlaneInfo, error) {
	cli, err := clientutil.New()
	if err != nil {
		return ControlPlaneInfo{}, err
	}
	return ControlPlaneFromLister(ctx, cli)
}

func Platform(ctx context.Context) (platform.Platform, error) {
	ocpCli, err := clientutil.NewOCPClientSet()
	if err != nil {
		return platform.Unknown, err
	}
	return PlatformFromClients(ctx, ocpCli.ConfigV1.ClusterVersions(), ocpCli.ConfigV1.Infrastructures())
}

func Version(ctx context.Context, plat platform.Platform) (platform.Version, error) {
	if plat == platform.OpenShift || plat == platform.HyperShift {
		return OpenshiftVersion(ctx)
	}
	return KubernetesVersion(ctx)
}

// TODO: we need to wait for the client-go to be fixed to accept a context
func KubernetesVersion(ctx context.Context) (platform.Version, error) {
	env := platform.Environment{
		Environment: deployer.Environment{
			Ctx: context.Background(),
			Log: logr.Discard(), // TODO
		},
	}
	err := env.EnsureClient()
	if err != nil {
		return platform.MissingVersion, err
	}
	return KubernetesVersionFromEnv(&env)
}

func OpenshiftVersion(ctx context.Context) (platform.Version, error) {
	env := platform.Environment{
		Environment: deployer.Environment{
			Ctx: context.Background(),
			Log: logr.Discard(), // TODO
		},
	}
	err := env.EnsureClient()
	if err != nil {
		return platform.MissingVersion, err
	}
	return OpenshiftVersionFromEnv(&env)
}

// ControlPlaneFromLister is deprecated, use ControlPlaneFromEnv instead
func ControlPlaneFromLister(ctx context.Context, cli client.Client) (ControlPlaneInfo, error) {
	env := deployer.Environment{
		Ctx: ctx,
		Cli: cli,
		Log: logr.Discard(), // TODO
	}
	return ControlPlaneFromEnv(&env)
}

func ControlPlaneFromEnv(env *deployer.Environment) (ControlPlaneInfo, error) {
	info := ControlPlaneInfo{}
	nodes, err := nodes.GetControlPlane(env)
	if err != nil {
		return info, err
	}
	info.NodeCount = len(nodes)
	return info, nil
}

// PlatformFromLister is deprecated, use PlatformFromClients instead
func PlatformFromLister(ctx context.Context, cvLister platform.ClusterVersionsLister) (platform.Platform, error) {
	vers, err := cvLister.List(ctx, metav1.ListOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return platform.Kubernetes, nil
		}
		return platform.Unknown, err
	}
	if len(vers.Items) > 0 {
		return platform.OpenShift, nil
	}
	return platform.Kubernetes, nil
}

// PlatformFromClients is deprecated, use PlatformFromEnv instead
func PlatformFromClients(ctx context.Context, cvLister platform.ClusterVersionsLister, infraGetter platform.InfrastructuresGetter) (platform.Platform, error) {
	env := platform.Environment{
		Environment: deployer.Environment{
			Ctx: ctx,
			Log: logr.Discard(), // TODO
		},
		CVLister:    cvLister,
		InfraGetter: infraGetter,
	}
	return PlatformFromEnv(&env)
}

func PlatformFromEnv(env *platform.Environment) (platform.Platform, error) {
	vers, err := env.CVLister.List(env.Ctx, metav1.ListOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return platform.Kubernetes, nil
		}
		return platform.Unknown, err
	}
	if len(vers.Items) > 0 {
		infra, err := env.InfraGetter.Get(env.Ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			return platform.Unknown, err
		}
		if infra.Status.ControlPlaneTopology == ocpconfigv1.ExternalTopologyMode {
			return platform.HyperShift, nil
		}
		return platform.OpenShift, nil
	}
	return platform.Kubernetes, nil

}

func VersionFromEnv(env *platform.Environment, plat platform.Platform) (platform.Version, error) {
	if plat == platform.OpenShift || plat == platform.HyperShift {
		return OpenshiftVersionFromEnv(env)
	}
	return KubernetesVersionFromEnv(env)
}

func KubernetesVersionFromEnv(env *platform.Environment) (platform.Version, error) {
	ver, err := env.DiscCli.ServerVersion()
	if err != nil {
		return "", err
	}
	return platform.ParseVersion(ver.GitVersion)
}

func OpenshiftVersionFromEnv(env *platform.Environment) (platform.Version, error) {
	ocpApi, err := env.COGetter.Get(env.Ctx, "openshift-apiserver", metav1.GetOptions{})
	if err != nil {
		return platform.MissingVersion, err
	}
	if len(ocpApi.Status.Versions) == 0 {
		return platform.MissingVersion, fmt.Errorf("unexpected amount of operands: %d", len(ocpApi.Status.Versions))
	}
	return platform.ParseVersion(ocpApi.Status.Versions[0].Version)
}

// KubernetesVersionFromDiscovery is deprecated, use KubernetesVersionFromEnv instead
func KubernetesVersionFromDiscovery(ctx context.Context, cli discovery.ServerVersionInterface) (platform.Version, error) {
	env := platform.Environment{
		Environment: deployer.Environment{
			Ctx: ctx,
			Log: logr.Discard(), // TODO
		},
		DiscCli: cli,
	}
	return KubernetesVersionFromEnv(&env)
}

// OpenshiftVersionFromDiscovery is deprecated, use OpenshiftVersionFromEnv instead
func OpenshiftVersionFromGetter(ctx context.Context, coGetter platform.ClusterOperatorsGetter) (platform.Version, error) {
	env := platform.Environment{
		Environment: deployer.Environment{
			Ctx: ctx,
			Log: logr.Discard(), // TODO
		},
		COGetter: coGetter,
	}
	return OpenshiftVersionFromEnv(&env)
}
