#!/bin/bash

set -xue

# expect oc to be in PATH by default
OC_TOOL="${OC_TOOL:-oc}"
RTE_NAMESPACE="${RTE_NAMESPACE:-rte-e2e}"

echo "Undeploy RTE"
"$OC_TOOL" delete clusterrolebinding handle-rte
"$OC_TOOL" delete clusterrole rte-handler
"$OC_TOOL" delete ns "${RTE_NAMESPACE}"

echo "Delete CRD"
"$OC_TOOL" delete -f manifests/crd.yaml
