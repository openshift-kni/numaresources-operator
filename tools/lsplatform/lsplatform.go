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
