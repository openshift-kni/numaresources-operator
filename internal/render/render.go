/*
 * Copyright 2021 Red Hat, Inc.
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

package render

import (
	"fmt"
	"os"
	"time"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	"github.com/k8stopologyawareschedwg/deployer/pkg/options"

	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	"github.com/openshift-kni/numaresources-operator/pkg/images"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/rte"
	schedupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/sched"
)

type Params struct {
	NRTCRD         bool
	Namespace      string
	ExporterImage  string
	SchedulerImage string
}

func Objects(objs []client.Object) error {
	for _, obj := range objs {
		fmt.Printf("---\n")
		if err := manifests.SerializeObject(obj, os.Stdout); err != nil {
			return err
		}
	}

	return nil
}

// RTEManifests renders the reconciler manifests so they can be deployed on the cluster.
func RTEManifests(rteManifests rtemanifests.Manifests, namespace string, imgs images.Data) (rtemanifests.Manifests, error) {
	klog.InfoS("Updating RTE manifests")
	mf, err := rteManifests.Render(options.UpdaterDaemon{
		Namespace: namespace,
		DaemonSet: options.DaemonSet{
			Verbose:            2,
			NotificationEnable: true,
			UpdateInterval:     10 * time.Second,
		},
	})
	if err != nil {
		return mf, err
	}

	err = rteupdate.DaemonSetUserImageSettings(mf.DaemonSet, imgs.Discovered(), imgs.Builtin, images.NullPolicy)
	if err != nil {
		return mf, err
	}

	err = rteupdate.DaemonSetPauseContainerSettings(mf.DaemonSet)
	if err != nil {
		return mf, err
	}
	if mf.ConfigMap != nil {
		rteupdate.DaemonSetHashAnnotation(mf.DaemonSet, hash.ConfigMapData(mf.ConfigMap))
	}
	return mf, err
}

func SchedulerManifests(schedManifests schedmanifests.Manifests, imageSpec string) (schedmanifests.Manifests, error) {
	klog.InfoS("Updating scheduler manifests")
	mf := schedManifests.Clone()
	schedupdate.DeploymentImageSettings(mf.Deployment, imageSpec)
	// empty string is fine. Will be handled as "disabled".
	// We only care about setting the environ variable to declare it exists,
	// the best setting is "present, but disabled" vs "missing, thus implicitly disabled"
	schedupdate.DeploymentConfigMapSettings(mf.Deployment, schedManifests.ConfigMap.Name, hash.ConfigMapData(schedManifests.ConfigMap))
	return mf, nil
}
