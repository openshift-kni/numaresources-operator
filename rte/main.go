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
	"os"
	"runtime"
	"sort"
	"time"

	"k8s.io/klog/v2"

	"github.com/k8stopologyawareschedwg/podfingerprint"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/k8shelpers"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/nrtupdater"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podres"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podres/middleware/podexclude"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podres/middleware/sharedcpuspool"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/prometheus"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcemonitor"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcetopologyexporter"

	"github.com/openshift-kni/numaresources-operator/pkg/version"

	"github.com/openshift-kni/numaresources-operator/rte/pkg/config"
)

const (
	// k8s 1.25 https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#topology-manager-policies
	defaultTopologyManagerPolicy = "none"
	// k8s 1.25 https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#topology-manager-scopes
	defaultTopologyManagerScope = "container"
)

type localArgs struct {
	ConfigPath  string
	PodExcludes podexclude.List
}

type ProgArgs struct {
	NRTupdater      nrtupdater.Args
	Resourcemonitor resourcemonitor.Args
	RTE             resourcetopologyexporter.Args
	LocalArgs       localArgs
	Version         bool
	DumpConfig      bool
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

	if parsedArgs.DumpConfig {
		data, err := json.Marshal(parsedArgs)
		if err != nil {
			klog.Fatalf("encoding the configuration to JSON")
		}
		fmt.Printf("%s\n", string(data))
		os.Exit(0)
	}

	k8scli, err := k8shelpers.GetK8sClient("")
	if err != nil {
		klog.Fatalf("failed to get k8s client: %w", err)
	}

	cli, cleanup, err := podres.GetClient(parsedArgs.RTE.PodResourcesSocketPath)
	if err != nil {
		klog.Fatalf("failed to start prometheus server: %v", err)
	}
	//nolint: errcheck
	defer cleanup()

	cli = sharedcpuspool.NewFromLister(cli, parsedArgs.RTE.Debug, parsedArgs.RTE.ReferenceContainer)

	err = prometheus.InitPrometheus()
	if err != nil {
		klog.Fatalf("failed to start prometheus server: %v", err)
	}

	// TODO: recycled flag (no big deal, but still)
	cli = podexclude.NewFromLister(cli, parsedArgs.RTE.Debug, parsedArgs.LocalArgs.PodExcludes)
	hnd := resourcemonitor.Handle{
		PodResCli: cli,
		K8SCli:    k8scli,
	}
	err = resourcetopologyexporter.Execute(hnd, parsedArgs.NRTupdater, parsedArgs.Resourcemonitor, parsedArgs.RTE)
	// must never execute; if it does, we want to know
	klog.Fatalf("failed to execute: %v", err)
}

// The args is passed only for testing purposes.
func parseArgs(args ...string) (ProgArgs, error) {
	pArgs := ProgArgs{}

	var pfpMethod string

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
	flags.StringVar(&pArgs.Resourcemonitor.PodSetFingerprintStatusFile, "pods-fingerprint-status-file", "", "File to dump the pods fingerprint status. Use \"\" to disable.")
	flags.StringVar(&pfpMethod, "pods-fingerprint-method", podfingerprint.MethodAll, fmt.Sprintf("Select the method to compute the pods fingerprint. Valid options: %s.", resourcemonitor.PFPMethodSupported()))

	flags.StringVar(&pArgs.LocalArgs.ConfigPath, "config", "/etc/resource-topology-exporter/config.yaml", "Configuration file path. Use this to set the exclude list.")

	flags.BoolVar(&pArgs.RTE.Debug, "debug", false, " Enable debug output.")
	flags.StringVar(&pArgs.RTE.TopologyManagerPolicy, "topology-manager-policy", "", "Explicitly set the topology manager policy instead of reading from the kubelet.")
	flags.StringVar(&pArgs.RTE.TopologyManagerScope, "topology-manager-scope", "", "Explicitly set the topology manager scope instead of reading from the kubelet.")
	flags.DurationVar(&pArgs.RTE.SleepInterval, "sleep-interval", 60*time.Second, "Time to sleep between podresources API polls.")
	flags.StringVar(&pArgs.RTE.KubeletConfigFile, "kubelet-config-file", "/podresources/config.yaml", "Kubelet config file path.")
	flags.StringVar(&pArgs.RTE.PodResourcesSocketPath, "podresources-socket", "unix:///podresources/kubelet.sock", "Pod Resource Socket path to use.")
	flags.BoolVar(&pArgs.RTE.PodReadinessEnable, "podreadiness", true, "Custom condition injection using Podreadiness.")
	flags.BoolVar(&pArgs.RTE.AddNRTOwnerEnable, "add-nrt-owner", true, "RTE will inject NRT's related node as OwnerReference to ensure cleanup if the node is deleted.")

	refCnt := flags.String("reference-container", "", "Reference container, used to learn about the shared cpu pool\n See: https://github.com/kubernetes/kubernetes/issues/102190\n format of spec is namespace/podname/containername.\n Alternatively, you can use the env vars REFERENCE_NAMESPACE, REFERENCE_POD_NAME, REFERENCE_CONTAINER_NAME.")

	flags.StringVar(&pArgs.RTE.NotifyFilePath, "notify-file", "", "Notification file path.")
	// Lets keep it simple by now and expose only "events-per-second"
	// but logic is prepared to be able to also define the time base
	// that is why TimeUnitToLimitEvents is hard initialized to Second
	flags.Int64Var(&pArgs.RTE.MaxEventsPerTimeUnit, "max-events-per-second", 1, "Max times per second resources will be scanned and updated")
	pArgs.RTE.TimeUnitToLimitEvents = time.Second

	flags.BoolVar(&pArgs.DumpConfig, "dump-config", false, "Output the configuration settings and exit - the output format is JSON, subjected to change without notice.")
	flags.BoolVar(&pArgs.Version, "version", false, "Output version and exit")

	err := flags.Parse(args)
	if err != nil {
		return pArgs, err
	}

	if pArgs.Version {
		return pArgs, err
	}

	pArgs.RTE.ReferenceContainer, err = setContainerIdent(*refCnt)
	if err != nil {
		return pArgs, err
	}
	if pArgs.RTE.ReferenceContainer.IsEmpty() {
		pArgs.RTE.ReferenceContainer = sharedcpuspool.ContainerIdentFromEnv()
	}

	pArgs.Resourcemonitor.PodSetFingerprintMethod, err = resourcemonitor.PFPMethodIsSupported(pfpMethod)
	if err != nil {
		return pArgs, err
	}

	conf, err := config.ReadFile(pArgs.LocalArgs.ConfigPath)
	if err != nil {
		return pArgs, fmt.Errorf("error getting exclude list from the configuration: %v", err)
	}
	if len(conf.ExcludeList) != 0 {
		pArgs.Resourcemonitor.ResourceExclude = conf.ExcludeList
		klog.V(2).Infof("using exclude list:\n%s", pArgs.Resourcemonitor.ResourceExclude.String())
	}

	pArgs.LocalArgs.PodExcludes = makePodExcludeList(conf.PodExcludes)

	err = setupTopologyManagerConfig(&pArgs, conf)
	if err != nil {
		return pArgs, err
	}

	return pArgs, nil
}

func makePodExcludeList(excludes map[string]string) podexclude.List {
	keys := []string{}
	for key := range excludes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	exList := podexclude.List{}
	for _, key := range keys {
		val := excludes[key]
		exList = append(exList, podexclude.Item{
			NamespacePattern: key,
			NamePattern:      val,
		})
	}
	return exList
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

func setupTopologyManagerConfig(pArgs *ProgArgs, conf config.Config) error {
	// general flow: do not overwrite with empty an existing value (e.g. from opts)
	// precedence order:
	// - args, which overrides
	// - env var, which overrides
	// - config file, which overrides
	// - defaults (hardcoded)

	type choice struct {
		value  string
		origin string
	}

	pickFirstNonEmpty := func(choices []choice) choice {
		for _, ch := range choices {
			if ch.value != "" {
				return ch
			}
		}
		return choice{}
	}

	policy := pickFirstNonEmpty([]choice{
		{
			value:  pArgs.RTE.TopologyManagerPolicy,
			origin: "args",
		},
		{
			value:  os.Getenv("TOPOLOGY_MANAGER_POLICY"),
			origin: "env",
		},
		{
			value:  conf.TopologyManagerPolicy,
			origin: "conf",
		},
		{
			value:  defaultTopologyManagerPolicy,
			origin: "default",
		},
	})
	pArgs.RTE.TopologyManagerPolicy = policy.value

	scope := pickFirstNonEmpty([]choice{
		{
			value:  pArgs.RTE.TopologyManagerScope,
			origin: "args",
		},
		{
			value:  os.Getenv("TOPOLOGY_MANAGER_SCOPE"),
			origin: "env",
		},
		{
			value:  conf.TopologyManagerScope,
			origin: "conf",
		},
		{
			value:  defaultTopologyManagerScope,
			origin: "default",
		},
	})
	pArgs.RTE.TopologyManagerScope = scope.value

	klog.Infof("using Topology Manager scope %q from %q (conf=%s) policy %q from %q (conf=%s)",
		pArgs.RTE.TopologyManagerScope, scope.origin, conf.TopologyManagerScope,
		pArgs.RTE.TopologyManagerPolicy, policy.origin, conf.TopologyManagerPolicy,
	)

	if pArgs.RTE.TopologyManagerPolicy == "" || pArgs.RTE.TopologyManagerScope == "" {
		return fmt.Errorf("incomplete Topology Manager configuration")
	}
	return nil
}

func setContainerIdent(value string) (*sharedcpuspool.ContainerIdent, error) {
	ci, err := sharedcpuspool.ContainerIdentFromString(value)
	if err != nil {
		return nil, err
	}

	if ci == nil {
		return &sharedcpuspool.ContainerIdent{}, nil
	}

	return ci, nil
}
