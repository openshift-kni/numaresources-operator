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

package clientutil

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
)

func init() {
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme.Scheme))
}

// New returns a controller-runtime client.
func New() (client.Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	cli, err := client.New(cfg, client.Options{})
	return cli, err
}

// NewK8s returns a kubernetes clientset
func NewK8s() (*kubernetes.Clientset, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

func NewDiscoveryClient() (*discovery.DiscoveryClient, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	cli, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return cli, nil
}

type OCPClientSet struct {
	ConfigV1 *configv1.ConfigV1Client
}

func NewOCPClientSet() (*OCPClientSet, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	configclient, err := configv1.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &OCPClientSet{
		ConfigV1: configclient,
	}, nil
}
