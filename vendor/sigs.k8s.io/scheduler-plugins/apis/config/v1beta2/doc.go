/*
Copyright 2021 The Kubernetes Authors.

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

// +k8s:deepcopy-gen=package
// +k8s:conversion-gen=sigs.k8s.io/scheduler-plugins/apis/config
// +k8s:defaulter-gen=TypeMeta
// +k8s:defaulter-gen-input=sigs.k8s.io/scheduler-plugins/apis/config/v1beta2
// +groupName=kubescheduler.config.k8s.io

// Package v1beta2 is the v1beta2 version of the API.
package v1beta2 // import "sigs.k8s.io/scheduler-plugins/apis/config/v1beta2"
