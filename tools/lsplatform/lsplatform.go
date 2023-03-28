package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform/detect"
)

func main() {
	platformName := ""
	flag.StringVar(&platformName, "is-platform", "", "check if the detected platform matches the given one")
	flag.Parse()

	clusterPlatform, err := detect.Platform(context.Background())
	if err != nil {
		log.Fatalf("error detecting the platform: %v", err)
	}

	if platformName != "" {
		ret := 1
		userPlatform, ok := platform.ParsePlatform(platformName)
		if !ok {
			log.Fatalf("error parsing the user platform: %q", platformName)
		}

		if userPlatform == clusterPlatform {
			ret = 0
		}
		os.Exit(ret)
	}

	fmt.Printf("%s\n", clusterPlatform)
}
