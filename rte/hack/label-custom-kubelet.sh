#!/bin/bash

set -e

# expect oc to be in PATH by default
OC_TOOL="${OC_TOOL:-oc}"

# Label worker MCP
echo "[INFO]: Labeling worker MCP with custom-kubelet"
mcp=$(${OC_TOOL} get mcp --selector='pools.operator.machineconfiguration.openshift.io/worker=' -o name)

${OC_TOOL} label --overwrite "$mcp" custom-kubelet=enabled
