#!/bin/sh

DIRNAME="$(dirname "$(readlink -f "$0")")"
REPOOWNER=${REPOOWNER:-openshift-kni}
IMAGENAME=${IMAGENAME:-resource-topology-exporter}
IMAGETAG=${IMAGETAG:-4.9-snapshot}
export RTE_CONTAINER_IMAGE=${RTE_CONTAINER_IMAGE:-quay.io/${REPOOWNER}/${IMAGENAME}:${IMAGETAG}}
export RTE_POLL_INTERVAL="${RTE_POLL_INTERVAL:-10s}"
export METRICS_PORT="${METRICS_PORT:-2112}"
envsubst < "${DIRNAME}"/../manifests/openshift-resource-topology-exporter-ds.yaml
