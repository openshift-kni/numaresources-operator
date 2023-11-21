#! /usr/bin/env bash

source hack/common.sh

### must-gather suite runner

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.no-color"
fi

setupreport

# Run must-gather test suite
echo "Running NRO must-gather test suite"
${BIN_DIR}/e2e-nrop-must-gather.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.fail-fast --ginkgo.junit-report=${REPORT_DIR}/must-gather.xml --ginkgo.focus='\[must-gather\]'
