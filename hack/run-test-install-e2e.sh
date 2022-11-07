#!/usr/bin/env bash

source hack/common.sh

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.no-color"
fi

setupreport

# Make sure that we always properly clean the environment
trap '{ echo "Running NRO uninstall test suite"; ${BIN_DIR}/e2e-nrop-uninstall.test ${NO_COLOR} --ginkgo.v --ginkgo.junit-report=${REPORT_DIR}/uninstall.xml; }' EXIT SIGINT SIGTERM SIGSTOP

# Run install test suite
echo "Running NRO install test suite"
${BIN_DIR}/e2e-nrop-install.test ${NO_COLOR} --ginkgo.v --ginkgo.fail-fast --ginkgo.junit-report=${REPORT_DIR}/install.xml --ginkgo.focus='\[Install\] durability'
