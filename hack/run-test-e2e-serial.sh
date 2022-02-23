#!/usr/bin/env bash

source hack/common.sh

# we expect the lane to run against a already configured cluster
SETUP="${SETUP:-false}"
TEARDOWN="${TEARDOWN:-false}"
RUN_TESTS="${RUN_TESTS:-true}"

# so few arguments is no bother enough for getopt
for arg in "$@"; do
	case "${arg}" in
		--setup) SETUP="true";;
		--teardown) TEARDOWN="true";;
		--no-run-tests) RUN_TESTS="false";;
	esac
done

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.noColor"
fi

if [ -n "${E2E_SERIAL_FOCUS}" ]; then
	FOCUS="-ginkgo.focus=${E2E_SERIAL_FOCUS}"
fi

function setup() {
	if [[ "${SETUP}" != "true" ]]; then
		return 0
	fi

	echo "Running NRO install test suite"
	${BIN_DIR}/e2e-install.test ${NO_COLOR} --ginkgo.v --ginkgo.failFast --ginkgo.reportFile=/tmp/artifacts/e2e-serial-install --ginkgo.focus='\[Install\] continuousIntegration'

	RC="$?"
	if [[ "${RC}" != "0" ]]; then
		return ${RC}
	fi

	echo "Running NROScheduler install test suite"
	${BIN_DIR}/e2e-sched-install.test ${NO_COLOR} --ginkgo.v --ginkgo.failFast --ginkgo.reportFile=/tmp/artifacts/e2e-serial-install-sched
}

function teardown() {
	if [[ "${TEARDOWN}" != "true" ]]; then
		return 0
	fi

	echo "Running NROScheduler uninstall test suite";
	${BIN_DIR}/e2e-sched-uninstall.test ${NO_COLOR} --ginkgo.v --ginkgo.reportFile=/tmp/artifacts/e2e-serial-uninstall-sched

	RC="$?"
	if [[ "${RC}" != "0" ]]; then
		return ${RC}
	fi

	echo "Running NRO uninstall test suite";
	${BIN_DIR}/e2e-uninstall.test ${NO_COLOR} --ginkgo.v --ginkgo.reportFile=/tmp/artifacts/e2e-serial-uninstall
}

function runtests() {
	if [[ "${RUN_TESTS}" != "true" ]]; then
		echo "running no tests"
		return 0
	fi
	echo "Running Serial, disruptive E2E Tests"
	${BIN_DIR}/e2e-serial.test ${NO_COLOR} --ginkgo.v --ginkgo.reportFile=/tmp/artifacts/e2e-serial-run --test.parallel=1 ${FOCUS}
}

# Make sure that we always properly clean the environment
trap 'teardown' EXIT SIGINT SIGTERM SIGSTOP

setup
if [ $? -ne 0 ]; then
    echo "Failed to install NRO"
    exit 1
fi
runtests
