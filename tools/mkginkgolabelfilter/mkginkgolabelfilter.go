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
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/openshift-kni/numaresources-operator/internal/api/features"
)

func main() {
	var help bool
	flag.BoolVar(&help, "h", help, "outputs tool description")
	flag.Parse()

	if help {
		e := features.NewTopicInfo()
		e.Active = []string{"feature_1", "feature_2"}
		filter := fmt.Sprintf("feature: consistAny {%s}\n", strings.Join(e.Active, ","))
		fmt.Printf("The tool expects a json format text and parses it into TopicInfo struct, then prints out a ginkgo label filter for the active features.\nExample: for input\n%+v\nthe tool will print\n%s\n", e, filter)
		os.Exit(0)
	}

	var topics features.TopicInfo
	err := json.NewDecoder(os.Stdin).Decode(&topics)
	if err != nil {
		log.Fatalf("error decoding topics data: %v", err)
	}
	fmt.Printf("feature: consistAny {%s}\n", strings.Join(topics.Active, ","))
}
