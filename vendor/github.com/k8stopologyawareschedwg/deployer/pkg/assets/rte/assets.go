package rte

import (
	_ "embed"
)

//go:embed selinuxinstall.service
var SELinuxInstallSystemdServiceTemplate []byte

//go:embed selinuxpolicy.cil
var SELinuxPolicy []byte
