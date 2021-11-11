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
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/nrtupdater"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podrescli"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/prometheus"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcemonitor"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcetopologyexporter"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/version"

	"github.com/openshift-kni/resource-topology-exporter/pkg/config"
	"github.com/openshift-kni/resource-topology-exporter/pkg/podrescompat"
	"github.com/openshift-kni/resource-topology-exporter/pkg/sysinfo"
)

type localArgs struct {
	SysConf sysinfo.Config
}

type ProgArgs struct {
	NRTupdater      nrtupdater.Args
	Resourcemonitor resourcemonitor.Args
	RTE             resourcetopologyexporter.Args
	Version         bool
	LocalArgs       localArgs
}

func (pa *ProgArgs) ToJson() ([]byte, error) {
	return json.Marshal(pa)
}

func main() {
	parsedArgs, err := parseArgs(os.Args[1:]...)
	if err != nil {
		log.Fatalf("failed to parse args: %v", err)
	}

	if parsedArgs.Version {
		fmt.Println(version.ProgramName, version.Get())
		os.Exit(0)
	}

	// only for debug purposes
	// printing the header so early includes any debug message from the sysinfo package
	log.Printf("=== System information ===\n")
	sysInfo, err := sysinfo.NewSysinfo(parsedArgs.LocalArgs.SysConf)
	if err != nil {
		log.Fatalf("failed to query system info: %v", err)
	}
	log.Printf("%s", sysInfo)
	log.Printf("==========================\n")

	k8sCli, err := podrescli.NewK8SClient(parsedArgs.RTE.PodResourcesSocketPath)
	if err != nil {
		log.Fatalf("failed to get podresources k8s client: %v", err)
	}

	sysCli := k8sCli
	if !parsedArgs.LocalArgs.SysConf.IsEmpty() {
		sysCli = podrescompat.NewSysinfoClientFromLister(k8sCli, parsedArgs.LocalArgs.SysConf)
	}

	cli, err := podrescli.NewFilteringClientFromLister(sysCli, parsedArgs.RTE.Debug, parsedArgs.RTE.ReferenceContainer)
	if err != nil {
		log.Fatalf("failed to get podresources filtering client: %v", err)
	}

	err = prometheus.InitPrometheus()
	if err != nil {
		log.Fatalf("failed to start prometheus server: %v", err)
	}

	err = resourcetopologyexporter.Execute(cli, parsedArgs.NRTupdater, parsedArgs.Resourcemonitor, parsedArgs.RTE)
	if err != nil {
		log.Fatalf("failed to execute: %v", err)
	}
}

// The args is passed only for testing purposes.
func parseArgs(args ...string) (ProgArgs, error) {
	pArgs := ProgArgs{
		nrtupdater.Args{},
		resourcemonitor.Args{},
		resourcetopologyexporter.Args{},
		false,
		localArgs{},
	}

	var configPath string
	flags := flag.NewFlagSet(version.ProgramName, flag.ExitOnError)

	flags.BoolVar(&pArgs.NRTupdater.NoPublish, "no-publish", false, "Do not publish discovered features to the cluster-local Kubernetes API server.")
	flags.BoolVar(&pArgs.NRTupdater.Oneshot, "oneshot", false, "Update once and exit.")
	flags.StringVar(&pArgs.NRTupdater.Namespace, "export-namespace", "", "Namespace on which update CRDs. Use \"\" for all namespaces")
	flags.StringVar(&pArgs.NRTupdater.Hostname, "hostname", defaultHostName(), "Override the node hostname.")

	flags.StringVar(&pArgs.Resourcemonitor.Namespace, "watch-namespace", "", "Namespace to watch pods for. Use \"\" for all namespaces.")
	flags.StringVar(&pArgs.Resourcemonitor.SysfsRoot, "sysfs", "/sys", "Top-level component path of sysfs.")

	flags.StringVar(&configPath, "config", "/etc/resource-topology-exporter/config.yaml", "Configuration file path. Use this to set the exclude list.")

	flags.BoolVar(&pArgs.RTE.Debug, "debug", false, " Enable debug output.")
	flags.StringVar(&pArgs.RTE.TopologyManagerPolicy, "topology-manager-policy", defaultTopologyManagerPolicy(), "Explicitly set the topology manager policy instead of reading from the kubelet.")
	flags.StringVar(&pArgs.RTE.TopologyManagerScope, "topology-manager-scope", defaultTopologyManagerScope(), "Explicitly set the topology manager scope instead of reading from the kubelet.")
	flags.DurationVar(&pArgs.RTE.SleepInterval, "sleep-interval", 60*time.Second, "Time to sleep between podresources API polls.")
	flags.StringVar(&pArgs.RTE.KubeletConfigFile, "kubelet-config-file", "/podresources/config.yaml", "Kubelet config file path.")
	flags.StringVar(&pArgs.RTE.PodResourcesSocketPath, "podresources-socket", "unix:///podresources/kubelet.sock", "Pod Resource Socket path to use.")

	kubeletStateDirs := flags.String("kubelet-state-dir", "", "Kubelet state directory (RO access needed), for smart polling.")
	refCnt := flags.String("reference-container", "", "Reference container, used to learn about the shared cpu pool\n See: https://github.com/kubernetes/kubernetes/issues/102190\n format of spec is namespace/podname/containername.\n Alternatively, you can use the env vars REFERENCE_NAMESPACE, REFERENCE_POD_NAME, REFERENCE_CONTAINER_NAME.")

	flags.BoolVar(&pArgs.Version, "version", false, "Output version and exit")

	err := flags.Parse(args)
	if err != nil {
		return pArgs, err
	}

	if pArgs.Version {
		return pArgs, err
	}

	pArgs.RTE.KubeletStateDirs, err = setKubeletStateDirs(*kubeletStateDirs)
	if err != nil {
		return pArgs, err
	}

	pArgs.RTE.ReferenceContainer, err = setContainerIdent(*refCnt)
	if err != nil {
		return pArgs, err
	}
	if pArgs.RTE.ReferenceContainer.IsEmpty() {
		pArgs.RTE.ReferenceContainer = podrescli.ContainerIdentFromEnv()
	}

	conf, err := config.ReadConfig(configPath)
	if err != nil {
		return pArgs, fmt.Errorf("error getting exclude list from the configuration: %v", err)
	}
	if len(conf.ExcludeList) != 0 {
		pArgs.Resourcemonitor.ExcludeList.ExcludeList = conf.ExcludeList
		log.Printf("using exclude list:\n%s", pArgs.Resourcemonitor.ExcludeList.String())
	}
	pArgs.LocalArgs.SysConf = conf.Resources

	// do not overwrite with empty an existing value (e.g. from opts)
	if pArgs.RTE.TopologyManagerPolicy == "" {
		pArgs.RTE.TopologyManagerPolicy = conf.TopologyManagerPolicy
	}

	return pArgs, nil
}

func defaultHostName() string {
	var err error

	val, ok := os.LookupEnv("NODE_NAME")
	if !ok || val == "" {
		val, err = os.Hostname()
		if err != nil {
			log.Fatalf("error getting the host name: %v", err)
		}
	}
	return val
}

func defaultTopologyManagerPolicy() string {
	if val, ok := os.LookupEnv("TOPOLOGY_MANAGER_POLICY"); ok {
		return val
	}
	// empty string is a valid value here, so just keep going
	return ""
}

func defaultTopologyManagerScope() string {
	if val, ok := os.LookupEnv("TOPOLOGY_MANAGER_SCOPE"); ok {
		return val
	}
	// empty string is a valid value here, so just keep going
	return ""
}

func setKubeletStateDirs(value string) ([]string, error) {
	ksd := make([]string, 0)
	for _, s := range strings.Split(value, " ") {
		ksd = append(ksd, s)
	}
	return ksd, nil
}

func setContainerIdent(value string) (*podrescli.ContainerIdent, error) {
	ci, err := podrescli.ContainerIdentFromString(value)
	if err != nil {
		return nil, err
	}

	if ci == nil {
		return &podrescli.ContainerIdent{}, nil
	}

	return ci, nil
}
