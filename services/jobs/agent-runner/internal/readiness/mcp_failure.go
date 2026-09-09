package readiness

import "errors"

type mcpStage string

const (
	mcpStageConfiguration     mcpStage = "CONFIGURATION"
	mcpStageTLSIdentity       mcpStage = "TLS_IDENTITY"
	mcpStageSocket            mcpStage = "LOCAL_SOCKET"
	mcpStageCallbackAuthority mcpStage = "CALLBACK_AUTHORITY"
	mcpStageInitialize        mcpStage = "INITIALIZE"
	mcpStageInitialized       mcpStage = "INITIALIZED_NOTIFICATION"
	mcpStageCatalogTransport  mcpStage = "CATALOG_TRANSPORT"
	mcpStageCatalogSchema     mcpStage = "CATALOG_SCHEMA"
	mcpStageCatalogBinding    mcpStage = "CATALOG_BINDING"
)

type mcpStartupError struct {
	stage mcpStage
	cause error
}

func (e *mcpStartupError) Error() string { return e.cause.Error() }
func (e *mcpStartupError) Unwrap() error { return e.cause }
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

// FailureStage возвращает только закрытый код, без URL, peer, input или текста ошибки.
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
