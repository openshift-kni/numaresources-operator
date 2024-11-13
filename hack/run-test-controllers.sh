#! /usr/bin/env bash

REPO_DIR=$(dirname "${BASH_SOURCE[0]}")/..
KUBEBUILDER_ASSETS="${KUBEBUILDER_ASSETS}"

### controllers tests runner

echo "Running controller tests with platform:openshift"
export TEST_PLATFORM="openshift"
go clean -testcache && go test ./${REPO_DIR}/controllers/...

echo "Running controller tests with platform:hypershift"
export TEST_PLATFORM="hypershift"
go clean -testcache && go test ./${REPO_DIR}/controllers/...
