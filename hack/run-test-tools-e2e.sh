#!/usr/bin/env bash

source hack/common.sh

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it" 1>&2
  NO_COLOR="-ginkgo.noColor"
fi

if [ -n "${E2E_TOOLS_FOCUS}" ]; then
	FOCUS="-ginkgo.focus=${E2E_TOOLS_FOCUS}"
fi
if [ -n "${E2E_TOOLS_SKIP}" ]; then
	SKIP="-ginkgo.skip=${E2E_TOOLS_SKIP}"
fi

DRY_RUN="false"
REPORT_DIR="/tmp/artifacts/nrop"
REPORT_FILE=""

# so few arguments is no bother enough for getopt
while [[ $# -gt 0 ]]; do
	case "$1" in
		--no-color)
			NO_COLOR="-ginkgo.noColor"
			shift
			;;
		--dry-run)
			DRY_RUN="true"
			shift
			;;
		--focus)
			FOCUS="-ginkgo.focus=$2"
			shift
			shift
			;;
		--skip)
			SKIP="-ginkgo.skip=$2"
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
	REPORT_FILE="${REPORT_DIR}/e2e-tools-run"
fi


echo "Running Tools, mostly local, E2E Tests"
runcmd ${BIN_DIR}/e2e-nrop-tools.test \
	--ginkgo.v \
	--test.parallel=1 \
	--ginkgo.reportFile=${REPORT_FILE} \
	${NO_COLOR} \
	${SKIP} \
	${FOCUS} \
	""
