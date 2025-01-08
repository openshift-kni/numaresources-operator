/*
 * Copyright 2025 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache./licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package kloglevel

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"k8s.io/klog/v2"
)

var tests = []struct {
	name       string
	levelToSet klog.Level
	expected   string
}{
	{
		name:     "keep the default level",
		expected: "0",
	},
	{
		name:       "update to 4",
		levelToSet: 4,
		expected:   "4",
	},
}

func init() {
	// before initializing the flags
	got, err := Get()
	if err == nil {
		fmt.Printf("expecting error but got %v", got)
		os.Exit(1)
	}

	klog.InitFlags(nil)
}

func TestGet(t *testing.T) {
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var v klog.Level
			// use Level set intentionally to isolate functionality checks (instead of using Set())
			err := v.Set(tc.levelToSet.String())
			if err != nil {
				t.Errorf("error setting klog level: %v", err)
				return
			}
			got, err := Get()
			if err != nil {
				t.Errorf("error getting klog level: %v", err)
				return
			}
			if got.String() != tc.expected {
				t.Errorf("got %q, expected %q", got.String(), tc.expected)
			}
		})
	}
}

func TestSet(t *testing.T) {
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Set(tc.levelToSet)
			if err != nil {
				t.Errorf("error setting klog level: %v", err)
				return
			}

			// get it using VisitAll intentionally to isolate functionality checks (instead of using Get())
			var got *klog.Level
			flag.VisitAll(func(f *flag.Flag) {
				if f.Name == "v" {
					got = f.Value.(*klog.Level)
				}
			})

			if got.String() != tc.expected {
				t.Errorf("got %q, expected %q", got.String(), tc.expected)
			}
		})
	}
}
