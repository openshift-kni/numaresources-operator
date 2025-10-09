# follow https://brewweb.engineering.redhat.com/brew/packageinfo?packageID=70135
FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_golang_1.24@sha256:b91431604c435f3cabec20ddb653c0537c8ba8097ada57960d54a1266f95a7c3 as tool-builder

WORKDIR /go/src/github.com/openshift-kni/numaresources-operator
COPY . .

RUN make bin/pfpsyncchk

FROM registry.redhat.io/openshift4/ose-must-gather-rhel9:latest as mgbuilder

COPY . .

# Save original gather script
RUN mv /usr/bin/gather /usr/bin/gather_original

RUN mkdir -p /usr/libexec/must-gather/numaresources-operator && \
    cp /must-gather/collection-scripts/* /usr/libexec/must-gather/numaresources-operator/

FROM registry.redhat.io/rhel9-4-els/rhel-minimal:9.4@sha256:44aca991940c02b026c46dd07d485a6a63356b3e809459c9cecd92f997ca4ca8

ARG OPENSHIFT_VERSION

RUN microdnf install -y procps-ng tar rsync ; microdnf clean all

# Copy must-gather required binaries
COPY --from=mgbuilder /usr/bin/openshift-must-gather /usr/bin/openshift-must-gather
COPY --from=mgbuilder /usr/bin/oc /usr/bin/oc
COPY --from=tool-builder /go/src/github.com/openshift-kni/numaresources-operator/bin/pfpsyncchk /usr/bin/pfpsyncchk

COPY --from=mgbuilder /usr/libexec/must-gather/numaresources-operator/* /usr/bin/

ENTRYPOINT ["/usr/bin/gather"]

LABEL com.redhat.component="numaresources-must-gather-container" \
    name="openshift4/numaresources-must-gather-rhel9" \
    summary="numa resources data gathering image" \
    io.openshift.expose-services="" \
    io.openshift.tags="data,images" \
    io.k8s.display-name="numaresources-must-gather" \
    io.k8s.description="numa resources data gathering image." \ 
    description="numa resources data gathering image." \
    maintainer="openshift-operators@redhat.com" \
    io.openshift.maintainer.component="NUMA Resources Operator" \
    io.openshift.maintainer.product="OpenShift Container Platform" \
    distribution-scope="public" \
    release="${OPENSHIFT_VERSION}" \
    url="https://github.com/openshift-kni/numaresources-operator" \
    vendor="Red Hat, Inc."
