package selinux

import (
	"embed"
	"path/filepath"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

const (
	// OCPVersion4.11 is DEPRECATED and will be removed in the next versions
	OCPVersion411 = "v4.11"
)

const (
	policyDir = "policy"

	ocpVersion410 = "v4.10"
	// TODO: demote public constant here once we can remove from the public API
	ocpVersion412 = "v4.12"
	ocpVersion413 = "v4.13"
)

//go:embed selinuxinstall.service.template
var InstallSystemdServiceTemplate []byte

//go:embed policy
var policy embed.FS

func GetPolicy(ver platform.Version) ([]byte, error) {
	// keep it ordered from most recent supported to the oldest supported
	for _, cand := range []string{ocpVersion413, ocpVersion412, OCPVersion411, ocpVersion410} {
		// error should never happen: we control the input here
		ok, err := ver.AtLeastString(cand)
		if err != nil {
			return nil, err
		}
		if ok {
			return policy.ReadFile(policyPathFromVer(cand))
		}
	}
	// just in case we end up here first supported version is 4.10, hence this is a safe fallback
	return policy.ReadFile(policyPathFromVer(ocpVersion410))
}

func policyPathFromVer(ver string) string {
	return filepath.Join(policyDir, "ocp_"+ver+".cil")
}
