/*
 * Copyright 2024 Red Hat, Inc.
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
	"log"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	topologyv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
)

func main() {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(topologyv1alpha2.AddToScheme(scheme))

	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatalf("error getting the config: %v", err)
	}

	cli, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		log.Fatalf("error creating a watchable client: %v", err)
	}

	ctx := context.Background()

	log.Printf("NRT objects watch loop begin")
	defer log.Printf("NRT objects watch loop end")

	for {
		log.Printf("start watching NRT objects")

		nrtObjs := topologyv1alpha2.NodeResourceTopologyList{}
		wa, err := cli.Watch(ctx, &nrtObjs)
		if err != nil {
			log.Fatalf("cannot watch NRT objects: %v", err)
		}

		for {
			select {
			case ev := <-wa.ResultChan():
				processEvent(ev)

			case <-ctx.Done():
				log.Printf("stop watching NRT objects")
				wa.Stop()
			}
		}
	}
}

func processEvent(ev watch.Event) bool {
	if ev.Type != watch.Modified {
		return false
	}

	nrtObj, ok := ev.Object.(*topologyv1alpha2.NodeResourceTopology)
	if !ok {
		log.Printf("unexpected object %T", ev.Object)
		return false
	}

	log.Printf("NRT %q attrs=%+v", nrtObj.Name, nrtObj.Attributes)
	return true
}
