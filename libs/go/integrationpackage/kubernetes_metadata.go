package integrationpackage

// KubernetesMetadataRead identifies the closed addressless read consumers that use the existing owner child/effect lifecycle without fabricated resource observations.
// KubernetesMetadataRead определяет закрытые безадресные reads через существующий owner child/effect lifecycle без выдуманных наблюдений ресурса.
func KubernetesMetadataRead(operation string) bool {
	registered, ok := KubernetesOperationByKey(operation)
	if !ok || registered.Risk != RiskRead || registered.Approval != ApprovalNone {
		return false
	}
	switch operation {
	case "kubernetes.version", "kubernetes.identity", "kubernetes.config.metadata", "kubernetes.help":
		return true
	default:
		return false
	}
}
