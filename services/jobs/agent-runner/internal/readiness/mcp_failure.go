package readiness

import "errors"

// mcpStage identifies a closed startup diagnostic phase independently from private error text, endpoints and peer identity.
// mcpStage определяет закрытую диагностическую стадию подготовки отдельно от частного текста ошибки, endpoint и идентичности peer.
type mcpStage string

const (
	// mcpStageConfiguration labels managed-proof, required-tool and endpoint configuration checks before transport/socket preparation.
	// mcpStageConfiguration обозначает проверки управляемого доказательства, обязательных инструментов и endpoint до подготовки транспорта и сокета.
	mcpStageConfiguration mcpStage = "CONFIGURATION"
	// mcpStageTLSIdentity labels preparation of the selected upstream callback transport and default mTLS identity.
	// mcpStageTLSIdentity обозначает подготовку выбранного транспорта callback upstream и стандартной идентичности mTLS.
	mcpStageTLSIdentity mcpStage = "TLS_IDENTITY"
	// mcpStageSocket labels local random-capability generation and protected UDS creation, not upstream authorization.
	// mcpStageSocket обозначает создание случайного локального билета и защищённого UDS, а не авторизацию upstream.
	mcpStageSocket mcpStage = "LOCAL_SOCKET"
	// mcpStageCallbackAuthority labels file-callback construction and exact ticket agreement before local readiness exchanges.
	// mcpStageCallbackAuthority обозначает создание файлового callback и точное совпадение билета до локального обмена проверки готовности.
	mcpStageCallbackAuthority mcpStage = "CALLBACK_AUTHORITY"
	// mcpStageInitialize labels the first initialize request and its required non-null JSON-RPC result.
	// mcpStageInitialize обозначает первый запрос initialize и его обязательный непустой JSON-RPC-результат.
	mcpStageInitialize mcpStage = "INITIALIZE"
	// mcpStageInitialized labels the initialized notification that must return accepted status without a response body.
	// mcpStageInitialized обозначает уведомление initialized, которое должно вернуть принятый статус без тела ответа.
	mcpStageInitialized mcpStage = "INITIALIZED_NOTIFICATION"
	// mcpStageCatalogTransport labels the tools/list exchange and successful JSON response transport before descriptor validation.
	// mcpStageCatalogTransport обозначает обмен tools/list и успешный JSON-ответ транспорта до проверки описаний.
	mcpStageCatalogTransport mcpStage = "CATALOG_TRANSPORT"
	// mcpStageCatalogSchema labels closed envelope/tool descriptor validation and required managed input-schema matching.
	// mcpStageCatalogSchema обозначает проверку закрытых оболочек и описаний инструментов, соответствия обязательных управляемых входных схем.
	mcpStageCatalogSchema mcpStage = "CATALOG_SCHEMA"
	// mcpStageCatalogBinding labels exact catalog-name agreement and the final managed-proof freshness check.
	// mcpStageCatalogBinding обозначает точное совпадение имён каталога и итоговую проверку свежести управляемого доказательства.
	mcpStageCatalogBinding mcpStage = "CATALOG_BINDING"
)

// mcpStartupError carries one startup stage and non-nil underlying cause without replacing that cause's error text.
// mcpStartupError хранит одну стадию подготовки и непустую вложенную причину без замены текста её ошибки.
type mcpStartupError struct {
	stage mcpStage
	cause error
}

// Error delegates to the required non-nil cause; safe public stage projection is provided separately by FailureStage.
// Error передаёт вызов обязательной непустой причине; безопасная публичная проекция стадии предоставляется отдельно FailureStage.
func (e *mcpStartupError) Error() string { return e.cause.Error() }

// Unwrap preserves the underlying startup cause for errors.Is/As without adding diagnostic or authority state.
// Unwrap сохраняет вложенную причину подготовки для errors.Is и errors.As без добавления диагностики или полномочий.
func (e *mcpStartupError) Unwrap() error { return e.cause }

// withMCPStage leaves success unchanged, preserves the innermost existing stage and otherwise wraps a non-nil error with the current phase.
// withMCPStage не изменяет успех, сохраняет внутреннюю уже заданную стадию, иначе оборачивает непустую ошибку текущей фазой.
func withMCPStage(err error, stage mcpStage) error {
	if err == nil {
		return nil
	}
	var previous *mcpStartupError
	if errors.As(err, &previous) {
		return err
	}
	return &mcpStartupError{stage: stage, cause: err}
}

// FailureStage projects only registered stage names from a constructed startup wrapper, otherwise UNKNOWN without copying error contents or endpoints.
// Wrappers are expected to be non-nil values produced by withMCPStage; this is not a general error sanitizer.
// FailureStage выдаёт только зарегистрированные имена стадии из созданной оболочки подготовки, иначе UNKNOWN без копирования содержимого ошибки или endpoint.
// Ожидаются непустые оболочки от withMCPStage; это не универсальный очиститель ошибок.
func FailureStage(err error) string {
	var failure *mcpStartupError
	if errors.As(err, &failure) {
		switch failure.stage {
		case mcpStageConfiguration, mcpStageTLSIdentity, mcpStageSocket, mcpStageCallbackAuthority, mcpStageInitialize, mcpStageInitialized, mcpStageCatalogTransport, mcpStageCatalogSchema, mcpStageCatalogBinding:
			return string(failure.stage)
		}
	}
	return "UNKNOWN"
}
