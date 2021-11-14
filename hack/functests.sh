#!/bin/bash

set -x

BASE_DIR="$(dirname "$(realpath "$0")")"
TEST_DIR="${BASE_DIR}"/../test/e2e
E2E_NAMESPACE_NAME="${1:-e2e-numaresources-operator}"

# expect oc to be in PATH by default
OC_TOOL="${OC_TOOL:-oc}"

"${BASE_DIR}"/functests-setup.sh "$E2E_NAMESPACE_NAME"

E2E_NAMESPACE_NAME=$E2E_NAMESPACE_NAME go test -v "${TEST_DIR}"/basic_install "${TEST_DIR}" \
-ginkgo.v -ginkgo.focus='\[BasicInstall\]|\[RTE\]*|\[InfraConsuming\]*'

#E2E_NAMESPACE_NAME=$E2E_NAMESPACE_NAME "${BASE_DIR}"/../bin/e2e-test  \
#  -ginkgo.v -ginkgo.focus='\[BasicInstall\]|\[RTE\]*|\[InfraConsuming\]*'

"${BASE_DIR}"/functests-teardown.sh "$E2E_NAMESPACE_NAME"
