#!/usr/bin/env bash

source hack/common.sh

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.no-color"
fi

setupreport

# Run uninstall test suite
echo "Running NRO uninstall test suite";
if ! "${BIN_DIR}"/e2e-nrop-uninstall.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.junit-report=${REPORT_DIR}/uninstall.xml; then
  echo "Failed to run NRO uninstall test suite"
  exit 2
fi
