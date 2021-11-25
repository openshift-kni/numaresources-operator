#!/bin/bash

OC_TOOL="${1:-oc}"
NAMESPACE="${2:-default}"
DAEMONSET="${3:-resource-topology-exporter-ds}"

FAILED=1

function wait-for-daemonset(){
    retries=24
    while [[ $retries -ge 0 ]];do
        sleep 5
        ready=$(${OC_TOOL} -n "$NAMESPACE" get daemonset "$DAEMONSET" -o jsonpath="{.status.numberReady}")
        required=$(${OC_TOOL} -n "$NAMESPACE" get daemonset "$DAEMONSET" -o jsonpath="{.status.desiredNumberScheduled}")
        if [[ $ready -eq $required ]];then
            echo "${NAMESPACE}/${DAEMONSET} ready"
            FAILED=0
            break
        fi
        ((retries--))
        # debug
        echo "${NAMESPACE}/${DAEMONSET} not ready: $ready/$required"
    done
}

echo "waiting for ${NAMESPACE}/${DAEMONSET}"
wait-for-daemonset "${NAMESPACE}" "${DAEMONSET}"
echo "${NAMESPACE}/${DAEMONSET} wait finished"
${OC_TOOL} -n "$NAMESPACE" describe daemonset "$DAEMONSET"
exit ${FAILED}
