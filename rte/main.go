/*
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

package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"text/template"
	"time"

	"github.com/docopt/docopt-go"

	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/nrtupdater"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podrescli"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcemonitor"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcetopologyexporter"

	"github.com/openshift-kni/resource-topology-exporter/pkg/config"
	"github.com/openshift-kni/resource-topology-exporter/pkg/podrescompat"
	"github.com/openshift-kni/resource-topology-exporter/pkg/sysinfo"
)

const (
	// ProgramName is the canonical name of this program
	ProgramName = "resource-topology-exporter"
)

type localArgs struct {
	SysConf sysinfo.Config
}

func main() {
	nrtupdaterArgs, resourcemonitorArgs, rteArgs, localArgs, err := argsParse(os.Args[1:])
	if err != nil {
		log.Fatalf("failed to parse command line: %v", err)
	}

	// only for debug purposes
	// printing the header so early includes any debug message from the sysinfo package
	log.Printf("=== System information ===\n")
	sysInfo, err := sysinfo.NewSysinfo(localArgs.SysConf)
	if err != nil {
		log.Fatalf("failed to query system info: %v", err)
	}
	log.Printf("%s", sysInfo)
	log.Printf("==========================\n")

	k8sCli, err := podrescli.NewK8SClient(rteArgs.PodResourcesSocketPath)
	if err != nil {
		log.Fatalf("failed to get podresources k8s client: %v", err)
	}

	sysCli := k8sCli
	if !localArgs.SysConf.IsEmpty() {
		sysCli = podrescompat.NewSysinfoClientFromLister(k8sCli, localArgs.SysConf)
	}

	cli, err := podrescli.NewFilteringClientFromLister(sysCli, rteArgs.Debug, rteArgs.ReferenceContainer)
	if err != nil {
		log.Fatalf("failed to get podresources filtering client: %v", err)
	}

	err = resourcetopologyexporter.Execute(cli, nrtupdaterArgs, resourcemonitorArgs, rteArgs)
	log.Fatalf("failed to execute: %v", err)
}

const helpTemplate string = `{{.ProgramName}}

  Usage:
  {{.ProgramName}}	[--debug]
                        [--no-publish]
			[--oneshot | --sleep-interval=<seconds>]
			[--podresources-socket=<path>]
			[--export-namespace=<namespace>]
			[--watch-namespace=<namespace>]
			[--sysfs=<mountpoint>]
			[--kubelet-state-dir=<path>...]
			[--kubelet-config-file=<path>]
			[--topology-manager-policy=<pol>]
			[--reference-container=<spec>]
			[--config=<path>]

  {{.ProgramName}} -h | --help
  {{.ProgramName}} --version

  Options:
  -h --help                       Show this screen.
  --debug                         Enable debug output. [Default: false]
  --version                       Output version and exit.
  --no-publish                    Do not publish discovered features to the
                                  cluster-local Kubernetes API server.
  --hostname                      Override the node hostname.
  --oneshot                       Update once and exit.
  --sleep-interval=<seconds>      Time to sleep between podresources API polls.
                                  [Default: 60s]
  --export-namespace=<namespace>  Namespace on which update CRDs. Use "" for all namespaces.
  --watch-namespace=<namespace>   Namespace to watch pods for. Use "" for all namespaces.
  --sysfs=<path>                  Top-level component path of sysfs. [Default: /sys]
  --kubelet-config-file=<path>    Kubelet config file path. [Default: ]
  --topology-manager-policy=<pol> Explicitely set the topology manager policy instead of reading
                                  from the kubelet. [Default: ]
  --kubelet-state-dir=<path>...   Kubelet state directory (RO access needed), for smart polling.
  --podresources-socket=<path>    Pod Resource Socket path to use.
                                  [Default: unix:///podresources/kubelet.sock]
  --reference-container=<spec>    Reference container, used to learn about the shared cpu pool
                                  See: https://github.com/kubernetes/kubernetes/issues/102190
                                  format of spec is namespace/podname/containername.
                                  Alternatively, you can use the env vars
                                  REFERENCE_NAMESPACE, REFERENCE_POD_NAME, REFERENCE_CONTAINER_NAME.
  --config=<path>                 Configuration file path. Use this to set the exclude list.
                                  [Default: /etc/resource-topology-exporter/config.yaml]`

func getUsage() (string, error) {
	var helpBuffer bytes.Buffer
	helpData := struct {
		ProgramName string
	}{
		ProgramName: ProgramName,
	}

	tmpl, err := template.New("help").Parse(helpTemplate)
	if err != nil {
		return "", err
	}
	err = tmpl.Execute(&helpBuffer, helpData)
	if err != nil {
		return "", err
	}

	return helpBuffer.String(), nil
}

// nrtupdaterArgsParse parses the command line arguments passed to the program.
// The argument argv is passed only for testing purposes.
func argsParse(argv []string) (nrtupdater.Args, resourcemonitor.Args, resourcetopologyexporter.Args, localArgs, error) {
	var nrtupdaterArgs nrtupdater.Args
	var resourcemonitorArgs resourcemonitor.Args
	var rteArgs resourcetopologyexporter.Args
	var localArgs localArgs

	usage, err := getUsage()
	if err != nil {
		return nrtupdaterArgs, resourcemonitorArgs, rteArgs, localArgs, err
	}

	arguments, _ := docopt.ParseArgs(usage, argv, fmt.Sprintf("%s %s", ProgramName, "TBD"))

	// Parse argument values as usable types.
	nrtupdaterArgs.NoPublish = arguments["--no-publish"].(bool)
	nrtupdaterArgs.Oneshot = arguments["--oneshot"].(bool)
	if hostname, ok := arguments["--hostname"].(string); ok {
		nrtupdaterArgs.Hostname = hostname
	}
	if nrtupdaterArgs.Hostname == "" {
		var err error
		nrtupdaterArgs.Hostname = os.Getenv("NODE_NAME")
		if nrtupdaterArgs.Hostname == "" {
			nrtupdaterArgs.Hostname, err = os.Hostname()
			if err != nil {
				return nrtupdaterArgs, resourcemonitorArgs, rteArgs, localArgs, fmt.Errorf("error getting the host name: %w", err)
			}
		}
	}

	rteArgs.SleepInterval, err = time.ParseDuration(arguments["--sleep-interval"].(string))
	if err != nil {
		return nrtupdaterArgs, resourcemonitorArgs, rteArgs, localArgs, fmt.Errorf("invalid --sleep-interval specified: %w", err)
	}
	if ns, ok := arguments["--watch-namespace"].(string); ok {
		resourcemonitorArgs.Namespace = ns
	}
	if kubeletConfigPath, ok := arguments["--kubelet-config-file"].(string); ok {
		rteArgs.KubeletConfigFile = kubeletConfigPath
	}
	resourcemonitorArgs.SysfsRoot = arguments["--sysfs"].(string)
	if path, ok := arguments["--podresources-socket"].(string); ok {
		rteArgs.PodResourcesSocketPath = path
	}

	if kubeletStateDirs, ok := arguments["--kubelet-state-dir"].([]string); ok {
		rteArgs.KubeletStateDirs = kubeletStateDirs
	}

	rteArgs.Debug = arguments["--debug"].(bool)
	if refCnt, ok := arguments["--reference-container"].(string); ok {
		rteArgs.ReferenceContainer, err = podrescli.ContainerIdentFromString(refCnt)
		if err != nil {
			return nrtupdaterArgs, resourcemonitorArgs, rteArgs, localArgs, err
		}
	}
	if rteArgs.ReferenceContainer == nil {
		rteArgs.ReferenceContainer = podrescli.ContainerIdentFromEnv()
	}

	if configPath, ok := arguments["--config"].(string); ok {
		conf, err := config.ReadConfig(configPath)
		if err != nil {
			log.Fatalf("error reading the configuration file: %v", err)
		}
		resourcemonitorArgs.ExcludeList.ExcludeList = conf.ExcludeList
		localArgs.SysConf = conf.Resources
		// do not overwrite with empty an existing value (e.g. from opts)
		if rteArgs.TopologyManagerPolicy == "" {
			rteArgs.TopologyManagerPolicy = conf.TopologyManagerPolicy
		}
	}

	if tmPolicy, ok := arguments["--topology-manager-policy"].(string); ok {
		rteArgs.TopologyManagerPolicy = tmPolicy
	}

	return nrtupdaterArgs, resourcemonitorArgs, rteArgs, localArgs, nil
}
