#!/usr/bin/env bash

source hack/common.sh

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.no-color"
fi

setupreport

# Run upgrade test suite
echo "Running NRO upgrade test suite"
if ! "${BIN_DIR}"/e2e-nrop-upgrade.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=1h --ginkgo.fail-fast --ginkgo.junit-report=${REPORT_DIR}/install.xml --ginkgo.label-filter="upgrade"; then
  echo "Failed to run NRO upgrade test suite"
  exit 1
fi
