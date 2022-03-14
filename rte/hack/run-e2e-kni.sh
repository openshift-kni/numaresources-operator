#!/bin/bash

set -xue

# expect oc to be in PATH by default
OC_TOOL="${OC_TOOL:-oc}"

BASE_DIR="$(dirname "$(realpath "$0")")"
PROJECT_DIR="${BASE_DIR}"/..

export RTE_NAMESPACE="${RTE_NAMESPACE:-rte-e2e}"
export E2E_NAMESPACE_NAME="${E2E_NAMESPACE_NAME:-rte-e2e}"
export E2E_TOPOLOGY_MANAGER_POLICY="${E2E_TOPOLOGY_MANAGER_POLICY:-SingleNUMANodePodLevel}"
export RTE_CONTAINER_IMAGE="${RTE_CONTAINER_IMAGE}"

make -C "${PROJECT_DIR}" kube-update
make -C "${PROJECT_DIR}" wait-for-mcp
echo "Kubelet configured properly"

"${BASE_DIR}"/setup-e2e.sh
make -C "${PROJECT_DIR}" test-e2e
