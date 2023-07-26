#!/bin/bash

DIRNAME="$(dirname "$(readlink -f "$0")")"

oc delete -f ${DIRNAME}/manifests/sample-devices/namespace.yaml

