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
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/nrtupdater"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podrescli"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/prometheus"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcemonitor"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcetopologyexporter"

	"github.com/openshift-kni/numaresources-operator/pkg/version"

	"github.com/openshift-kni/numaresources-operator/rte/pkg/config"
	"github.com/openshift-kni/numaresources-operator/rte/pkg/podrescompat"
	"github.com/openshift-kni/numaresources-operator/rte/pkg/sysinfo"
)

type localArgs struct {
	SysConf    sysinfo.Config
	ConfigPath string
}

type ProgArgs struct {
	NRTupdater      nrtupdater.Args
	Resourcemonitor resourcemonitor.Args
	RTE             resourcetopologyexporter.Args
	LocalArgs       localArgs
	Version         bool
	SysinfoOnly     bool
}

func main() {
	klog.Infof("starting %s %s %s %s\n", version.ProgramName(), version.Get(), version.GetGitCommit(), runtime.Version())

	parsedArgs, err := parseArgs(os.Args[1:]...)
	if err != nil {
		klog.Fatalf("failed to parse args: %v", err)
	}

	if parsedArgs.Version {
		fmt.Printf("%s %s %s %s\n", version.ProgramName(), version.Get(), version.GetGitCommit(), runtime.Version())
		os.Exit(0)
	}

	// only for debug purposes
	// printing the header so early includes any debug message from the sysinfo package
	klog.Infof("=== System information ===\n")
	sysInfo, err := sysinfo.NewSysinfo(parsedArgs.LocalArgs.SysConf)
	if err != nil {
		klog.Fatalf("failed to query system info: %w", err)
	}
	klog.Infof("\n%s", sysInfo)
	klog.Infof("==========================\n")

	if parsedArgs.SysinfoOnly {
		os.Exit(0)
	}

	k8sCli, err := podrescli.NewK8SClient(parsedArgs.RTE.PodResourcesSocketPath)
	if err != nil {
		klog.Fatalf("failed to start prometheus server: %v", err)
	}

	sysCli := k8sCli
	if !parsedArgs.LocalArgs.SysConf.IsEmpty() {
		sysCli = podrescompat.NewSysinfoClientFromLister(k8sCli, parsedArgs.LocalArgs.SysConf)
	}

	cli, err := podrescli.NewFilteringClientFromLister(sysCli, parsedArgs.RTE.Debug, parsedArgs.RTE.ReferenceContainer)
	if err != nil {
		klog.Fatalf("failed to get podresources filtering client: %v", err)
	}

	err = prometheus.InitPrometheus()
	if err != nil {
		klog.Fatalf("failed to start prometheus server: %v", err)
	}

	err = resourcetopologyexporter.Execute(cli, parsedArgs.NRTupdater, parsedArgs.Resourcemonitor, parsedArgs.RTE)
	// must never execute; if it does, we want to know
	klog.Fatalf("failed to execute: %v", err)
}

// The args is passed only for testing purposes.
func parseArgs(args ...string) (ProgArgs, error) {
	pArgs := ProgArgs{}

	var sysReservedCPUs string
	var sysReservedMemory string
	var sysResourceMapping string

	flags := flag.NewFlagSet(version.ProgramName(), flag.ExitOnError)

	klog.InitFlags(flags)

	flags.BoolVar(&pArgs.NRTupdater.NoPublish, "no-publish", false, "Do not publish discovered features to the cluster-local Kubernetes API server.")
	flags.BoolVar(&pArgs.NRTupdater.Oneshot, "oneshot", false, "Update once and exit.")
	flags.StringVar(&pArgs.NRTupdater.Hostname, "hostname", defaultHostName(), "Override the node hostname.")

	flags.StringVar(&pArgs.Resourcemonitor.Namespace, "watch-namespace", "", "Namespace to watch pods for. Use \"\" for all namespaces.")
	flags.StringVar(&pArgs.Resourcemonitor.SysfsRoot, "sysfs", "/sys", "Top-level component path of sysfs.")
	flags.BoolVar(&pArgs.Resourcemonitor.PodSetFingerprint, "pods-fingerprint", false, "If enable, compute and report the pod set fingerprint.")
	flags.BoolVar(&pArgs.Resourcemonitor.ExposeTiming, "expose-timing", false, "If enable, expose expected and actual sleep interval as annotations.")
	flags.BoolVar(&pArgs.Resourcemonitor.RefreshNodeResources, "refresh-node-resources", false, "If enable, track changes in node's resources")

	flags.StringVar(&pArgs.LocalArgs.ConfigPath, "config", "/etc/resource-topology-exporter/config.yaml", "Configuration file path. Use this to set the exclude list.")

	flags.BoolVar(&pArgs.RTE.Debug, "debug", false, " Enable debug output.")
	flags.StringVar(&pArgs.RTE.TopologyManagerPolicy, "topology-manager-policy", defaultTopologyManagerPolicy(), "Explicitly set the topology manager policy instead of reading from the kubelet.")
	flags.StringVar(&pArgs.RTE.TopologyManagerScope, "topology-manager-scope", defaultTopologyManagerScope(), "Explicitly set the topology manager scope instead of reading from the kubelet.")
	flags.DurationVar(&pArgs.RTE.SleepInterval, "sleep-interval", 60*time.Second, "Time to sleep between podresources API polls.")
	flags.StringVar(&pArgs.RTE.KubeletConfigFile, "kubelet-config-file", "/podresources/config.yaml", "Kubelet config file path.")
	flags.StringVar(&pArgs.RTE.PodResourcesSocketPath, "podresources-socket", "unix:///podresources/kubelet.sock", "Pod Resource Socket path to use.")
	flags.BoolVar(&pArgs.RTE.PodReadinessEnable, "podreadiness", true, "Custom condition injection using Podreadiness.")

	kubeletStateDirs := flags.String("kubelet-state-dir", "", "Kubelet state directory (RO access needed), for smart polling.")
	refCnt := flags.String("reference-container", "", "Reference container, used to learn about the shared cpu pool\n See: https://github.com/kubernetes/kubernetes/issues/102190\n format of spec is namespace/podname/containername.\n Alternatively, you can use the env vars REFERENCE_NAMESPACE, REFERENCE_POD_NAME, REFERENCE_CONTAINER_NAME.")

	flags.StringVar(&pArgs.RTE.NotifyFilePath, "notify-file", "", "Notification file path.")
	// Lets keep it simple by now and expose only "events-per-second"
	// but logic is prepared to be able to also define the time base
	// that is why TimeUnitToLimitEvents is hard initialized to Second
	flags.Int64Var(&pArgs.RTE.MaxEventsPerTimeUnit, "max-events-per-second", 1, "Max times per second resources will be scanned and updated")
	pArgs.RTE.TimeUnitToLimitEvents = time.Second

	flags.StringVar(&sysReservedCPUs, "system-info-reserved-cpus", "", "kubelet reserved CPUs (cpuset format: 0,1 or 0,1-3 ...)")
	flags.StringVar(&sysReservedMemory, "system-info-reserved-memory", "", "kubelet reserved memory: comma-separated 'numaID=amount' (example: '0=16Gi,1=8192Mi,3=1Gi')")
	flags.StringVar(&sysResourceMapping, "system-info-resource-mapping", "", "kubelet resource mapping: comma-separated 'vendor:device=resourcename'")
	flags.BoolVar(&pArgs.SysinfoOnly, "system-info", false, "Output detected system info and exit")

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

	conf, err := config.ReadConfig(pArgs.LocalArgs.ConfigPath)
	if err != nil {
		return pArgs, fmt.Errorf("error getting exclude list from the configuration: %v", err)
	}
	if len(conf.ExcludeList) != 0 {
		pArgs.Resourcemonitor.ExcludeList = conf.ExcludeList
		klog.V(2).Infof("using exclude list:\n%s", pArgs.Resourcemonitor.ExcludeList.String())
	}
	pArgs.LocalArgs.SysConf = conf.Resources

	// override from the command line
	if sysReservedCPUs != "" {
		pArgs.LocalArgs.SysConf.ReservedCPUs = sysReservedCPUs
	}
	if rmap := sysinfo.ResourceMappingFromString(sysResourceMapping); len(rmap) > 0 {
		pArgs.LocalArgs.SysConf.ResourceMapping = rmap
	}
	if rmap := sysinfo.ReservedMemoryFromString(sysReservedMemory); len(rmap) > 0 {
		pArgs.LocalArgs.SysConf.ReservedMemory = rmap
	}

	klog.Infof("using sysinfo:\n%s", pArgs.LocalArgs.SysConf.ToYAMLString())

	// do not overwrite with empty an existing value (e.g. from opts)
	if pArgs.RTE.TopologyManagerPolicy == "" {
		pArgs.RTE.TopologyManagerPolicy = conf.TopologyManagerPolicy
	}

	// overwrite if explicitly mentioned in conf
	if conf.TopologyManagerScope != "" {
		pArgs.RTE.TopologyManagerScope = conf.TopologyManagerScope
	}

	return pArgs, nil
}

func defaultHostName() string {
	var err error

	val, ok := os.LookupEnv("NODE_NAME")
	if !ok || val == "" {
		val, err = os.Hostname()
		if err != nil {
			klog.Fatalf("error getting the host name: %v", err)
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
	//https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#topology-manager-scopes
	return "container"
}

func setKubeletStateDirs(value string) ([]string, error) {
	return append([]string{}, strings.Split(value, " ")...), nil
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
