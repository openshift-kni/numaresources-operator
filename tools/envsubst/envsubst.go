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
	"bufio"
	"fmt"
	"log"
	"os"

	"github.com/drone/envsubst"
)

func main() {
	stdin := bufio.NewScanner(os.Stdin)

	for stdin.Scan() {
		line, err := envsubst.Eval(stdin.Text(), func(env string) string {
			if val, ok := os.LookupEnv(env); ok {
				return val
			}
			return fmt.Sprintf("${%s}", env)
		})
		if err != nil {
			log.Fatalf("Error while envsubst: %v", err)
		}
		fmt.Println(line)
	}
}
