/*
 * Copyright 2025 Red Hat, Inc.
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

package sched

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/flagcodec"
	k8swgobjupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate"
)

const (
	logTracrMountName = "run-logtracr"
	logTracrDirectory = "/run/logtracr"
)

func ToggleLogTracingParameters(podSpec *corev1.PodSpec, enabled bool) error {
	cnt := k8swgobjupdate.FindContainerByName(podSpec.Containers, MainContainerName)
	if cnt == nil {
		return fmt.Errorf("cannot find container data for %q", MainContainerName)
	}
	flags := flagcodec.ParseArgvKeyValue(cnt.Args, flagcodec.WithFlagNormalization)
	if flags == nil {
		return fmt.Errorf("cannot modify the arguments for container %s", cnt.Name)
	}

	// Set only the master flag. Use builtin defaults for all the other settings for now.
	if enabled {
		flags.SetOption("--log-tracr-directory", logTracrDirectory)
	} else {
		flags.Delete("--log-tracr-directory")
	}

	cnt.Args = flags.Argv()
	return nil
}
