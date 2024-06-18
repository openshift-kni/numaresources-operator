module github.com/openshift-kni/numaresources-operator

go 1.22.0

toolchain go1.22.8

require (
	github.com/aquasecurity/go-version v0.0.0-20210121072130-637058cfe492
	github.com/drone/envsubst v1.0.3
	github.com/go-logr/logr v1.4.2
	github.com/google/go-cmp v0.6.0
	github.com/jaypipes/ghw v0.13.0
	github.com/k8stopologyawareschedwg/deployer v0.21.0
	github.com/k8stopologyawareschedwg/noderesourcetopology-api v0.1.2
	github.com/k8stopologyawareschedwg/podfingerprint v0.2.2
	github.com/k8stopologyawareschedwg/resource-topology-exporter v0.20.1-0.20241202101038-c712cceeffb0
	github.com/kubevirt/device-plugin-manager v1.19.4
	github.com/mdomke/git-semver v1.0.0
	github.com/onsi/ginkgo/v2 v2.19.1
	github.com/onsi/gomega v1.34.0
	github.com/openshift/api v0.0.0-20240524162738-d899f8877d22
	github.com/openshift/cluster-node-tuning-operator v0.0.0-20240611064827-2bd8891ead93
	github.com/openshift/hypershift/api v0.0.0-20241115183703-d41904871380
	github.com/openshift/machine-config-operator v0.0.1-0.20230724174830-7b54f1dcce4e
	github.com/sergi/go-diff v1.1.0
	github.com/stretchr/testify v1.9.0
	golang.org/x/sync v0.8.0
	k8s.io/api v0.31.2
	k8s.io/apiextensions-apiserver v0.31.2
	k8s.io/apimachinery v0.31.2
	k8s.io/client-go v0.31.2
	k8s.io/code-generator v0.31.2
	k8s.io/klog/v2 v2.130.1
	k8s.io/kubectl v0.31.2
	k8s.io/kubelet v0.31.2
	k8s.io/utils v0.0.0-20240711033017-18e509b52bc8
	sigs.k8s.io/controller-runtime v0.19.1
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/OneOfOne/xxhash v1.2.9-0.20201014161131-8506fca4db5e // indirect
	github.com/StackExchange/wmi v1.2.1 // indirect
	github.com/andres-erbsen/clock v0.0.0-20160526145045-9e14626cd129 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/coreos/ignition/v2 v2.18.0 // indirect
	github.com/coreos/vcontext v0.0.0-20231102161604-685dc7299dc5 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.12.0 // indirect
	github.com/evanphx/json-patch/v5 v5.9.0 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/glog v1.2.1 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20240525223248-4bfdf5a9a2af // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.1 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jaypipes/pcidb v1.0.1 // indirect
	github.com/jeremywohl/flatten/v2 v2.0.0-20211013061545-07e4a09fb8e4 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/moby/spdystream v0.4.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/openshift/client-go v0.0.0-20240415214935-be70f772f157 // indirect
	github.com/openshift/custom-resource-status v1.1.3-0.20220503160415-f2fdb4999d87 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.19.1 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/spf13/cobra v1.8.1 // indirect
	github.com/spf13/pflag v1.0.6-0.20210604193023-d5e0c0615ace // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/vincent-petithory/dataurl v1.0.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.uber.org/ratelimit v0.2.0 // indirect
	golang.org/x/exp v0.0.0-20231006140011-7918f672742d // indirect
	golang.org/x/mod v0.17.0 // indirect
	golang.org/x/net v0.30.0 // indirect
	golang.org/x/oauth2 v0.21.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
	golang.org/x/term v0.25.0 // indirect
	golang.org/x/text v0.19.0 // indirect
	golang.org/x/time v0.5.0 // indirect
	golang.org/x/tools v0.21.1-0.20240508182429-e35e4ccd0d2d // indirect
	golang.org/x/xerrors v0.0.0-20231012003039-104605ab7028 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240701130421-f6361c86f094 // indirect
	google.golang.org/grpc v1.65.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	howett.net/plist v1.0.1 // indirect
	k8s.io/component-base v0.31.2 // indirect
	k8s.io/gengo/v2 v2.0.0-20240228010128-51d4e06bde70 // indirect
	k8s.io/klog v1.0.0 // indirect
	k8s.io/kube-openapi v0.0.0-20240411171206-dc4e619f62f3 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
)

replace (
	k8s.io/api => k8s.io/api v0.31.2
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.31.2
	k8s.io/apimachinery => k8s.io/apimachinery v0.31.2
	k8s.io/apiserver => k8s.io/apiserver v0.31.2
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.31.2
	k8s.io/client-go => k8s.io/client-go v0.31.2
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.31.2
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.31.2
	k8s.io/code-generator => k8s.io/code-generator v0.31.2
	k8s.io/component-base => k8s.io/component-base v0.31.2
	k8s.io/component-helpers => k8s.io/component-helpers v0.31.2
	k8s.io/controller-manager => k8s.io/controller-manager v0.31.2
	k8s.io/cri-api => k8s.io/cri-api v0.31.2
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.31.2
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.31.2
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.31.2
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.31.2
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.31.2
	k8s.io/kubectl => k8s.io/kubectl v0.31.2
	k8s.io/kubelet => k8s.io/kubelet v0.31.2
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.31.2
	k8s.io/metrics => k8s.io/metrics v0.31.2
	k8s.io/mount-utils => k8s.io/mount-utils v0.31.2
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.31.2
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.31.2
)
