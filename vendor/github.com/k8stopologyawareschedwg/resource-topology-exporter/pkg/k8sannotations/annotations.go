/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package k8sannotations

const (
	RTEUpdate      = "k8stopoawareschedwg/rte-update"
	SleepDuration  = "k8stopoawareschedwg/sleep-duration"
	UpdateInterval = "k8stopoawareschedwg/update-interval"
)

func Merge(kvs ...map[string]string) map[string]string {
	ret := make(map[string]string)
	for _, kv := range kvs {
		for key, value := range kv {
			ret[key] = value
		}
	}
	return ret
}
