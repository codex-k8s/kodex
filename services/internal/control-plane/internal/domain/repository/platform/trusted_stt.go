package platform

const (
	// TrustedSTTAuthorityOperation identifies resolution of organization-scoped speech-transcription authority for the registered STT workload; it is not a model or credential identifier.
	// TrustedSTTAuthorityOperation обозначает разрешение организационных полномочий транскрибации для зарегистрированного STT-workload; идентификатором модели или реквизита она не является.
	TrustedSTTAuthorityOperation = "platform.stt.authority.resolve"
	// TrustedSTTCatalogAuthorityOperation identifies the STT catalog-authority route mapped to organization.manage rather than ordinary platform.stt.use.
	// TrustedSTTCatalogAuthorityOperation обозначает маршрут полномочий каталога STT, сопоставляемый с organization.manage, а не с обычным platform.stt.use.
	TrustedSTTCatalogAuthorityOperation = "platform.stt.catalog-authority.resolve"
	// TrustedSTTPolicyOperation identifies STT policy resolution for the registered STT workload under platform.stt.use, separately from catalog administration.
	// TrustedSTTPolicyOperation обозначает разрешение политики STT для зарегистрированного STT-workload с platform.stt.use отдельно от администрирования каталога.
	TrustedSTTPolicyOperation = "platform.stt.policy.resolve"
	// TrustedSTTCredentialOperation identifies the credential-projection route allowed only for secret-broker by this mapping, returning the STT-use permission rather than exposing credentials itself.
	// TrustedSTTCredentialOperation обозначает маршрут проекции реквизита, разрешённый этим сопоставлением только secret-broker; возвращается полномочие использования STT, а не сам реквизит.
	TrustedSTTCredentialOperation = "platform.credential-projections.stt.resolve"
)

// TrustedSTTAuthorityPermission maps only the exact supported workload/operation pairs to organization-scoped permissions: secret-broker credential projection, STT authority/policy use and STT catalog management. All other pairs return false; callers must separately verify the explicit transport profile and actual principal authority.
// TrustedSTTAuthorityPermission сопоставляет только точные поддерживаемые пары workload и операции с организационными полномочиями: проекцию реквизита secret-broker, использование полномочий и политики STT и управление каталогом STT. Остальные пары дают false; вызывающий отдельно проверяет явный профиль транспорта и реальные полномочия субъекта.
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
