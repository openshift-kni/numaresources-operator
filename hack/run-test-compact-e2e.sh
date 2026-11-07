#!/usr/bin/env bash

set -e

source hack/common.sh

ENABLE_CLEANUP="${ENABLE_CLEANUP:-false}"

export E2E_NODE_ROLE="master"

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.no-color"
fi

setupreport

echo "Running NRO install test suite"
${BIN_DIR}/e2e-nrop-install.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.fail-fast --ginkgo.junit-report=${REPORT_DIR}/install.xml --ginkgo.focus='\[Install\] continuousIntegration'

echo "Running NROScheduler install test suite"
${BIN_DIR}/e2e-nrop-sched-install.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.fail-fast --ginkgo.junit-report=${REPORT_DIR}/sched-install.xml

echo "Deploying sample devices for RTE tests"
rte/hack/deploy-devices.sh
rte/hack/check-ds.sh oc sampledevices device-plugin-a-ds
rte/hack/check-ds.sh oc sampledevices device-plugin-hostlevel-a-ds

# OCPBUGS-90597: host-level device is present; run the gated serial coverage
export E2E_NROP_PFP_HOSTLEVEL_SIM="${E2E_NROP_PFP_HOSTLEVEL_SIM:-1}"
echo "Running OCPBUGS-90597 serial coverage (feature:pfphostlevel)"
hack/run-test-serial-e2e.sh --focus 'should schedule a Guaranteed TAS pod when host-level extended resources are held' --report-file "${REPORT_DIR}/e2e-serial-pfphostlevel.xml" --verbose

echo "Running Functional Tests: ${GINKGO_SUITS}"
${BIN_DIR}/e2e-nrop-sched.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.flake-attempts=2 --ginkgo.junit-report=${REPORT_DIR}/e2e-sched.xml

if [ "$ENABLE_CLEANUP" = true ]; then
  echo "Undeploying sample devices for RTE tests"
  rte/hack/undeploy-devices.sh

  echo "Running NROScheduler uninstall test suite";
  ${BIN_DIR}/e2e-nrop-sched-uninstall.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.junit-report=${REPORT_DIR}/sched-uninstall.xml

  echo "Running NRO uninstall test suite";
  ${BIN_DIR}/e2e-nrop-uninstall.test ${NO_COLOR} --ginkgo.v --ginkgo.timeout=5h --ginkgo.junit-report=${REPORT_DIR}/uninstall.xml
fi
