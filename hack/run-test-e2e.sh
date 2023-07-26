#!/usr/bin/env bash

set -e

source hack/common.sh

ENABLE_SCHED_TESTS="${ENABLE_SCHED_TESTS:-true}"

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.no-color"
fi

setupreport

# Run install test suite
echo "Running NRO install test suite"
${BIN_DIR}/e2e-nrop-install.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.fail-fast --ginkgo.junit-report=${REPORT_DIR}/install.xml --ginkgo.focus='\[Install\] continuousIntegration'

echo "Running Functional Tests: ${GINKGO_SUITS}"

echo "Deploying sample devices for RTE tests"
rte/hack/deploy-devices.sh

rte/hack/check-ds.sh oc sampledevices device-plugin-a-ds

echo "Running RTE tests"
export E2E_TOPOLOGY_MANAGER_POLICY="${E2E_TOPOLOGY_MANAGER_POLICY}"
export E2E_TOPOLOGY_MANAGER_SCOPE="${E2E_TOPOLOGY_MANAGER_SCOPE}"
# -v: print out the text and location for each spec before running it and flush output to stdout in realtime
# -timeout: exit the suite after the specified time
# -r: run suites recursively
# --fail-fast: ginkgo will stop the suite right after the first spec failure
# --flake-attempts: rerun the test if it fails
# -requireSuite: fail if tests are not executed because of missing suite
echo "TM config policy=[${E2E_TOPOLOGY_MANAGER_POLICY}] scope=[${E2E_TOPOLOGY_MANAGER_SCOPE}]"
${BIN_DIR}/e2e-nrop-rte.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.fail-fast --ginkgo.flake-attempts=2 --ginkgo.junit-report=${REPORT_DIR}/e2e.xml --ginkgo.skip='\[Disruptive\]|\[StateDirectories\]|\[NodeRefresh\]|\[local\]' --ginkgo.focus='\[release\]'

if [ "$ENABLE_SCHED_TESTS" = true ]; then
  # Run install test suite
  echo "Running NROScheduler install test suite"
  ${BIN_DIR}/e2e-nrop-sched-install.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.fail-fast --ginkgo.junit-report=${REPORT_DIR}/sched-install.xml

  echo "Running Functional Tests: ${GINKGO_SUITS}"
  # -v: print out the text and location for each spec before running it and flush output to stdout in realtime
  # -timeout: exit the suite after the specified time
  # -r: run suites recursively
  # --fail-fast: ginkgo will stop the suite right after the first spec failure
  # --flake-attempts: rerun the test if it fails
  # -requireSuite: fail if tests are not executed because of missing suite
  ${BIN_DIR}/e2e-nrop-sched.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.fail-fast --ginkgo.flake-attempts=2 --ginkgo.junit-report=${REPORT_DIR}/e2e-sched.xml

  echo "Running NROScheduler uninstall test suite";
  ${BIN_DIR}/e2e-nrop-sched-uninstall.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.junit-report=${REPORT_DIR}/sched-uninstall.xml
fi

echo "Undeploying sample devices for RTE tests"
rte/hack/undeploy-devices.sh

echo "Running NRO uninstall test suite";
${BIN_DIR}/e2e-nrop-uninstall.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.junit-report=${REPORT_DIR}/uninstall.xml
