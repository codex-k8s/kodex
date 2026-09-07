package providercredential

import "errors"

// Этапы принадлежат observer, а не входному payload или тексту ошибки провайдера.
type catalogStage string

const (
	catalogStageUnknown           catalogStage = "unknown"
	catalogStageCredential        catalogStage = "credential_read"
	catalogStageAuthentication    catalogStage = "authentication"
	catalogStageRuntime           catalogStage = "runtime_check"
	catalogStageAPI               catalogStage = "api_catalog"
	catalogStageProcess           catalogStage = "process_start"
	catalogStageInitialize        catalogStage = "initialize"
	catalogStageLogin             catalogStage = "login_response"
	catalogStageListCall          catalogStage = "model_list_call"
	catalogStageListSchema        catalogStage = "model_list_schema"
	catalogStageListIdentity      catalogStage = "model_list_identity"
	catalogStageListCapabilities  catalogStage = "model_list_capabilities"
	catalogStageListCursor        catalogStage = "model_list_cursor"
	catalogStageCacheOpen         catalogStage = "cache_open"
	catalogStageCacheMetadata     catalogStage = "cache_metadata"
	catalogStageCacheRead         catalogStage = "cache_read"
	catalogStageCacheSchema       catalogStage = "cache_schema"
	catalogStageCacheVersion      catalogStage = "cache_version"
	catalogStageCacheFreshness    catalogStage = "cache_freshness"
	catalogStageCacheIdentity     catalogStage = "cache_identity"
	catalogStageCacheCapabilities catalogStage = "cache_capabilities"
	catalogStageCapabilitiesMatch catalogStage = "capabilities_match"
	catalogStageCleanup           catalogStage = "cleanup"
	catalogStageResult            catalogStage = "result_validation"
)

type catalogDiagnosticError struct {
	stage catalogStage
	cause error
}

func (err catalogDiagnosticError) Error() string { return "provider model catalog observation failed" }
func (err catalogDiagnosticError) Unwrap() error { return err.cause }

func atCatalogStage(stage catalogStage, cause error) error {
	var existing catalogDiagnosticError
	if errors.As(cause, &existing) {
		return cause
	}
	return catalogDiagnosticError{stage: stage, cause: cause}
}

// DiagnosticStage возвращает только закрытый код; поля каталога не сериализуются в лог.
func (catalog ModelCatalog) DiagnosticStage() string {
	switch catalog.stage {
	case catalogStageCredential, catalogStageAuthentication, catalogStageRuntime, catalogStageAPI,
		catalogStageProcess, catalogStageInitialize, catalogStageLogin, catalogStageListCall,
		catalogStageListSchema, catalogStageListIdentity, catalogStageListCapabilities, catalogStageListCursor,
		catalogStageCacheOpen, catalogStageCacheMetadata, catalogStageCacheRead, catalogStageCacheSchema,
		catalogStageCacheVersion, catalogStageCacheFreshness, catalogStageCacheIdentity,
		catalogStageCacheCapabilities, catalogStageCapabilitiesMatch, catalogStageCleanup, catalogStageResult:
		return string(catalog.stage)
	default:
		return string(catalogStageUnknown)
	}
}
