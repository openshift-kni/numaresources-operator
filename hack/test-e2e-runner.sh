#!/usr/bin/env bash


set -eu

source hack/common.sh

echo "Ginkgo Version: " `ginkgo version`

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
Running: /usr/local/bin/e2e-nrop-serial.test --ginkgo.v --test.parallel=1 --ginkgo.output-dir=/tmp/artifacts/nrop --ginkgo.junit-report=e2e-serial-run -ginkgo.no-color"
if [ "${GOT}" != "${EXPECTED}" ]; then
	echo "[FAIL] --dry-run unexpected output: ${GOT}"
	exit 1
fi
echo "[PASS] --dry-run produced the expected output"

