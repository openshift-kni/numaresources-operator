#!/usr/bin/env bash

source hack/common.sh

echo "Ginkgo Version: " `ginkgo version`

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.no-color"
fi

# Make sure that we always properly clean the environment
trap '{ echo "Running NRO uninstall test suite"; ${BIN_DIR}/e2e-nrop-uninstall.test ${NO_COLOR} --ginkgo.v --ginkgo.junit-report=uninstall; }' EXIT SIGINT SIGTERM SIGSTOP

# Run install test suite
echo "Running NRO install test suite"
${BIN_DIR}/e2e-nrop-install.test ${NO_COLOR} --ginkgo.v --ginkgo.fail-fast --ginkgo.junit-report=install --ginkgo.focus='\[Install\] durability'
