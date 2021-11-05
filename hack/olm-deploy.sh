#!/bin/bash

set -e

dockercgf=`oc -n openshift-marketplace get sa builder -oyaml | grep imagePullSecrets -A 1 | grep -o "builder-.*"`

export OC_TOOL="${OC_TOOL:-oc}"
export BRANCH=${BRANCH:-main}

jobdefinition='apiVersion: v1
kind: Pod
metadata:
  name: numaresources-podman
  namespace: openshift-marketplace
spec:
  restartPolicy: Never
  serviceAccountName: builder
  containers:
    - name: priv
      image: quay.io/podman/stable
      command:
        - /bin/bash
        - -c
        - |
          set -xe

          yum install jq git wget -y
          wget https://github.com/operator-framework/operator-registry/releases/download/v1.19.0/linux-amd64-opm
          mv linux-amd64-opm opm
          chmod +x ./opm
          export pass=$( jq .\"image-registry.openshift-image-registry.svc:5000\".password /var/run/secrets/openshift.io/push/.dockercfg )
          podman login -u serviceaccount -p ${pass:1:-1} image-registry.openshift-image-registry.svc:5000 --tls-verify=false

          git clone --single-branch --branch BRANCH https://github.com/openshift-kni/numaresources-operator.git
          cd numaresources-operator

          podman build -f bundle.Dockerfile --tag image-registry.openshift-image-registry.svc:5000/openshift-marketplace/numaresources-operator:latest .
          podman push image-registry.openshift-image-registry.svc:5000/openshift-marketplace/numaresources-operator:latest --tls-verify=false
          cd ..

          ./opm index --skip-tls add --bundles image-registry.openshift-image-registry.svc:5000/openshift-marketplace/numaresources-operator:latest --tag image-registry.openshift-image-registry.svc:5000/openshift-marketplace/nro-ci-index:latest -p podman --mode semver
          podman push image-registry.openshift-image-registry.svc:5000/openshift-marketplace/nro-ci-index:latest --tls-verify=false
      securityContext:
        privileged: true
      volumeMounts:
        - mountPath: /var/run/secrets/openshift.io/push
          name: dockercfg
          readOnly: true
  volumes:
    - name: dockercfg
      defaultMode: 384
      secret:
      '

jobdefinition=$(sed "s#BRANCH#${BRANCH}#" <<< "$jobdefinition")

jobdefinition="${jobdefinition} secretName: ${dockercgf}"
echo "$jobdefinition"
echo "$jobdefinition" | ${OC_TOOL} apply -f -

success=0
iterations=0
sleep_time=10
max_iterations=72 # results in 12 minutes timeout
until [[ $success -eq 1 ]] || [[ $iterations -eq $max_iterations ]]
do
  run_status=$(oc -n openshift-marketplace get pod numaresources-podman -o json | jq '.status.phase' | tr -d '"')
   if [ $run_status == "Succeeded" ]; then
          success=1
          break
   fi
done

if [[ $success -eq 1 ]]; then
  echo "[INFO] numaresources-operator build succeeded"
else
  echo "[ERROR] numaresources-operator build failed"
  exit 1
fi

# print the build logs
${OC_TOOL} -n openshift-marketplace logs numaresources-podman

#Note: adding a CI index image
cat <<EOF | ${OC_TOOL} apply -f -
---
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: nro-ci-index
  namespace: openshift-marketplace
spec:
  displayName: NRO CI Index
  image: image-registry.openshift-image-registry.svc:5000/openshift-marketplace/nro-ci-index:latest
  publisher: Red Hat
  sourceType: grpc
  updateStrategy:
    registryPoll:
      interval: 10m0s
---
EOF


cat <<EOF | ${OC_TOOL} apply -f -
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: numaresources-operator
  namespace: openshift-operators
spec:
  name: numaresources-operator
  channel: alpha
  source: nro-ci-index
  sourceNamespace: openshift-marketplace
---
EOF
