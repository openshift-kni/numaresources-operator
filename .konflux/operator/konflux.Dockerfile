FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_golang_1.23@sha256:d4f94c6c8429565f44a3adc9983367f6ba2add6ef44954591ee881927a91774b as builder

WORKDIR /go/src/github.com/openshift-kni/numaresources-operator
COPY . .

ENV GOEXPERIMENT=strictfipsruntime
ENV CGO_ENABLED=1
ENV GOTAGS="strictfipsruntime"

# Build
RUN make binary-all

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:c7d44146f826037f6873d99da479299b889473492d3c1ab8af86f08af04ec8a0

ARG OPENSHIFT_VERSION

COPY --from=builder /go/src/github.com/openshift-kni/numaresources-operator/bin/manager /bin/numaresources-operator
# bundle the operand, and use a backward compatible name for RTE
COPY --from=builder /go/src/github.com/openshift-kni/numaresources-operator/bin/exporter /bin/resource-topology-exporter
COPY --from=builder /go/src/github.com/openshift-kni/numaresources-operator/bin/buildinfo.json /usr/local/share

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
      cpe="cpe:/a:redhat:openshift:4.19::el9"