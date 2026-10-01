#!/usr/bin/env bash

set -e

source hack/common.sh

# Like the serial suite, this runner expects an already configured cluster.
setupreport

echo "Running numazone E2E Tests"
"${BIN_DIR}/e2e-nrop-numazone.test" \
    --ginkgo.v \
    --ginkgo.timeout=30m \
    --ginkgo.junit-report="${REPORT_DIR}/e2e-numazone.xml" \
    "$@"
