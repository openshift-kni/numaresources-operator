# Runtime image is used to overlay clusterserviceversion.yaml for Konflux

# yq is required for merging the yaml files
# Run the overlay in a container
FROM quay.io/konflux-ci/yq:latest@sha256:15d0238843d954ee78c9c190705eb8b36f6e52c31434183c37d99a80841a635a AS overlay

# Set work dir
WORKDIR /tmp

# Copy bundle manifests
ENV MANIFESTS_PATH=/tmp/manifests
COPY --chown=yq:yq bundle/manifests $MANIFESTS_PATH

# Copy overlay scripts
ENV OVERLAY_PATH=/tmp/overlay
RUN mkdir -p $OVERLAY_PATH
COPY .konflux/bundle/overlay/ $OVERLAY_PATH

# Run the overlay
RUN $OVERLAY_PATH/overlay.bash \
    --set-pinning-file $OVERLAY_PATH/pin_images.in.yaml \
    --set-mapping-file $OVERLAY_PATH/map_images.in.yaml \
    --set-mapping-production \
    --set-csv-file $MANIFESTS_PATH/numaresources-operator.clusterserviceversion.yaml

# From here downwards this should mostly match the non-konflux bundle, i.e., `bundle.Dockerfile`
# However there are a few exceptions:
# 1. The label 'operators.operatorframework.io.bundle.channels.v1'
# 2. The label 'operators.operatorframework.io.bundle.channels.default.v1'
# 3. The copy of the manifests (copy from the overlay instead of from the git repo)

FROM scratch

ARG OPENSHIFT_VERSION

LABEL com.redhat.component="numaresources-operator-bundle-container"
LABEL name="openshift4/numaresources-operator-bundle-container-rhel8"
LABEL summary="NUMA resources operator for OpenShift"
LABEL io.k8s.display-name="numaresources-operator"
LABEL io.k8s.description="NUMA resurces support for OpenShift"
LABEL description="NUMA resurces support for OpenShift"
LABEL maintainer="openshift-operators@redhat.com"
LABEL license="ASL 2.0"
LABEL distribution-scope="public"
LABEL release="${OPENSHIFT_VERSION}"
LABEL url="https://github.com/openshift-kni/numaresources-operator"
LABEL vendor="Red Hat, Inc."

LABEL io.openshift.expose-services=""
LABEL io.openshift.tags="numa,topology,node"
LABEL io.openshift.maintainer.component="NUMA Resources Operator"
LABEL io.openshift.maintainer.product="OpenShift Container Platform"

LABEL operators.operatorframework.io.bundle.mediatype.v1=registry+v1
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
LABEL operators.operatorframework.io.bundle.package.v1=numaresources-operator
#TODO - consider adding stable channel (e.g. stable,4.13)
LABEL operators.operatorframework.io.bundle.channels.v1="${OPENSHIFT_VERSION}"
#TODO - consider default to stable
LABEL operators.operatorframework.io.bundle.channel.default.v1="${OPENSHIFT_VERSION}"

# Labels for testing.
LABEL operators.operatorframework.io.test.mediatype.v1=scorecard+v1
LABEL operators.operatorframework.io.test.config.v1=tests/scorecard/

# These are three labels needed to control how the pipeline should handle this container image

# This first label tells the pipeline that this is a bundle image and should be
# delivered via an index image
LABEL com.redhat.delivery.operator.bundle=true

# This second label tells the pipeline which versions of OpenShift the operator supports.
# This is used to control which index images should include this operator.
LABEL com.redhat.openshift.versions="=v${OPENSHIFT_VERSION}"

# This third label tells the pipeline that this operator should *also* be supported on OCP 4.4 and
# earlier.  It is used to control whether or not the pipeline should attempt to automatically
# backport this content into the old appregistry format and upload it to the quay.io application
# registry endpoints.
# NROP is first shipped with OCP 4.10
LABEL com.redhat.delivery.backport=false

# Copy files to locations specified by labels.
COPY --from=overlay /tmp/manifests /manifests/
COPY bundle/metadata /metadata/
COPY bundle/tests/scorecard /tests/scorecard/

# Replace additional bundle files
COPY .konflux/bundle/metadata/annotations.yaml /metadata/
COPY .konflux/bundle/tests/scorecard/config.yaml /tests/scorecard/
