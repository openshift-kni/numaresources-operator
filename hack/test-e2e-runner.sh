#!/usr/bin/env bash


set -eu

source hack/common.sh

RUNNER="$( realpath ${REPO_DIR}/bin/run-e2e-nrop-serial.sh )"

echo "basic sanity tests for ${RUNNER}:"

echo "[TEST] runner exists"
if [ ! -x ${RUNNER} ]; then
	echo "[FAIL] runner script not found"
	exit 1
fi
echo "[PASS] runner script found"

echo "[TEST] --dry-run exits cleanly"
$RUNNER --dry-run &> /dev/null
if [ "$?" != "0" ]; then
	echo "[FAIL] --dry-run returned non-zero exit code ${RC}"
	exit 1
fi
echo "[PASS] --dry-run returned zero exit code"

echo "[TEST] --dry-run produces expected output"
GOT=$( $RUNNER --dry-run --no-color)
EXPECTED="Running Serial, disruptive E2E Tests
Running: ginkgo -v -parallel=1 -output-dir=/tmp/artifacts/nrop -junit-report=e2e-serial-run -no-color /usr/local/bin/e2e-nrop-serial.test"
if [ "${GOT}" != "${EXPECTED}" ]; then
	echo "[FAIL] --dry-run unexpected output: ${GOT}"
	exit 1
fi
echo "[PASS] --dry-run produced the expected output"

