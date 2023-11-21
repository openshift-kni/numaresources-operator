#!/usr/bin/env bash

set -eu

source hack/common.sh

FILES=("serial" "must-gather" "tools")
MARKERS=("### serial suite runner" "### must-gather suite runner" "### tools suite runner")

for ((i=0; i<${#FILES[@]}; i++)); do
    TMPFILE=$(mktemp "/tmp/rune2e_${i}.XXXXXX")
    MARKER=$(grep -Fn "${MARKERS[$i]}" hack/run-test-${FILES[$i]}-e2e.sh | cut -d: -f1)

    function cleanup() {
        rm ${TMPFILE}
    }

    trap 'cleanup' EXIT SIGINT SIGTERM SIGSTOP

    grep -v 'REPO_DIR' hack/common.sh >> ${TMPFILE}
    echo -e "BIN_DIR=\"/usr/local/bin\"\n" >> ${TMPFILE}
    tail -n+${MARKER} "hack/run-test-${FILES[$i]}-e2e.sh" >> ${TMPFILE}

    mkdir -p ${REPO_DIR}/bin && install -m 0755 ${TMPFILE} ${REPO_DIR}/bin/run-e2e-nrop-${FILES[$i]}.sh
done
