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

package normalize

import (
	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/v1alpha1"
	nodegroupv1alpha1 "github.com/openshift-kni/numaresources-operator/api/v1alpha1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

func NodeGroupTreesConfig(trees []nodegroupv1alpha1.Tree) {
	for idx := range trees {
		tree := &trees[idx]
		NodeGroupConfig(tree.NodeGroup)
	}
}

func NodeGroupConfig(nodeGroup *nropv1alpha1.NodeGroup) {
	conf := nropv1alpha1.DefaultNodeGroupConfig()
	if nodeGroup.DisablePodsFingerprinting != nil {
		// handle the bool added in 4.11. We will deprecate once we move out from v1alpha1.
		setNodeGroupConfigFromNodeGroup(&conf, *nodeGroup)
	}
	if nodeGroup.Config != nil {
		conf = merge.NodeGroupConfig(conf, *nodeGroup.Config)
	}
	nodeGroup.Config = &conf
}

func setNodeGroupConfigFromNodeGroup(conf *nropv1alpha1.NodeGroupConfig, ng nropv1alpha1.NodeGroup) {
	var podsFp nropv1alpha1.PodsFingerprintingMode
	if *ng.DisablePodsFingerprinting {
		podsFp = nropv1alpha1.PodsFingerprintingDisabled
	} else {
		podsFp = nropv1alpha1.PodsFingerprintingEnabled
	}
	conf.PodsFingerprinting = &podsFp
}
