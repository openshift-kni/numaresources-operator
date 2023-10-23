module github.com/openshift-kni/numaresources-operator

go 1.20

require (
	github.com/asaskevich/govalidator v0.0.0-20210307081110-f21760c49a8d
	github.com/drone/envsubst v1.0.3
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v1.2.4
	github.com/google/go-cmp v0.5.9
	github.com/jaypipes/ghw v0.9.0
	github.com/k8stopologyawareschedwg/deployer v0.12.1-0.20230322120411-111a4d4522b1
	github.com/k8stopologyawareschedwg/noderesourcetopology-api v0.1.1
	github.com/k8stopologyawareschedwg/podfingerprint v0.2.2
	github.com/k8stopologyawareschedwg/resource-topology-exporter v0.14.3
	github.com/kubevirt/device-plugin-manager v1.19.4
	github.com/mdomke/git-semver v1.0.0
	github.com/onsi/ginkgo/v2 v2.9.5
	github.com/onsi/gomega v1.27.7
	github.com/openshift/api v0.0.0-20230330150608-05635858d40f
	github.com/openshift/machine-config-operator v0.0.1-0.20230724174830-7b54f1dcce4e
	github.com/pkg/errors v0.9.1
	github.com/sergi/go-diff v1.1.0
	github.com/stretchr/testify v1.8.2
	k8s.io/api v0.27.2
	k8s.io/apiextensions-apiserver v0.27.2
	k8s.io/apimachinery v0.27.2
	k8s.io/client-go v0.27.2
	k8s.io/code-generator v0.26.7
	k8s.io/klog/v2 v2.90.1
	k8s.io/kubectl v0.25.1
	k8s.io/kubelet v0.26.7
	k8s.io/kubernetes v1.26.7
	kubevirt.io/qe-tools v0.1.8
	sigs.k8s.io/controller-runtime v0.15.0
	sigs.k8s.io/scheduler-plugins v0.24.9
	sigs.k8s.io/yaml v1.3.0
)

require (
	github.com/OneOfOne/xxhash v1.2.9-0.20201014161131-8506fca4db5e // indirect
	github.com/StackExchange/wmi v1.2.1 // indirect
	github.com/andres-erbsen/clock v0.0.0-20160526145045-9e14626cd129 // indirect
	github.com/aquasecurity/go-version v0.0.0-20210121072130-637058cfe492 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/coreos/ignition/v2 v2.15.0 // indirect
	github.com/coreos/vcontext v0.0.0-20230201181013-d72178a18687 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.9.0 // indirect
	github.com/evanphx/json-patch v4.12.0+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.6.0 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-logr/zapr v1.2.4 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.1 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/glog v1.0.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/gnostic v0.5.7-v3refs // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/imdario/mergo v0.3.13 // indirect
	github.com/inconshreveable/mousetrap v1.0.1 // indirect
	github.com/jaypipes/pcidb v1.0.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/moby/spdystream v0.2.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/openshift/client-go v0.0.0-20221019143426-16aed247da5c // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.15.1 // indirect
	github.com/prometheus/client_model v0.4.0 // indirect
	github.com/prometheus/common v0.42.0 // indirect
	github.com/prometheus/procfs v0.9.0 // indirect
	github.com/spf13/cobra v1.6.0 // indirect
	github.com/spf13/pflag v1.0.6-0.20210604193023-d5e0c0615ace // indirect
	github.com/stretchr/objx v0.5.0 // indirect
	github.com/vincent-petithory/dataurl v1.0.0 // indirect
	go.uber.org/ratelimit v0.2.0 // indirect
	golang.org/x/mod v0.10.0 // indirect
	golang.org/x/net v0.10.0 // indirect
	golang.org/x/oauth2 v0.5.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
	golang.org/x/term v0.13.0 // indirect
	golang.org/x/text v0.13.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	golang.org/x/tools v0.9.1 // indirect
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2 // indirect
	gomodules.xyz/jsonpatch/v2 v2.3.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20230209215440-0dfe4f8abfcc // indirect
	google.golang.org/grpc v1.53.0 // indirect
	google.golang.org/protobuf v1.30.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	howett.net/plist v1.0.0 // indirect
	k8s.io/apiserver v0.26.7 // indirect
	k8s.io/component-base v0.27.2 // indirect
	k8s.io/gengo v0.0.0-20220902162205-c0856e24416d // indirect
	k8s.io/kube-openapi v0.0.0-20230501164219-8b0f38b5fd1f // indirect
	k8s.io/kube-scheduler v0.26.7 // indirect
	k8s.io/utils v0.0.0-20230209194617-a36077c30491 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
)

replace (
	github.com/gogo/protobuf => github.com/gogo/protobuf v1.3.2
	golang.org/x/text => golang.org/x/text v0.3.8
	k8s.io/api => k8s.io/api v0.26.7
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.26.7
	k8s.io/apimachinery => k8s.io/apimachinery v0.26.7
	k8s.io/apiserver => k8s.io/apiserver v0.26.7
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.26.7
	k8s.io/client-go => k8s.io/client-go v0.26.7
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.26.7
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.26.7
	k8s.io/code-generator => k8s.io/code-generator v0.26.7
	k8s.io/component-base => k8s.io/component-base v0.26.7
	k8s.io/component-helpers => k8s.io/component-helpers v0.26.7
	k8s.io/controller-manager => k8s.io/controller-manager v0.26.7
	k8s.io/cri-api => k8s.io/cri-api v0.26.7
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.26.7
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.26.7
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.26.7
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.26.7
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.26.7
	k8s.io/kubectl => k8s.io/kubectl v0.26.7
	k8s.io/kubelet => k8s.io/kubelet v0.26.7
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.26.7
	k8s.io/metrics => k8s.io/metrics v0.26.7
	k8s.io/mount-utils => k8s.io/mount-utils v0.26.7
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.26.7
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.26.7
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.14.6
)

// local pinning
replace (
	github.com/containerd/containerd => github.com/containerd/containerd v1.4.11
	github.com/onsi/ginkgo/v2 => github.com/onsi/ginkgo/v2 v2.4.0
	github.com/onsi/gomega => github.com/onsi/gomega v1.23.0
	github.com/openshift/machine-config-operator => github.com/openshift/machine-config-operator v0.0.1-0.20230724174830-7b54f1dcce4e // release-4.13
	golang.org/x/net => golang.org/x/net v0.17.0
	golang.org/x/sys => golang.org/x/sys v0.13.0
)
