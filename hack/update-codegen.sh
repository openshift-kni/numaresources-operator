#!/usr/bin/env bash

# Copyright 2020 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${SCRIPT_ROOT}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

# prepare paths as expected by generate-groups.sh. There are likely cleaner ways, we should explore them
function setup() {
	mkdir ${SCRIPT_ROOT}/api/numaresourcesoperator
	ln -s ../v1alpha1 ${SCRIPT_ROOT}/api/numaresourcesoperator/v1alpha1
	ln -s ../v1 ${SCRIPT_ROOT}/api/numaresourcesoperator/v1
}

function cleanup() {
	rm -rf ${SCRIPT_ROOT}/api/numaresourcesoperator
}

function fix_imports() {
	find ${SCRIPT_ROOT}/pkg/k8sclientset -name "*.go" -type f -print0 | xargs -0 sed -i 's|github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator|github.com/openshift-kni/numaresources-operator/api|g'
}

trap 'cleanup' EXIT SIGINT SIGTERM SIGSTOP

setup

# generate the code with:
# --output-base    because this script should also be able to run inside the vendor dir of
#                  k8s.io/kubernetes. The output-base is needed for the generators to output into the vendor dir
#                  instead of the $GOPATH directly. For normal projects this can be dropped.
#
bash "${CODEGEN_PKG}"/generate-groups.sh "client" \
  github.com/openshift-kni/numaresources-operator/pkg/k8sclientset/generated \
  github.com/openshift-kni/numaresources-operator/api \
  numaresourcesoperator:v1alpha1,v1 \
  --output-base "$(dirname "${BASH_SOURCE[0]}")/../../../.." \
  --go-header-file "${SCRIPT_ROOT}"/hack/boilerplate.go.txt

fix_imports
