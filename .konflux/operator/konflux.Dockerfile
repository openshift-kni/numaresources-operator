FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_golang_1.22@sha256:9febad53eab6f5b6d1f9a32c7b5f22afdebd6d7735e08f068e0dcbe2ffa6ca64 as builder

WORKDIR /go/src/github.com/openshift-kni/numaresources-operator
COPY . .

ENV GOEXPERIMENT=strictfipsruntime
ENV CGO_ENABLED=1
ENV GOTAGS="strictfipsruntime"

# Build
RUN make binary-all

FROM registry.redhat.io/rhel9-4-els/rhel-minimal:9.4@sha256:9aadcce1175ddce06e83bb5ddfceb1713d79b1f549330aacf2ff72e3ede77693

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