#!/usr/bin/env bash

REPO_DIR=$(dirname "${BASH_SOURCE[0]}")/..
BIN_DIR="${REPO_DIR}/bin"

function runcmd() {
	echo "Running: $@"
	if [[ "${DRY_RUN}" == "true" ]]; then
		return 0
	fi
	eval $@
}

