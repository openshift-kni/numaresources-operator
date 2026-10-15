#!/usr/bin/env bash

JQ="/usr/bin/jq"
NOTIFIER_FILE="${1}"

bundle=$( ${JQ} -r '.bundle' /dev/stdin 2>&1 )
pod_cgroup=$( ${JQ} .linux.cgroupsPath < ${bundle}/config.json )

# Do not touch file for non guaranteed pods
if [[ ${pod_cgroup} =~ ".*burstable.*" ]] || [[ ${pod_cgroup} =~ ".*besteffort.*" ]]; then
    exit 0
fi

touch "${NOTIFIER_FILE}" || :
