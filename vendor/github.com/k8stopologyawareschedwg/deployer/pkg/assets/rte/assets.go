package rte

import (
	_ "embed"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

const (
	OCPVersion411 = "v4.11"
)

//go:embed selinuxinstall.service.template
var SELinuxInstallSystemdServiceTemplate []byte

//go:embed selinuxpolicy-ocp410.cil
var SELinuxPolicyOCP410 []byte

//go:embed selinuxpolicy-ocp411.cil
var SELinuxPolicyOCP411 []byte

//go:embed hookconfigrtenotifier.json.template
var HookConfigRTENotifier []byte

//go:embed rte-notifier.sh
var NotifierScript []byte

func GetSELinuxPolicy(ver platform.Version) ([]byte, error) {
	// error should never happen: we control the input here
	ok, err := ver.AtLeastString(OCPVersion411)
	if err != nil {
		return nil, err
	}
	if ok {
		return SELinuxPolicyOCP411, nil
	}
	return SELinuxPolicyOCP410, nil
}
