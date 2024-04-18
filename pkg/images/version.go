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

package images

const (
	defaultRepo string = "quay.io/openshift-kni"
	defaultName string = "numaresources-operator"
	defaultTag  string = "devel"
)

// tag Must not be const, supposed to be set using ldflags at build time
var tag = defaultTag

// Tag returns the image tag as a string
func Tag() string {
	return tag
}

// Name returns the image name as a string
func Name() string {
	return defaultName
}

// Repo returns the image repo as a string
func Repo() string {
	return defaultRepo
}

// SpecPath() returns the image full image spec path as a string
func SpecPath() string {
	return Repo() + "/" + Name() + ":" + Tag()
}
