package rte

import (
	_ "embed"
)

//go:embed selinuxinstall.service.template
var SELinuxInstallSystemdServiceTemplate []byte

//go:embed selinuxpolicy.cil
var SELinuxPolicy []byte

//go:embed hookconfigrtenotifier.json.template
var HookConfigRTENotifier []byte

//go:embed rte-notifier.sh
var NotifierScript []byte
