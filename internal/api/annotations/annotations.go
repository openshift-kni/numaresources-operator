package annotations

const (
	SELinuxPolicyConfigAnnotation = "config.node.openshift-kni.io/selinux-policy"
	SELinuxPolicyCustom           = "custom"
)

func IsCustomPolicyEnabled(annot map[string]string) bool {
	if v, ok := annot[SELinuxPolicyConfigAnnotation]; ok && v == SELinuxPolicyCustom {
		return true
	}
	return false
}
