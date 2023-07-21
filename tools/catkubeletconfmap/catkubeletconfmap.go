package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"

	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
)

func main() {
	var prefix string
	var cmNamespace string
	var cmName string
	flag.StringVar(&cmNamespace, "namespace", "numaresources", "namespace to look the configmap into")
	flag.StringVar(&cmName, "name", "numaresourcesoperator-worker", "name of the configmap to look for")
	flag.StringVar(&prefix, "prefix", "", "prefix for the output")
	flag.Parse()

	cli, err := clientutil.New()
	if err != nil {
		log.Fatalf("error creating a client: %v", err)
	}

	ctx := context.Background()
	key := client.ObjectKey{
		Namespace: cmNamespace,
		Name:      cmName,
	}
	cm := corev1.ConfigMap{}
	err = cli.Get(ctx, key, &cm)
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

	fmt.Printf("%sTOPOLOGY_MANAGER_POLICY=%s\n", prefix, conf.TopologyManagerPolicy)
	fmt.Printf("%sTOPOLOGY_MANAGER_SCOPE=%s\n", prefix, conf.TopologyManagerScope)
}
