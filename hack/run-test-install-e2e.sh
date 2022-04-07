#!/usr/bin/env bash

source hack/common.sh

exit 0

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.noColor"
fi

# Make sure that we always properly clean the environment
trap '{ echo "Running NRO uninstall test suite"; ${BIN_DIR}/e2e-nrop-uninstall.test ${NO_COLOR} --ginkgo.v --ginkgo.reportFile=/tmp/artifacts/uninstall; }' EXIT SIGINT SIGTERM SIGSTOP

# Run install test suite
echo "Running NRO install test suite"
${BIN_DIR}/e2e-nrop-install.test ${NO_COLOR} --ginkgo.v --ginkgo.failFast --ginkgo.reportFile=/tmp/artifacts/install --ginkgo.focus='\[Install\] durability'
