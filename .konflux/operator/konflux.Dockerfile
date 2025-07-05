FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_golang_1.21@sha256:2b45f967c73c98155118386a33d9d19e5f8bbbdf46cdd9dffb01c3ff98cc4dbe as builder

WORKDIR /go/src/github.com/openshift-kni/numaresources-operator
COPY . .

ENV GOEXPERIMENT=strictfipsruntime
ENV CGO_ENABLED=1
ENV GOTAGS="strictfipsruntime"

# Build
RUN make binary-all

FROM registry.redhat.io/rhel9-4-els/rhel-minimal:9.4@sha256:9bdee4d2b5b05125ddbf44bece40131585a9be08004a30c3b74b5181de7a3634

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
      name="openshift4/numaresources-operator-rhel9" \
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
      vendor="Red Hat, Inc."