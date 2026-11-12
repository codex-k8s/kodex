package providercredential

import (
	"errors"
)

// catalogStage names observer-owned failure boundaries, never provider text or caller-controlled
// metric/log label values; public exposure passes through DiagnosticStage's allowlist.
// catalogStage обозначает принадлежащие observer границы отказа, а не provider текст или управляемые
// caller значения metric/log labels; публичная выдача проходит список DiagnosticStage.
type catalogStage string

const (
	// catalogStageUnknown is the bounded fallback for empty or unrecognized stages, preventing arbitrary
	// stored strings from becoming diagnostic labels.
	// catalogStageUnknown задаёт ограниченный fallback пустого или неизвестного stage, не позволяя
	// произвольным сохранённым строкам стать diagnostic labels.
	catalogStageUnknown catalogStage = "unknown"
	// catalogStageCredential identifies failure of the owner-selected exact credential read before any
	// catalog observation is invoked.
	// catalogStageCredential обозначает отказ точного чтения credential, выбранного владельцем, до вызова
	// наблюдения каталога.
	catalogStageCredential catalogStage = "credential_read"
	// catalogStageResult identifies service-level observation-time or source/failure/capability consistency
	// refusal after a lower-level observer returns.
	// catalogStageResult обозначает service отказ проверки времени наблюдения либо согласованности
	// source/failure/capabilities после возврата нижележащего observer.
	catalogStageResult catalogStage = "result_validation"
)

// catalogDiagnosticError wraps a private cause with an observer-assigned stage; its public Error text is
// fixed, while Unwrap preserves typed internal classification.
// catalogDiagnosticError оборачивает приватную причину с назначенным observer stage; публичный Error
// текст фиксирован, Unwrap сохраняет внутреннюю типизированную классификацию.
type catalogDiagnosticError struct {
	stage catalogStage
	cause error
}

// Error returns only the fixed catalog-observation failure message, never the wrapped cause or provider
// payload.
// Error возвращает только фиксированное сообщение отказа catalog observation без обёрнутой причины и
// provider payload.
func (err catalogDiagnosticError) Error() string { return "provider model catalog observation failed" }

// Unwrap exposes the original error for internal errors.Is/errors.As classification without including it
// in the public diagnostic string.
// Unwrap предоставляет исходную ошибку для внутренней errors.Is/errors.As классификации без включения её
// в публичную diagnostic строку.
func (err catalogDiagnosticError) Unwrap() error { return err.cause }

// atCatalogStage preserves an existing diagnostic wrapper so outer layers cannot overwrite the more
// precise inner stage; otherwise it attaches the supplied observer-owned stage.
// atCatalogStage сохраняет существующую diagnostic обёртку, не позволяя внешнему слою перезаписать более
// точный внутренний stage; иначе присоединяет переданный observer-owned stage.
func atCatalogStage(stage catalogStage, cause error) error {
	var existing catalogDiagnosticError
	if errors.As(cause, &existing) {
		return cause
	}
	return catalogDiagnosticError{stage: stage, cause: cause}
}

// DiagnosticStage returns only the closed observer stage allowlist or unknown; model fields, raw causes
// and arbitrary private stage values cannot become log labels.
// DiagnosticStage возвращает только закрытый список observer stages либо unknown; model поля, сырые
// причины и произвольные private stage значения не становятся log labels.
func (catalog ModelCatalog) DiagnosticStage() string {
	switch catalog.stage {
	case catalogStageCredential, catalogStageResult:
		return string(catalog.stage)
	default:
		return string(catalogStageUnknown)
	}
}
