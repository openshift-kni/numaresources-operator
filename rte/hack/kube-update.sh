#!/bin/bash

set -e

# expect oc to be in PATH by default
OC_TOOL="${OC_TOOL:-oc}"

echo "Updating Kubelet with necessary configuration for RTE deployment"
cat << EOF | oc apply -f -
apiVersion: machineconfiguration.openshift.io/v1
kind: KubeletConfig
metadata:
  name: tm-enabled
spec:
  machineConfigPoolSelector:
    matchLabels:
      custom-kubelet: enabled
  kubeletConfig:
    cpuManagerPolicy: "static"
    cpuManagerReconcilePeriod: "5s"
    reservedSystemCPUs: "0,1"
    memoryManagerPolicy: "Static"
    systemReserved: {"memory" :"512Mi"}
    kubeReserved: {"memory" :"512Mi"}
    evictionHard: {"memory.available": "100Mi"}
    reservedMemory: [{"numaNode": 0, "limits": {"memory": "1124Mi"}}]
    topologyManagerPolicy: "single-numa-node"
EOF
