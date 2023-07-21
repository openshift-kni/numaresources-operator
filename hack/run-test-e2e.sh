#!/usr/bin/env bash

source hack/common.sh

ENABLE_SCHED_TESTS="${ENABLE_SCHED_TESTS:-true}"
NO_TEARDOWN="${NO_TEARDOWN:-false}"

function test_sched() {
  # Run install test suite
  echo "Running NROScheduler install test suite"
  ${BIN_DIR}/e2e-nrop-sched-install.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.fail-fast --ginkgo.junit-report=${REPORT_DIR}/sched-install.xml

  # The install failed, no taste to continue
  if [ $? -ne 0 ]; then
      echo "Failed to install NROScheduler"
      exit 1
  fi

  echo "Running Functional Tests: ${GINKGO_SUITS}"
  # -v: print out the text and location for each spec before running it and flush output to stdout in realtime
  # -timeout: exit the suite after the specified time
  # -r: run suites recursively
  # --fail-fast: ginkgo will stop the suite right after the first spec failure
  # --flake-attempts: rerun the test if it fails
  # -requireSuite: fail if tests are not executed because of missing suite
  ${BIN_DIR}/e2e-nrop-sched.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.fail-fast --ginkgo.flake-attempts=2 --ginkgo.junit-report=${REPORT_DIR}/e2e-sched.xml
}

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.no-color"
fi

setupreport

# Make sure that we always properly clean the environment
trap '{ if [ "${NO_TEARDOWN}" = false ]; then
          if [ "${ENABLE_SCHED_TESTS}" = true ]; then
             echo "Running NROScheduler uninstall test suite";
             ${BIN_DIR}/e2e-nrop-sched-uninstall.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.junit-report=${REPORT_DIR}/sched-uninstall.xml
          fi

          echo "Undeploying sample devices for RTE tests"
          rte/hack/undeploy-devices.sh

          echo "Running NRO uninstall test suite";
	  ${BIN_DIR}/e2e-nrop-uninstall.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.junit-report=${REPORT_DIR}/uninstall.xml
        fi }' EXIT SIGINT SIGTERM SIGSTOP

# Run install test suite
echo "Running NRO install test suite"
${BIN_DIR}/e2e-nrop-install.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.fail-fast --ginkgo.junit-report=${REPORT_DIR}/install.xml --ginkgo.focus='\[Install\] continuousIntegration'

# The install failed, no reason to continue
if [ $? -ne 0 ]; then
    echo "Failed to install NRO"
    exit 1
fi


echo "Running Functional Tests: ${GINKGO_SUITS}"

echo "Deploying sample devices for RTE tests"
rte/hack/deploy-devices.sh
if [ $? -ne 0 ]; then
    echo "Failed to deploy sample device plugin for RTE tests"
    exit 2
fi

rte/hack/check-ds.sh oc sampledevices device-plugin-a-ds
if [ $? -ne 0 ]; then
    echo "Failed to verify sample device plugin for RTE tests"
    exit 4
fi

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
  test_sched
fi
