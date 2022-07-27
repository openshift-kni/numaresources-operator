#! /usr/bin/env bash

source hack/common.sh

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.noColor"
fi


# Run must-gather test suite
echo "Running NRO must-gather test suite"
${BIN_DIR}/e2e-nrop-must-gather.test ${NO_COLOR} --ginkgo.v --ginkgo.failFast --ginkgo.reportFile=/tmp/artifacts/nrop/must-gather --ginkgo.focus='\[must-gather\]'
