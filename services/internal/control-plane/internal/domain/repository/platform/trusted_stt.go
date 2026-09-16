package platform

const (
	TrustedSTTAuthorityOperation        = "platform.stt.authority.resolve"
	TrustedSTTCatalogAuthorityOperation = "platform.stt.catalog-authority.resolve"
	TrustedSTTPolicyOperation           = "platform.stt.policy.resolve"
	TrustedSTTCredentialOperation       = "platform.credential-projections.stt.resolve"
)

// TrustedSTTAuthorityPermission — закрытое org-only делегирование сессии.
// Вызывающий слой обязан отдельно проверить explicit transport profile.
func TrustedSTTAuthorityPermission(workload, operation string) (string, bool) {
	if workload == "secret-broker" && operation == TrustedSTTCredentialOperation {
		return "platform.stt.use", true
	}
	if workload != "stt-tts-service" {
		return "", false
	}
	switch operation {
	case TrustedSTTAuthorityOperation, TrustedSTTPolicyOperation:
		return "platform.stt.use", true
	case TrustedSTTCatalogAuthorityOperation:
		return "organization.manage", true
	default:
		return "", false
	}
}
