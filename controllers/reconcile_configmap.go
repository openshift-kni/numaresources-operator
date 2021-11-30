/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ghodss/yaml"
	"github.com/pkg/errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	cfgstate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/cfg"
	rtestate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
	"github.com/openshift-kni/numaresources-operator/rte/pkg/sysinfo"
)

const (
	ConfigDataField = "config.yaml"
)

type configMapHelper struct {
	cli       client.Client
	scm       *runtime.Scheme
	instance  *nropv1alpha1.NUMAResourcesOperator
	namespace string
}

func (cmh *configMapHelper) reconcileConfigMap(ctx context.Context, kcKey client.ObjectKey) (*corev1.ConfigMap, error) {
	mcoKc := &mcov1.KubeletConfig{}
	if err := cmh.cli.Get(ctx, kcKey, mcoKc); err != nil {
		return nil, err
	}

	kubeletConfig, err := mcoKubeletConfToKubeletConf(mcoKc)
	if err != nil {
		return nil, err
	}

	mcps, err := getNodeGroupsMCPs(ctx, cmh.cli, cmh.instance.Spec.NodeGroups)
	if err != nil {
		return nil, err
	}

	mcp, err := findMCPForKubeletConfig(mcps, mcoKc)
	if err != nil {
		return nil, err
	}

	generatedName := rtestate.GetComponentName(cmh.instance.Name, mcp.Name)
	return cmh.syncConfigMap(ctx, kubeletConfig, generatedName)
}

func (cmh *configMapHelper) syncConfigMap(ctx context.Context, kubeletConfig *kubeletconfigv1beta1.KubeletConfiguration, name string) (*corev1.ConfigMap, error) {
	rendered, err := renderRTEConfig(cmh.namespace, name, kubeletConfig)
	if err != nil {
		klog.ErrorS(err, "rendering config", "namespace", cmh.namespace, "name", name)
		return nil, err
	}

	cfgManifests := cfgstate.Manifests{
		Config: rendered,
	}
	existing := cfgstate.FromClient(context.TODO(), cmh.cli, cmh.namespace, name)
	for _, objState := range existing.State(cfgManifests) {
		if err := controllerutil.SetControllerReference(cmh.instance, objState.Desired, cmh.scm); err != nil {
			return nil, errors.Wrapf(err, "Failed to set controller reference to %s %s", objState.Desired.GetNamespace(), objState.Desired.GetName())
		}
		if _, err := apply.ApplyObject(context.TODO(), cmh.cli, objState); err != nil {
			return nil, errors.Wrapf(err, "could not create %s", objState.Desired.GetObjectKind().GroupVersionKind().String())
		}
	}
	return rendered, nil
}

func findMCPForKubeletConfig(mcps []*machineconfigv1.MachineConfigPool, mcoKc *mcov1.KubeletConfig) (*machineconfigv1.MachineConfigPool, error) {
	for _, mcp := range mcps {
		if mcoKc.Spec.MachineConfigPoolSelector == nil {
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(mcoKc.Spec.MachineConfigPoolSelector)
		if err != nil {
			return nil, err
		}

		if selector.Matches(labels.Set(mcp.Labels)) {
			return mcp, nil
		}
	}
	return nil, fmt.Errorf("no match")
}

func mcoKubeletConfToKubeletConf(mcoKc *mcov1.KubeletConfig) (*kubeletconfigv1beta1.KubeletConfiguration, error) {
	kc := &kubeletconfigv1beta1.KubeletConfiguration{}
	err := json.Unmarshal(mcoKc.Spec.KubeletConfig.Raw, kc)
	return kc, err
}

func renderRTEConfig(namespace, name string, klConfig *kubeletconfigv1beta1.KubeletConfiguration) (*corev1.ConfigMap, error) {
	conf := rteconfig.Config{
		Resources: sysinfo.Config{
			ReservedCPUs: klConfig.ReservedSystemCPUs,
		},
		TopologyManagerPolicy: klConfig.TopologyManagerPolicy,
		TopologyManagerScope:  klConfig.TopologyManagerScope,
	}
	data, err := yaml.Marshal(conf)
	if err != nil {
		return nil, err
	}
	return rtemanifests.CreateConfigMap(namespace, name, string(data)), nil
}
