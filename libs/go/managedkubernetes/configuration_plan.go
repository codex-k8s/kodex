package managedkubernetes

const (
	// ConfigurationPlanUnsupportedReason distinguishes the unimplemented assistant configuration
	// workflow from the supported direct manager namespace-grant API and Kubernetes execution.
	// ConfigurationPlanUnsupportedReason отличает нереализованный assistant configuration
	// workflow от поддерживаемых direct manager namespace-grant API и Kubernetes execution.
	ConfigurationPlanUnsupportedReason = "KUBERNETES_CONFIGURATION_PLAN_UNSUPPORTED"
	// ConfigurationPlanUnsupportedMessage is fixed and carries no package, owner or grant input.
	// ConfigurationPlanUnsupportedMessage фиксирован и не содержит package, owner либо grant input.
	ConfigurationPlanUnsupportedMessage = "managed Kubernetes configuration plans are unsupported; use direct namespace grant settings"
)
