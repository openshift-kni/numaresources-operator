#!/bin/sh
if /usr/sbin/semodule -l | grep -q -w "rte"; then
    echo "RTE policy found. Attempting removal..."
    if /usr/sbin/semodule -r rte; then
        echo "Success: RTE policy removed."
    else
        echo "Error: Failed to remove RTE policy."
        exit 1
    fi
else
    echo "RTE policy is not installed. Nothing to do."
fi
