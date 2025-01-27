#!/bin/bash
set -eu

DIFF=$( gofmt -s -d api internal pkg rte test )
if [ -n "${DIFF}" ]; then
	echo "${DIFF}"
	exit 1
fi
exit 0
