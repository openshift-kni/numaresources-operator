package consts

const (
	// NodePoolNameLabel uses to label ConfigMap objects which are associated with the NodePool
	NodePoolNameLabel = "hypershift.openshift.io/nodePool"

	// KubeletConfigConfigMapLabel uses
	// to label a ConfigMap that holds a KubeletConfig object
	KubeletConfigConfigMapLabel = "hypershift.openshift.io/kubeletconfig-config"

	// ConfigKey is the key under ConfigMap.Data on which encoded
	// machine-config, kubelet-config objects are stored.
	ConfigKey = "config"
)
