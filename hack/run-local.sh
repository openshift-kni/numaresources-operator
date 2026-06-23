#!/usr/bin/env bash
#
# Run the numaresources-operator locally as its own ServiceAccount.
#
# Default flow: setup -> run -> teardown (no cluster leftovers).
#
# All positional arguments are passed through to bin/manager:
#   hack/run-local.sh --enable-metrics=false --leader-elect=false -v=6
#
# Standalone modes (run one stage and exit):
#   hack/run-local.sh --prepare    install CRDs + RBAC + Namespace, then exit
#   hack/run-local.sh --cleanup    remove  CRDs + RBAC + Namespace, then exit
# or manually:
#   bin/kustomize build config/local-run | kubectl apply -f -
#   bin/kustomize build config/local-run | kubectl delete --ignore-not-found -f -
#
# Script behavior is configured via environment variables only:
#   NAMESPACE       operator namespace            (default: numaresources)
#   SA_NAME         ServiceAccount name           (default: numaresources-controller-manager)
#   TOKEN_DURATION  SA token lifetime             (default: 3600s)
#   SKIP_SETUP      skip kustomize apply if true  (default: false)
#   SKIP_TEARDOWN   skip kustomize delete on exit (default: false)
#   BARE            skip both setup and teardown  (default: false)
#   KUSTOMIZE       path to kustomize             (default: bin/kustomize)
#   KUBECTL         path to kubectl               (default: kubectl)
#   OPERATOR_BIN    path to operator binary       (default: bin/manager)
#   OPERATOR_DEFAULT_ARGS  default args matching the Deployment (default: -v=4)
#   DRY_RUN         print commands without running (from hack/common.sh)

set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

NAMESPACE="${NAMESPACE:-numaresources}"
SA_NAME="${SA_NAME:-numaresources-controller-manager}"
TOKEN_DURATION="${TOKEN_DURATION:-14400s}" # the serial suite is slow

KUSTOMIZE="${KUSTOMIZE:-${BIN_DIR}/kustomize}"
KUBECTL="${KUBECTL:-kubectl}"
OPERATOR_BIN="${OPERATOR_BIN:-${BIN_DIR}/manager}"
OPERATOR_DEFAULT_ARGS="${OPERATOR_DEFAULT_ARGS:-v=4}"

if [[ "${BARE:-false}" == "true" ]]; then
	SKIP_SETUP=true
	SKIP_TEARDOWN=true
fi
SKIP_SETUP="${SKIP_SETUP:-false}"
SKIP_TEARDOWN="${SKIP_TEARDOWN:-false}"

if [[ "${1:-}" == "--prepare" ]]; then
	echo "Installing CRDs + RBAC + Namespace..."
	runcmd "${KUSTOMIZE} build ${REPO_DIR}/config/local-run | ${KUBECTL} apply -f -"
	exit 0
fi

if [[ "${1:-}" == "--cleanup" ]]; then
	echo "Removing CRDs + RBAC + Namespace..."
	runcmd "${KUSTOMIZE} build ${REPO_DIR}/config/local-run | ${KUBECTL} delete --ignore-not-found -f -"
	exit 0
fi

SA_KUBECONFIG=""
CA_TMPFILE=""

cleanup() {
	local exit_code=$?
	if [[ -n "${CA_TMPFILE}" && -f "${CA_TMPFILE}" ]]; then
		rm -f "${CA_TMPFILE}"
	fi
	if [[ -n "${SA_KUBECONFIG}" && -f "${SA_KUBECONFIG}" ]]; then
		rm -f "${SA_KUBECONFIG}"
	fi
	if [[ "${SKIP_TEARDOWN}" != "true" ]]; then
		echo ""
		echo "Tearing down CRDs + RBAC + Namespace..."
		runcmd "${KUSTOMIZE} build ${REPO_DIR}/config/local-run | ${KUBECTL} delete --ignore-not-found -f -" || true
	fi
	exit "${exit_code}"
}
trap cleanup EXIT INT TERM

if [[ "${SKIP_SETUP}" != "true" ]]; then
	echo "Installing CRDs + RBAC + Namespace..."
	runcmd "${KUSTOMIZE} build ${REPO_DIR}/config/local-run | ${KUBECTL} apply -f -"
fi

# Create a short-lived SA token
if [[ "${DRY_RUN}" == "true" ]]; then
	echo "DRY_RUN: would create SA token (duration: ${TOKEN_DURATION})"
	SA_TOKEN="dry-run-token"
else
	echo "Creating SA token (duration: ${TOKEN_DURATION})..."
	SA_TOKEN=$(${KUBECTL} create token "${SA_NAME}" \
		--namespace "${NAMESPACE}" \
		--duration "${TOKEN_DURATION}")
fi

# Build SA kubeconfig
CLUSTER_SERVER=$(${KUBECTL} config view --minify -o jsonpath='{.clusters[0].cluster.server}')
CLUSTER_CA_DATA=$(${KUBECTL} config view --minify --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
CLUSTER_NAME=$(${KUBECTL} config view --minify -o jsonpath='{.clusters[0].name}')

if [[ -z "${CLUSTER_CA_DATA}" ]]; then
	CA_FILE=$(${KUBECTL} config view --minify -o jsonpath='{.clusters[0].cluster.certificate-authority}')
	if [[ -n "${CA_FILE}" && -f "${CA_FILE}" ]]; then
		CLUSTER_CA_DATA=$(base64 -w0 < "${CA_FILE}")
	else
		echo "WARNING: could not extract cluster CA certificate"
	fi
fi

SA_KUBECONFIG=$(mktemp "${BIN_DIR}/.sa-kubeconfig.XXXXXX")

if [[ -n "${CLUSTER_CA_DATA}" ]]; then
	CA_TMPFILE=$(mktemp "${BIN_DIR}/.ca-cert.XXXXXX")
	echo "${CLUSTER_CA_DATA}" | base64 -d > "${CA_TMPFILE}"
	KUBECONFIG="${SA_KUBECONFIG}" ${KUBECTL} config set-cluster "${CLUSTER_NAME}" \
		--server="${CLUSTER_SERVER}" \
		--certificate-authority="${CA_TMPFILE}" \
		--embed-certs=true > /dev/null
else
	KUBECONFIG="${SA_KUBECONFIG}" ${KUBECTL} config set-cluster "${CLUSTER_NAME}" \
		--server="${CLUSTER_SERVER}" \
		--embed-certs=false > /dev/null
fi
KUBECONFIG="${SA_KUBECONFIG}" ${KUBECTL} config set-credentials "sa-${SA_NAME}" \
	--token="${SA_TOKEN}" > /dev/null
KUBECONFIG="${SA_KUBECONFIG}" ${KUBECTL} config set-context "local-run" \
	--cluster="${CLUSTER_NAME}" \
	--user="sa-${SA_NAME}" \
	--namespace="${NAMESPACE}" > /dev/null
KUBECONFIG="${SA_KUBECONFIG}" ${KUBECTL} config use-context "local-run" > /dev/null

echo "Verifying SA authentication..."
if ${KUBECTL} --kubeconfig="${SA_KUBECONFIG}" auth can-i get numaresourcesoperators.nodetopology.openshift.io 2>/dev/null; then
	echo "SA authentication verified"
else
	echo "WARNING: SA may not have expected permissions yet"
fi

echo ""
echo "Running operator as SA ${SA_NAME} (token expires in ${TOKEN_DURATION})"
echo "  namespace: ${NAMESPACE}"
echo "  args:      ${OPERATOR_DEFAULT_ARGS} $*"
echo ""

if [[ "${DRY_RUN}" == "true" ]]; then
	echo "DRY_RUN: would execute ${OPERATOR_BIN} ${OPERATOR_DEFAULT_ARGS} $*"
else
	KUBECONFIG="${SA_KUBECONFIG}" \
	NAMESPACE="${NAMESPACE}" \
		"${OPERATOR_BIN}" ${OPERATOR_DEFAULT_ARGS} "$@"
fi
