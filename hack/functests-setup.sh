#!/bin/bash

set -xe

E2E_NAMESPACE_NAME=$1

# expect oc to be in PATH by default
OC_TOOL="${OC_TOOL:-oc}"

function create_ns() {
echo "Create $E2E_NAMESPACE_NAME namespace"
  cat << EOF | $OC_TOOL apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: "$E2E_NAMESPACE_NAME"
EOF
}

create_ns
