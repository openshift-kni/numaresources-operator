#!/bin/bash

# Get the list of all worker nodes
nodes=$(oc get nodes -l node-role.kubernetes.io/worker= -o jsonpath='{.items[*].metadata.name}')

# Loop through each node and create a Job
for node in $nodes; do
        echo "execute job on node: ${node}"
        cat <<EOF | oc create -f -
apiVersion: batch/v1
kind: Job
metadata:
  namespace: openshift-infra
  generateName: selinux-restorecon-
spec:
  template:
    spec:
      nodeName: $node
      containers:
        - name: selinux-context-restoration-cnt
          image: registry.redhat.io/ubi9/ubi
          command: ["/usr/sbin/chroot", "/host", "/usr/sbin/restorecon", "-R", "/var/lib/kubelet/pod-resources"]
          securityContext:
            privileged: true
            runAsUser: 0
          volumeMounts:
            - mountPath: /host
              name: host
      restartPolicy: Never
      volumes:
        - hostPath:
            path: /
            type: Directory
          name: host
      securityContext:
        runAsUser: 0
      serviceAccountName: build-controller
EOF
done