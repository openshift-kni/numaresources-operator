#!/bin/bash

set -xe

E2E_NAMESPACE_NAME=$1

# expect oc to be in PATH by default
OC_TOOL="${OC_TOOL:-oc}"

function delete_ns() {
  echo "Delete $E2E_NAMESPACE_NAME namespace"
  $OC_TOOL delete ns "$E2E_NAMESPACE_NAME"
}

delete_ns
