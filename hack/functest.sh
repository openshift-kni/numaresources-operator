#!/bin/bash

set -x

BASE_DIR="$(dirname "$(realpath "$0")")"
TEST_DIR="${BASE_DIR}"/../test/e2e
E2E_NAMESPACE_NAME="${1:-e2e-numaresources-operator}"

# expect oc to be in PATH by default
OC_TOOL="${OC_TOOL:-oc}"

"${BASE_DIR}"/functest-setup.sh "$E2E_NAMESPACE_NAME"

E2E_NAMESPACE_NAME=$E2E_NAMESPACE_NAME ginkgo -v \
 --focus='\[BasicInstall\]' \
  -requireSuite  "${TEST_DIR}"/basic_install "${TEST_DIR}"

"${BASE_DIR}"/functest-teardown.sh "$E2E_NAMESPACE_NAME"
