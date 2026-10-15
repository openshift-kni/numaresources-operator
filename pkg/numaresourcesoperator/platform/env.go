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

package platform

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"

	ocpconfigv1 "github.com/openshift/api/config/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
)

type ClusterOperatorsGetter interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*ocpconfigv1.ClusterOperator, error)
}

type InfrastructuresGetter interface {
	Get(ctx context.Context, name string, options metav1.GetOptions) (*ocpconfigv1.Infrastructure, error)
}

type ClusterVersionsLister interface {
	List(ctx context.Context, opts metav1.ListOptions) (*ocpconfigv1.ClusterVersionList, error)
}

type Environment struct {
	deployer.Environment
	DiscCli     discovery.ServerVersionInterface
	COGetter    ClusterOperatorsGetter
	CVLister    ClusterVersionsLister
	InfraGetter InfrastructuresGetter
}

func (env *Environment) EnsureClient() error {
	if err := env.Environment.EnsureClient(); err != nil {
		return err
	}
	if env.DiscCli == nil {
		cli, err := clientutil.NewDiscoveryClient()
		if err != nil {
			return err
		}
		env.DiscCli = cli
	}

	var err error
	var ocpCli *clientutil.OCPClientSet
	if env.COGetter == nil {
		if ocpCli == nil {
			ocpCli, err = clientutil.NewOCPClientSet()
			if err != nil {
				return err
			}
		}
		env.COGetter = ocpCli.ConfigV1.ClusterOperators()
	}
	if env.CVLister == nil {
		if ocpCli == nil {
			ocpCli, err = clientutil.NewOCPClientSet()
			if err != nil {
				return err
			}
		}
		env.CVLister = ocpCli.ConfigV1.ClusterVersions()
	}
	if env.InfraGetter == nil {
		if ocpCli == nil {
			ocpCli, err = clientutil.NewOCPClientSet()
			if err != nil {
				return err
			}
		}
		env.InfraGetter = ocpCli.ConfigV1.Infrastructures()
	}
	return nil
}
