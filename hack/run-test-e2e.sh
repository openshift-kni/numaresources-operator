#!/usr/bin/env bash

source hack/common.sh

ENABLE_SCHED_TESTS="${ENABLE_SCHED_TESTS:-false}"
NO_TEARDOWN="${NO_TEARDOWN:-false}"

function test_sched() {
  # Run install test suite
  echo "Running NROScheduler install test suite"
  ginkgo ${NO_COLOR} -v -fail-fast -output-dir=/tmp/artifacts/nrop -junit-report=sched-install ${BIN_DIR}/e2e-nrop-sched-install.test

  # The install failed, no taste to continue
  if [ $? -ne 0 ]; then
      echo "Failed to install NROScheduler"
      exit 1
  fi

  echo "Running Functional Tests: ${GINKGO_SUITS}"
  # -v: print out the text and location for each spec before running it and flush output to stdout in realtime
  # -r: run suites recursively
  # --failFast: ginkgo will stop the suite right after the first spec failure
  # --flake-attempts: rerun the test if it fails
  # -requireSuite: fail if tests are not executed because of missing suite
  ginkgo ${NO_COLOR} -v -fail-fast -flake-attempts=2 -output-dir=/tmp/artifacts/nrop -junit-report=e2e-sched ${BIN_DIR}/e2e-nrop-sched.test
}

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-no-color"
fi

# Make sure that we always properly clean the environment
trap '{ if [ "${NO_TEARDOWN}" = false ]; then
          if [ "${ENABLE_SCHED_TESTS}" = true ]; then
             echo "Running NROScheduler uninstall test suite";
             ginkgo ${NO_COLOR} -v -output-dir=/tmp/artifacts/nrop -junit-report=sched-uninstall ${BIN_DIR}/e2e-nrop-sched-uninstall.test;
          fi
          echo "Running NRO uninstall test suite";
          ginkgo ${NO_COLOR} -v -output-dir=/tmp/artifacts/nrop -junit-report=uninstall ${BIN_DIR}/e2e-nrop-uninstall.test;
        fi }' EXIT SIGINT SIGTERM SIGSTOP

# Run install test suite
echo "Running NRO install test suite"
ginkgo ${NO_COLOR} -v -fail-fast -output-dir=/tmp/artifacts/nrop -junit-report=install -focus='\[Install\] continuousIntegration' ${BIN_DIR}/e2e-nrop-install.test 

# The install failed, no taste to continue
if [ $? -ne 0 ]; then
    echo "Failed to install NRO"
    exit 1
fi


echo "Running Functional Tests: ${GINKGO_SUITS}"
export E2E_TOPOLOGY_MANAGER_POLICY="${E2E_TOPOLOGY_MANAGER_POLICY:-SingleNUMANodePodLevel}"
# -v: print out the text and location for each spec before running it and flush output to stdout in realtime
# -r: run suites recursively
# --failFast: ginkgo will stop the suite right after the first spec failure
# --flake-attempts: rerun the test if it fails
# -requireSuite: fail if tests are not executed because of missing suite
ginkgo ${NO_COLOR} -v -fail-fast -flake-attempts=2 -output-dir=/tmp/artifacts/nrop -junit-report=e2e -skip='\[Disruptive\]|\[StateDirectories\]' ${BIN_DIR}/e2e-nrop-rte.test 


if [ "$ENABLE_SCHED_TESTS" = true ]; then
  test_sched
fi
