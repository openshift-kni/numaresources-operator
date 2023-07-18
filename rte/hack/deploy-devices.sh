#!/bin/bash

set -ex

DIRNAME="$(dirname "$(readlink -f "$0")")"

oc create -f ${DIRNAME}/manifests/sample-devices/namespace.yaml
oc create -f ${DIRNAME}/manifests/sample-devices/serviceaccount.yaml
oc create -f ${DIRNAME}/manifests/sample-devices/role.yaml
oc create -f ${DIRNAME}/manifests/sample-devices/rolebinding.yaml
oc create -f ${DIRNAME}/manifests/sample-devices/configmap-dpa.yaml
oc create -f ${DIRNAME}/manifests/sample-devices/daemonset-dpa.yaml
