package selinux

import (
	"embed"
	"path/filepath"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

const (
	policyDir = "policy"

	ocpVersion410 = "v4.10"
	ocpVersion411 = "v4.11"
	ocpVersion412 = "v4.12"
	ocpVersion413 = "v4.13"
	ocpVersion414 = "v4.14"
	ocpVersion415 = "v4.15"
)

//go:embed selinuxinstall.service.template
var InstallSystemdServiceTemplate []byte

//go:embed policy
var policy embed.FS

func GetPolicy(ver platform.Version) ([]byte, error) {
	// keep it ordered from most recent supported to the oldest supported
	allVersions := knownVersions()
	for _, cand := range allVersions {
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

func knownVersions() []string {
	return []string{
		ocpVersion415,
		ocpVersion414,
		ocpVersion413,
		ocpVersion412,
		ocpVersion411,
		ocpVersion410,
	}
}

func policyPathFromVer(ver string) string {
	return filepath.Join(policyDir, "ocp_"+ver+".cil")
}
