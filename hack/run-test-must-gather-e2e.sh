#! /usr/bin/env bash

source hack/common.sh

### must-gather suite runner

NO_COLOR=""
if ! which tput &> /dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
  echo "Terminal does not seem to support colored output, disabling it"
  NO_COLOR="-ginkgo.no-color"
fi

FOCUS="-ginkgo.focus='\\[must-gather\\]'"
SKIP=""
LABEL_FILTER=""
VERBOSE=""
REPORT_FILE="${REPORT_DIR}/must-gather.xml"

# Keep argument handling compatible with run-e2e-nrop-serial.sh so
# Jenkins-generated scripts can reuse the same options for must-gather.
while [[ $# -gt 0 ]]; do
	case "$1" in
		--no-color)
			NO_COLOR="-ginkgo.no-color"
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
		--label-filter)
			LABEL_FILTER="-ginkgo.label-filter='$2'"
			shift
			shift
			;;
		--report-file)
			REPORT_FILE="$2"
			shift
			shift
			;;
		--verbose)
			VERBOSE="${VERBOSE} -ginkgo.v"
			shift
			;;
		--debug)
			VERBOSE="${VERBOSE} -ginkgo.vv -test.v"
			shift
			;;
		--help)
			echo "usage: $0 [options]"
			echo "[options] are always '--opt val' and never '--opt=val'"
			echo "available options:"
			echo "--no-color              force colored output to off"
			echo "--dry-run               logs what about to do, but don't actually do it"
			echo "--focus <regex>         only run cases matching <regex> (passed to -ginkgo.focus)"
			echo "--skip <regex>          skip cases matching <regex> (passed to -ginkgo.skip)"
			echo "--label-filter <query>  filter cases based on the matching <query> (passed to -ginkgo.label-filter)"
			echo "--report-file <file>    write report file for this suite on <file>"
			echo "--verbose               add debug output (becomes -ginkgo.v)"
			echo "--debug                 add even more debug output (becomes -ginkgo.vv + other settings)"
			echo "--help                  shows this message and helps correctly"
			exit 0
			;;
		*)
			echo "unrecognized option: $1"
			echo "use the form --opt val and not the form --opt=val"
			exit 1
			;;
	esac
done

setupreport

# Run must-gather test suite
echo "Running NRO must-gather test suite"
runcmd ${BIN_DIR}/e2e-nrop-must-gather.test \
	--ginkgo.v \
	--ginkgo.timeout=20m \
	--ginkgo.junit-report=${REPORT_FILE} \
	${NO_COLOR} \
	${VERBOSE} \
	${SKIP} \
	${LABEL_FILTER} \
	${FOCUS}
