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
 * Copyright 2024 Red Hat, Inc.
 */

package options

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/platform"
)

type SCCVersion string

const (
	SCCV1 SCCVersion = "v1"
	SCCV2 SCCVersion = "v2"
)

type DaemonSet struct {
	Verbose            int
	PullIfNotPresent   bool
	PFPEnable          bool
	NotificationEnable bool
	NodeSelector       *metav1.LabelSelector
	UpdateInterval     time.Duration
	SCCVersion         SCCVersion
}

type UpdaterDaemon struct {
	DaemonSet                 DaemonSet
	MachineConfigPoolSelector *metav1.LabelSelector
	ConfigData                string
	Namespace                 string
	Name                      string
}

type Render struct {
	Platform            platform.Platform
	PlatformVersion     platform.Version
	Namespace           string
	EnableCRIHooks      bool
	CustomSELinuxPolicy bool
}
