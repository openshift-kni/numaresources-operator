#!/usr/bin/env bash

source hack/common.sh

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-no-color"
fi

# Make sure that we always properly clean the environment
trap '{ echo "Running NRO uninstall test suite"; ginkgo ${NO_COLOR} -v -output-dir=/tmp/artifacts -junit-report=uninstall ${BIN_DIR}/e2e-nrop-uninstall.test; }' EXIT SIGINT SIGTERM SIGSTOP

# Run install test suite
echo "Running NRO install test suite"
ginkgo -v -fail-fast -junit-report=install -focus='\[Install\] durability' -output-dir=/tmp/artifacts ${BIN_DIR}/e2e-nrop-install.test
