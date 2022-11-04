#!/usr/bin/env bash

source hack/common.sh

### serial suite runner
# we expect the lane to run against a already configured cluster
# we use this verbose form to play nice with envsubst
if [ -z "${SETUP}" ]; then
	SETUP="false"
fi
if [ -z "${TEARDOWN}" ]; then
	TEARDOWN="false"
fi
if [ -z "${RUN_TESTS}" ]; then
	RUN_TESTS="true"
fi

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it" 1>&2
  NO_COLOR="-ginkgo.noColor"
fi

if [ -n "${E2E_SERIAL_FOCUS}" ]; then
	FOCUS="-ginkgo.focus=${E2E_SERIAL_FOCUS}"
fi
if [ -n "${E2E_SERIAL_SKIP}" ]; then
	SKIP="-ginkgo.skip=${E2E_SERIAL_SKIP}"
fi

DRY_RUN="false"
REPORT_DIR="/tmp/artifacts/nrop"
REPORT_FILE=""

# so few arguments is no bother enough for getopt
while [[ $# -gt 0 ]]; do
	case "$1" in
		--setup)
			SETUP="true"
			shift
			;;
		--teardown)
			TEARDOWN="true"
			shift
			;;
		--no-run-tests)
			RUN_TESTS="false"
			shift
			;;
		--no-color)
			NO_COLOR="-ginkgo.noColor"
			shift
			;;
		--dry-run)
			DRY_RUN="true"
			shift
			;;
		--focus)
			FOCUS="-ginkgo.focus='$2'"
			shift
			shift
			;;
		--skip)
			SKIP="-ginkgo.skip='$2'"
			shift
			shift
			;;
		--report-dir)
			REPORT_DIR="$2"
			shift
			shift
			;;
		--report-file)
			REPORT_FILE="$2"
			shift
			shift
			;;
		--help)
			echo "usage: $0 [options]"
			echo "[options] are always '--opt val' and never '--opt=val'"
			echo "available options:"
			echo "--setup               perform cluster setup before to run the tests (if enabled, see --no-run-tests)"
			echo "--teardown            perform cluster teardown after having run the tests (if enabled, see --no-run-tests)"
			echo "--no-run-tests        don't run the tests (see --setup and --teardown)"
			echo "--no-color            force colored output to off"
			echo "--dry-run             logs what about to do, but don't actually do it"
			echo "--focus <regex>       only run cases matching <regex> (passed to -ginkgo.focus)"
			echo "--skip <regex>        skip cases matching <regex> (passed to -ginkgo.skip)"
			echo "--report-dir <dir>    write report artifacts on <dir>"
			echo "--report-file <file>  write report file for this suite on <file>"
			echo "--help                shows this message and helps correctly"
			exit 0
			;;
		*)
			echo "unrecognized option: $1"
			echo "use the form --opt val and not the form --opt=val"
			exit 1
			;;
	esac
done

# mandatory
if [ -z "${REPORT_FILE}" ] && [ -z "${REPORT_DIR}" ]; then
	echo "invalid report directory"
	exit 1
fi

if [ -z "${REPORT_FILE}" ]; then
	REPORT_FILE="${REPORT_DIR}/e2e-serial-run"
fi

function setup() {
	if [[ "${SETUP}" != "true" ]]; then
		return 0
	fi

	echo "Running NRO install test suite"
	runcmd ${BIN_DIR}/e2e-nrop-install.test \
		--ginkgo.v \
		--ginkgo.failFast \
		--ginkgo.reportFile=${REPORT_DIR}/e2e-serial-install \
		--ginkgo.focus='\[Install\] continuousIntegration' \
		${NO_COLOR}

	RC="$?"
	if [[ "${RC}" != "0" ]]; then
		return ${RC}
	fi

	echo "Running NROScheduler install test suite"
	runcmd ${BIN_DIR}/e2e-nrop-sched-install.test \
		--ginkgo.v \
		--ginkgo.failFast \
		--ginkgo.reportFile=${REPORT_DIR}/e2e-serial-install-sched \
		${NO_COLOR}
}

function teardown() {
	if [[ "${TEARDOWN}" != "true" ]]; then
		return 0
	fi

	echo "Running NROScheduler uninstall test suite";
	runcmd ${BIN_DIR}/e2e-nrop-sched-uninstall.test \
		--ginkgo.v \
		--ginkgo.reportFile=${REPORT_DIR}/e2e-serial-uninstall-sched \
		${NO_COLOR}

	RC="$?"
	if [[ "${RC}" != "0" ]]; then
		return ${RC}
	fi

	echo "Running NRO uninstall test suite";
	runcmd ${BIN_DIR}/e2e-nrop-uninstall.test \
		--ginkgo.v \
		--ginkgo.reportFile=${REPORT_DIR}/e2e-serial-uninstall \
		${NO_COLOR}
}

function runtests() {
	if [[ "${RUN_TESTS}" != "true" ]]; then
		echo "running no tests"
		return 0
	fi
	echo "Running Serial, disruptive E2E Tests"
	runcmd ${BIN_DIR}/e2e-nrop-serial.test \
		--ginkgo.v \
		--ginkgo.reportFile=${REPORT_FILE} \
		${NO_COLOR} \
		${SKIP} \
		${FOCUS}
}

# Make sure that we always properly clean the environment
trap 'teardown' EXIT SIGINT SIGTERM SIGSTOP

setup
if [ $? -ne 0 ]; then
    echo "Failed to install NRO"
    exit 1
fi
runtests
