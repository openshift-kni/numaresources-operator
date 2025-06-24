#!/usr/bin/env bash

source hack/common.sh

# Specify the URL link to the machine config pool CRD
CRD_MACHINE_CONFIG_POOL_URL="https://raw.githubusercontent.com/openshift/api/refs/heads/master/payload-manifests/crds/0000_80_machine-config_01_machineconfigpools.crd.yaml"
# Specify the URL link to the kubeletconfig CRD
CRD_KUBELET_CONFIG_URL="https://raw.githubusercontent.com/openshift/api/refs/heads/master/payload-manifests/crds/0000_80_machine-config_01_kubeletconfigs.crd.yaml"

[ ! -x ${REPO_DIR}/bin/lsplatform ] && exit 1

if ${REPO_DIR}/bin/lsplatform -is-platform openshift; then
	echo "detected openshift platform - nothing to do"
	exit 0
fi

if ${REPO_DIR}/bin/lsplatform -is-platform HyperShift; then
	echo "detected HyperShift platform - nothing to do"
	exit 0
fi

runcmd kubectl apply -f ${CRD_MACHINE_CONFIG_POOL_URL}
runcmd kubectl apply -f ${CRD_KUBELET_CONFIG_URL}
