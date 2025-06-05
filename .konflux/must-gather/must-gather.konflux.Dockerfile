FROM registry.redhat.io/openshift4/ose-must-gather:4.14@sha256:30a5401ac5c0a87b3656eda22647a70d1c0d765780e907db1567f1d2c6eb3d2c as mgbuilder

COPY . .

# Save original gather script
RUN mv /usr/bin/gather /usr/bin/gather_original

RUN mkdir -p /usr/libexec/must-gather/numaresources-operator && \
    cp /must-gather/collection-scripts/* /usr/libexec/must-gather/numaresources-operator/

FROM registry.redhat.io/rhel9-4-els/rhel-minimal:9.4@sha256:9aadcce1175ddce06e83bb5ddfceb1713d79b1f549330aacf2ff72e3ede77693

ARG OPENSHIFT_VERSION

RUN microdnf install -y procps-ng tar rsync ; microdnf clean all

# Copy must-gather required binaries
COPY --from=mgbuilder /usr/bin/openshift-must-gather /usr/bin/openshift-must-gather
COPY --from=mgbuilder /usr/bin/oc /usr/bin/oc

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