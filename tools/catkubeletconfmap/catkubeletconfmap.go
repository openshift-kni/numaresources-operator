/*
 * Copyright 2023 Red Hat, Inc.
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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8swait "k8s.io/apimachinery/pkg/util/wait"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"

	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
)

func main() {
	prefix := ""
	cmNamespace := "numaresources"
	cmName := "numaresourcesoperator-worker"
	waitTimeout := 0 * time.Second  // no wait
	waitInterval := 2 * time.Second // if we wait at all, this is the intervall between polls
	waitImmediate := true           // guarantee to call at least once, necessary with default timout

	flag.StringVar(&cmNamespace, "namespace", cmNamespace, "namespace to look the configmap into")
	flag.StringVar(&cmName, "name", cmName, "name of the configmap to look for")
	flag.StringVar(&prefix, "prefix", prefix, "prefix for the output")
	flag.DurationVar(&waitTimeout, "wait", waitTimeout, "retry till this time limit")
	flag.Parse()

	cli, err := clientutil.New()
	if err != nil {
		log.Fatalf("error creating a client: %v", err)
	}

	log.Printf("trying to fetch %s/%s...", cmNamespace, cmName)

	ctx := context.Background()
	cm := corev1.ConfigMap{}

	err = k8swait.PollUntilContextTimeout(ctx, waitInterval, waitTimeout, waitImmediate, func(fctx context.Context) (bool, error) {
		key := client.ObjectKey{
			Namespace: cmNamespace,
			Name:      cmName,
		}
		ferr := cli.Get(fctx, key, &cm)
		if ferr != nil {
			if apierrors.IsNotFound(ferr) {
				log.Printf("failed to get %s/%s - not found, retrying...", cmNamespace, cmName)
				return false, nil
			}
			log.Printf("failed to get %s/%s: %v, aborting", cmNamespace, cmName, ferr)
			return false, ferr
		}
		log.Printf("got %s/%s!", cmNamespace, cmName)
		return true, nil
	})

	if err != nil {
		log.Fatalf("error getting the ConfigMap %s/%s: %v", cmNamespace, cmName, err)
	}

	cmData, err := rteconfig.UnpackConfigMap(&cm)
	if err != nil {
		log.Fatalf("error unpacking ConfigMap %s/%s: %v", cmNamespace, cmName, err)
	}

	conf, err := rteconfig.Unrender(cmData)
	if err != nil {
		log.Fatalf("error unrendering ConfigMap %s/%s: %v", cmNamespace, cmName, err)
	}

	fmt.Printf("%sTOPOLOGY_MANAGER_POLICY=%s\n", prefix, conf.Kubelet.TopologyManagerPolicy)
	fmt.Printf("%sTOPOLOGY_MANAGER_SCOPE=%s\n", prefix, conf.Kubelet.TopologyManagerScope)
}
