#!/bin/bash
set -xeu

DIR=$(dirname "${BASH_SOURCE[0]}")

TOPOLOGYAPI_MANIFESTS="https://raw.githubusercontent.com/k8stopologyawareschedwg/noderesourcetopology-api/master/manifests"
curl -L ${TOPOLOGYAPI_MANIFESTS}/crd.yaml			-o $DIR/api/crd.yaml

SCHEDULER_PLUGIN_MANIFESTS="https://raw.githubusercontent.com/kubernetes-sigs/scheduler-plugins/master/manifests/noderesourcetopology"
curl -L ${SCHEDULER_PLUGIN_MANIFESTS}/ns.yaml 			-o $DIR/sched/namespace.yaml
curl -L ${SCHEDULER_PLUGIN_MANIFESTS}/scheduler-configmap.yaml	-o $DIR/sched/configmap.yaml
curl -L ${SCHEDULER_PLUGIN_MANIFESTS}/cluster-role.yaml		-o $DIR/sched/clusterrole-allinone.yaml
curl -L ${SCHEDULER_PLUGIN_MANIFESTS}/deploy.yaml		-o $DIR/sched/deployment.yaml

