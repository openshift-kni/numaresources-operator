package rte

import (
	_ "embed"
)

//go:embed hookconfigrtenotifier.json.template
var HookConfigRTENotifier []byte

//go:embed rte-notifier.sh
var NotifierScript []byte
