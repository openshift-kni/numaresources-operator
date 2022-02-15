/*
Copyright 2022.

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

package validators

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/version"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
	"github.com/k8stopologyawareschedwg/deployer/pkg/validator"
	"github.com/openshift-kni/numaresources-operator/controllers"
	"github.com/openshift-kni/numaresources-operator/pkg/kubeletconfig"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

type KubeletConfigDataGatherer interface {
	CollecData() (*kubeletConfigValidatorData, error)
}

type k8KubeletConfigDataGatherer struct {
	conn    k8sConnection
	nroName string
}

func (gatherer *k8KubeletConfigDataGatherer) CollecData() (*kubeletConfigValidatorData, error) {

	// Get Node Names for those nodes with TAS enabled
	nroNamespacedName := types.NamespacedName{
		Name: controllers.DefaultNUMAResourcesOperatorCrName,
	}
	nroInstance := &nropv1alpha1.NUMAResourcesOperator{}
	err := gatherer.conn.client.Get(gatherer.conn.context, nroNamespacedName, nroInstance)
	if err != nil {
		return nil, err
	}

	nroMcps, err := machineconfigpools.GetNodeGroupsMCPs(gatherer.conn.context, gatherer.conn.client, nroInstance.Spec.NodeGroups)
	if err != nil {
		return nil, err
	}

	data := &kubeletConfigValidatorData{}

	data.tasEnabledNodeNames = sets.NewString()
	for _, mcp := range nroMcps {
		nodes, err := machineconfigpools.GetNodeListFromMachineConfigPool(gatherer.conn.context, gatherer.conn.client, *mcp)
		if err != nil {
			return nil, err
		}
		data.tasEnabledNodeNames.Insert(getNodeNames(nodes)...)
	}

	mcoKubeletConfigList := mcov1.KubeletConfigList{}
	if err := gatherer.conn.client.List(gatherer.conn.context, &mcoKubeletConfigList); err != nil {
		return nil, err
	}

	// Get MCO::KubeletConfiguration data for nodes.
	data.kConfigs = make(map[string]*kubeletconfigv1beta1.KubeletConfiguration)
	for _, mcoKubeletConfig := range mcoKubeletConfigList.Items {
		kubeletConfig, err := kubeletconfig.MCOKubeletConfToKubeletConf(&mcoKubeletConfig)
		if err != nil {
			return nil, err
		}

		machineConfigPools, err := kubeletconfig.GetMCPsFromMCOKubeletConfig(gatherer.conn.context, gatherer.conn.client, mcoKubeletConfig)
		if err != nil {
			return nil, err
		}
		for _, mcp := range machineConfigPools {

			nodes, err := machineconfigpools.GetNodeListFromMachineConfigPool(gatherer.conn.context, gatherer.conn.client, *mcp)
			if err != nil {
				return nil, err
			}
			nodeNames := getNodeNames(nodes)

			for _, nodeName := range nodeNames {
				// we are just interested on nodes with TAS enabled
				if !data.tasEnabledNodeNames.Has(nodeName) {
					continue
				}

				_, found := data.kConfigs[nodeName]
				if found {
					return nil, fmt.Errorf("Found two KubeletConfigurations for node %q", nodeName)
				}
				data.kConfigs[nodeName] = kubeletConfig
			}
		}
	}

	// get Version Info
	ver, err := getClusterVersionInfo()
	if err != nil {
		return nil, err
	}

	data.versionInfo = ver
	return data, nil
}

type KubeletConfigValidator struct {
	connection   k8sConnection
	data         kubeletConfigValidatorData
	dataGatherer KubeletConfigDataGatherer
}
type k8sConnection struct {
	client  client.Client
	context context.Context
}
type kubeletConfigValidatorData struct {
	tasEnabledNodeNames sets.String
	kConfigs            map[string]*kubeletconfigv1beta1.KubeletConfiguration
	versionInfo         *version.Info
}

func NewKubeletConfigValidator(aContext context.Context, aClient client.Client) *KubeletConfigValidator {
	defaultGatherer := &k8KubeletConfigDataGatherer{
		conn: k8sConnection{
			client:  aClient,
			context: aContext,
		},
		nroName: controllers.DefaultNUMAResourcesOperatorCrName,
	}
	return NewKubeletConfigValidatorWithDataGatherer(aContext, aClient, defaultGatherer)
}

func NewKubeletConfigValidatorWithDataGatherer(aContext context.Context, aClient client.Client, aDataGatherer KubeletConfigDataGatherer) *KubeletConfigValidator {

	ret := &KubeletConfigValidator{
		connection: k8sConnection{
			context: aContext,
			client:  aClient,
		},
		dataGatherer: aDataGatherer,
	}
	return ret
}

func (kvalidator *KubeletConfigValidator) Setup() error {

	data, err := kvalidator.dataGatherer.CollecData()
	if err != nil {
		return err
	}
	kvalidator.data = *data
	return nil
}

func (kvalidator *KubeletConfigValidator) Validate() ([]validator.ValidationResult, error) {

	var ret []validator.ValidationResult
	for _, nodeName := range kvalidator.data.tasEnabledNodeNames.List() {
		kc := kvalidator.data.kConfigs[nodeName]
		// if a TAS enabled node has no MCO kubelet configuration kc will be nil and ValidateClusterNodeKubeletConfig will fill the proper error
		res := validator.ValidateClusterNodeKubeletConfig(nodeName, kvalidator.data.versionInfo, kc)
		ret = append(ret, res...)
	}
	return ret, nil
}

func (kvalidator *KubeletConfigValidator) Teardown() error {
	// nothing to do
	return nil
}

func getClusterVersionInfo() (*version.Info, error) {
	cli, err := clientutil.NewDiscoveryClient()
	if err != nil {
		return nil, err
	}
	ver, err := cli.ServerVersion()
	if err != nil {
		return nil, err
	}
	return ver, nil
}

func getNodeNames(nodes []corev1.Node) []string {
	var nodeNames []string
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}

	return nodeNames
}
