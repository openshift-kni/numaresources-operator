FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_golang_1.22@sha256:05701005c47c99ade6d022b99a84648e01551e01816f8b399865e6d2d5fed197 as builder

WORKDIR /go/src/github.com/openshift-kni/numaresources-operator
COPY . .

ENV GOEXPERIMENT=strictfipsruntime
ENV CGO_ENABLED=1
ENV GOTAGS="strictfipsruntime"

# Build
RUN make binary-all

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:759f5f42d9d6ce2a705e290b7fc549e2d2cd39312c4fa345f93c02e4abb8da95

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
      name="openshift4/numaresources-rhel9-operator" \
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
      cpe="cpe:/a:redhat:openshift:4.16::el9"