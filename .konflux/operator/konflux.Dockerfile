FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_8_golang_1.19@sha256:b2583405e58b375939233f5de21b5baecedcfa476d501d88be1baabce059e6d7 as builder

WORKDIR /go/src/github.com/openshift-kni/numaresources-operator
COPY . .

# Build
RUN make binary-all

FROM registry.redhat.io/ubi8/ubi-minimal:latest@sha256:46f08923b566497395dda7d4d07bad383b4eedd5a382c3e22e715b169f414331

ARG OPENSHIFT_VERSION

COPY --from=builder /go/src/github.com/openshift-kni/numaresources-operator/bin/manager /bin/numaresources-operator
# bundle the operand, and use a backward compatible name for RTE
COPY --from=builder /go/src/github.com/openshift-kni/numaresources-operator/bin/exporter /bin/resource-topology-exporter

RUN mkdir /etc/resource-topology-exporter/ && \
    touch /etc/resource-topology-exporter/config.yaml
RUN microdnf install -y hwdata && \
    microdnf clean -y all
USER 65532:65532
ENTRYPOINT ["/bin/numaresources-operator"]
LABEL com.redhat.component="numaresources-operator-container" \
      name="openshift4/numaresources-rhel8-operator" \
      summary="numaresources-operator" \
      io.openshift.expose-services="" \
      io.openshift.tags="operator" \
      io.k8s.display-name="numaresources-operator" \
      io.k8s.description="Numa Resources Operator" \
      maintainer="openshift-operators@redhat.com" \
      description="numaresources-operator" \
      io.openshift.maintainer.component="NUMA Resources Operator" \
      io.openshift.maintainer.product="OpenShift Container Platform" \
      distribution-scope="public" \
      release="${OPENSHIFT_VERSION}" \
      url="https://github.com/openshift-kni/numaresources-operator" \
      vendor="Red Hat, Inc." \
      cpe="cpe:/a:redhat:openshift:4.12::el8"