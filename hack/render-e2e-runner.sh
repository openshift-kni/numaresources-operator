#!/usr/bin/env bash

set -eu

source hack/common.sh

TMPFILE=$(mktemp /tmp/rune2e.XXXXXX)
MARKER=$(grep -Fn '### serial suite runner' hack/run-test-serial-e2e.sh | cut -d: -f1)

function cleanup() {
	rm ${TMPFILE}
}

trap 'cleanup' EXIT SIGINT SIGTERM SIGSTOP

grep -v 'REPO_DIR' hack/common.sh >> ${TMPFILE}
echo -e 'BIN_DIR="/usr/local/bin"\n' >> ${TMPFILE}
tail -n+${MARKER} hack/run-test-serial-e2e.sh >> ${TMPFILE}

mkdir -p ${REPO_DIR}/bin && install -m  0755 ${TMPFILE} ${REPO_DIR}/bin/run-e2e-nrop-serial.sh
