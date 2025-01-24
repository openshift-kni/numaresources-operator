FROM docker.io/golang:1.23 AS builder

WORKDIR /go/src/github.com/openshift-kni/numaresources-operator
COPY . .

RUN make build-numacell

FROM registry.access.redhat.com/ubi9/ubi-minimal
COPY --from=builder /go/src/github.com/openshift-kni/numaresources-operator/bin/numacell /bin/numacell
ENTRYPOINT ["/bin/numacell", "-alsologtostderr", "-v", "3"]
