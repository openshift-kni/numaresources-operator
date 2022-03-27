#!/usr/bin/env bash

set -eu

source hack/common.sh

mkdir -p ${REPO_DIR}/bin
grep -v 'source hack/common.sh' ${REPO_DIR}/hack/run-test-serial-e2e.sh | \
	env BIN_DIR=/usr/local/bin ${REPO_DIR}/bin/envsubst '${BIN_DIR}' > ${REPO_DIR}/bin/run-e2e-nrop-serial.sh
chmod 0755 ${REPO_DIR}/bin/run-e2e-nrop-serial.sh
