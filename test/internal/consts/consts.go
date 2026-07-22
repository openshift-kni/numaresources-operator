package consts

const (
	// This is the name and the package name of the subscription spec in the operator subscription CRD.
	// in operator subscription it would be found under `spec.name` or `spec.package`
	SubscriptionSpecNamePackage = "numaresources-operator"

	OperatorDeploymentName   = "numaresources-controller-manager"
	OperatorManagerContainer = "manager"
)

var (
	OperatorDeploymentLabels = map[string]string{
		"control-plane": "controller-manager",
		"app":           "numaresources-operator",
	}
)
