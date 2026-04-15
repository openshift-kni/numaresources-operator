/*
 * Copyright 2026 Red Hat, Inc.
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
	"log"
	"os"
	"path/filepath"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift-kni/numaresources-operator/internal/podlogs"
	schedcp "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/controlplane"
	schedupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/sched"
)

func main() {
	containerName := schedupdate.MainContainerName
	namespace := "numaresources"
	since := 5 * time.Minute

	flag.StringVar(&namespace, "namespace", namespace, "namespace where the scheduler is deployed")
	flag.StringVar(&containerName, "container", containerName, "name of the scheduler container")
	flag.DurationVar(&since, "since", since, "fetch logs from the last duration (e.g. 30s, 5m)")
	flag.Parse()

	kubeconfig, ok := os.LookupEnv("KUBECONFIG")
	if !ok {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		log.Printf("using default kubeconfig: %q", kubeconfig)
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatalf("error building kubeconfig: %v", err)
	}

	k8sCli, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("error creating kubernetes client: %v", err)
	}

	ctx := context.Background()

	leaderPod, err := schedcp.GetLeaderPod(ctx, k8sCli, namespace)
	if err != nil {
		log.Fatalf("error finding leader pod: %v", err)
	}
	log.Printf("leader pod: %s/%s", leaderPod.Namespace, leaderPod.Name)

	logs, err := podlogs.GetSince(ctx, k8sCli, leaderPod.Namespace, leaderPod.Name, containerName, since)
	if err != nil {
		log.Fatalf("error fetching logs: %v", err)
	}

	fmt.Println("-------8<-------")
	fmt.Print(logs)
	fmt.Println("-------8<-------")
}
