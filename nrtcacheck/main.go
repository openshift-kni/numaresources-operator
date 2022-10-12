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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
	appsv1 "k8s.io/api/apps/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"

	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/rte"
	"github.com/openshift-kni/numaresources-operator/pkg/version"

	"github.com/openshift-kni/numaresources-operator/internal/nodes"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/schedcache"
)

var (
	scheme = k8sruntime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(nropv1alpha1.AddToScheme(scheme))
	utilruntime.Must(machineconfigv1.Install(scheme))
	utilruntime.Must(securityv1.Install(scheme))
}

type ProgArgs struct {
	Version bool
	Verbose bool
}

func main() {
	parsedArgs, err := parseArgs(os.Args[1:]...)
	if err != nil {
		klog.V(1).ErrorS(err, "parsing args")
		os.Exit(1)
	}

	if parsedArgs.Version {
		fmt.Println(version.ProgramName(), version.Get())
		os.Exit(0)
	}

	cli, err := NewClientWithScheme(scheme)
	if err != nil {
		klog.V(1).ErrorS(err, "creating client with scheme")
		os.Exit(1)
	}

	rtePodsByNode, err := findRTEPodsByNodeName(cli)
	if err != nil {
		klog.V(1).ErrorS(err, "mapping RTE pods to nodes")
		os.Exit(1)
	}

	for node, podnn := range rtePodsByNode {
		klog.V(1).InfoS("RTE:", "node", node, "pod", podnn.String())
	}

	k8sCli, err := clientutil.NewK8s()
	if err != nil {
		klog.V(1).ErrorS(err, "creating k8s client")
		os.Exit(1)
	}

	workers, err := nodes.GetWorkerNodes(cli)
	if err != nil {
		klog.V(1).ErrorS(err, "getting worker nodes")
		os.Exit(1)
	}

	ok, unsynced, err := schedcache.HasSynced(cli, k8sCli, nodes.GetNames(workers))
	if err != nil {
		klog.V(1).ErrorS(err, "checking sched cache state")
		os.Exit(1)
	}
	if ok {
		if parsedArgs.Verbose {
			fmt.Fprintf(os.Stderr, "all nodes synced\n")
		}
		os.Exit(0)
	}

	for nodeName, podsBySched := range unsynced {
		podnn, ok := rtePodsByNode[nodeName]
		if !ok {
			klog.Warningf("no RTE pod on %q?", nodeName)
			continue
		}

		st, err := schedcache.GetUpdaterFingerprintStatus(k8sCli, podnn.Namespace, podnn.Name, rteupdate.MainContainerName)
		if err != nil {
			klog.V(1).ErrorS(err, "cannot get RTE pfp status from %q %s", nodeName, podnn.String())
			continue
		}

		podsByRTE := sets.String{}
		for _, nn := range st.Pods {
			podsByRTE.Insert(nn.String())
		}

		klog.InfoS("pods on sched, not on RTE", "node", nodeName, "pods", podsBySched.Difference(podsByRTE).List())
		klog.InfoS("pods on RTE, not on sched", "node", nodeName, "pods", podsByRTE.Difference(podsBySched).List())
	}
}

func findRTEPodsByNodeName(cli client.Client) (map[string]types.NamespacedName, error) {
	nroNName := types.NamespacedName{
		Name: objectnames.DefaultNUMAResourcesOperatorCrName,
	}
	nroObj := nropv1alpha1.NUMAResourcesOperator{}
	err := cli.Get(context.TODO(), nroNName, &nroObj)
	if err != nil {
		return nil, err
	}

	podsByName := make(map[string]types.NamespacedName)
	for _, ds := range nroObj.Status.DaemonSets {
		dsObj := appsv1.DaemonSet{}
		err = cli.Get(context.TODO(), types.NamespacedName{Namespace: ds.Namespace, Name: ds.Name}, &dsObj)
		if err != nil {
			return nil, err
		}

		pods, err := podlist.ByDaemonset(cli, dsObj)
		if err != nil {
			return nil, err
		}

		for _, pod := range pods {
			podsByName[pod.Spec.NodeName] = types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      pod.Name,
			}
		}
	}

	return podsByName, nil
}

func parseArgs(args ...string) (ProgArgs, error) {
	pArgs := ProgArgs{}

	flags := flag.NewFlagSet(version.ProgramName(), flag.ExitOnError)

	flags.BoolVar(&pArgs.Version, "version", false, "Output version and exit")

	klog.InitFlags(flags)
	err := flags.Parse(args)
	return pArgs, err
}

func NewClientWithScheme(scheme *k8sruntime.Scheme) (client.Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	return client.New(cfg, client.Options{Scheme: scheme})
}
