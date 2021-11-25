#!/usr/bin/env bash

notifier_file="${1}"
bundle=$(jq -r '.bundle' /dev/stdin 2>&1)
pod_cgroup=$(jq .linux.cgroupsPath < ${bundle}/config.json)

# Do not touch file for non guaranteed pods
if [[ ${pod_cgroup} =~ ".*burstable.*" ]] || [[ ${pod_cgroup} =~ ".*besteffort.*" ]]; then
    exit 0
fi

touch "${notifier_file}" || :
