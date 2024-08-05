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

package images

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	RTE string = "RTE"
	NFD string = "NFD"
)

type Output struct {
	TopologyUpdater     string `json:"topology_updater"`
	SchedulerPlugin     string `json:"scheduler_plugin"`
	SchedulerController string `json:"scheduler_controller"`
}

func NewOutput(imgs Images, updaterType string) Output {
	imo := Output{
		SchedulerPlugin:     imgs.SchedulerPluginScheduler,
		SchedulerController: imgs.SchedulerPluginController,
		TopologyUpdater:     getUpdaterImage(imgs, updaterType),
	}
	return imo
}

type List []string

func (imo Output) ToList() List {
	return []string{
		imo.TopologyUpdater,
		imo.SchedulerPlugin,
		imo.SchedulerController,
	}
}

const (
	FormatJSON = iota
	FormatText
)

type Formatter interface {
	Format(kind int, w io.Writer)
}

func (il List) EncodeText(w io.Writer) {
	fmt.Fprintf(w, "%s\n", strings.Join(il, "\n"))
}

func (il List) EncodeJSON(w io.Writer) {
	json.NewEncoder(os.Stdout).Encode(il)
}

func (il List) Format(kind int, w io.Writer) {
	if kind == FormatJSON {
		il.EncodeJSON(w)
	} else {
		il.EncodeText(w)
	}
}

func (imo Output) EncodeText(w io.Writer) {
	fmt.Fprintf(w, "TAS_SCHEDULER_PLUGIN_IMAGE=%s\n", imo.SchedulerPlugin)
	fmt.Fprintf(w, "TAS_SCHEDULER_PLUGIN_CONTROLLER_IMAGE=%s\n", imo.SchedulerController)
	fmt.Fprintf(w, "TAS_RESOURCE_EXPORTER_IMAGE=%s\n", imo.TopologyUpdater)
}

func (imo Output) EncodeJSON(w io.Writer) {
	json.NewEncoder(w).Encode(imo)
}

func (imo Output) Format(kind int, w io.Writer) {
	if kind == FormatJSON {
		imo.EncodeJSON(w)
	} else {
		imo.EncodeText(w)
	}
}

func getUpdaterImage(imgs Images, updaterType string) string {
	if updaterType == RTE {
		return imgs.ResourceTopologyExporter
	}
	return imgs.NodeFeatureDiscovery
}
