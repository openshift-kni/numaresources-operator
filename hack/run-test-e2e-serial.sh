#!/usr/bin/env bash

source hack/common.sh

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.noColor"
fi

if [ -n "${E2E_SERIAL_FOCUS}" ]; then
	FOCUS="-ginkgo.focus=${E2E_SERIAL_FOCUS}"
fi

echo "Running Serial, disruptive E2E Tests"
${BIN_DIR}/e2e-serial.test ${NO_COLOR} --ginkgo.v --ginkgo.failFast --ginkgo.reportFile=/tmp/artifacts/e2e-serial --test.parallel=1 ${FOCUS}
