#!/usr/bin/env bash

source hack/common.sh

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.no-color"
fi

setupreport

# Run install test suite
echo "Running NRO install test suite"
if ! "${BIN_DIR}"/e2e-nrop-install.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.fail-fast --ginkgo.junit-report=${REPORT_DIR}/install.xml --ginkgo.focus='\[Install\] durability'; then
  echo "Failed to run NRO install test suite"
  exit 1
fi
