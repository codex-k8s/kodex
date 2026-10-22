package providercredential

import (
	"context"
	"errors"
)

// catalogCleanupFailure preserves an existing primary error or classified failure; otherwise cleanup
// failure replaces apparent success with an unavailable metadata-only outcome at the cleanup stage.
// catalogCleanupFailure сохраняет существующую первичную ошибку либо классифицированный отказ; иначе
// ошибка cleanup заменяет видимый успех на metadata-only недоступность с cleanup stage.
func catalogCleanupFailure(ctx context.Context, result ModelCatalog, resultErr, cleanupErr error) (ModelCatalog, error) {
	if resultErr != nil || result.Failure != "" && result.Failure != CatalogFailureNone {
		return result, resultErr
	}
	return catalogFailure(ctx, atCatalogStage(catalogStageCleanup, cleanupErr))
}

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
	// catalogStageAuthentication identifies mode-specific auth-material rejection before selecting the HTTP
	// or app-server catalog route.
	// catalogStageAuthentication обозначает отказ auth material выбранного режима до выбора HTTP либо
	// app-server маршрута каталога.
	catalogStageAuthentication catalogStage = "authentication"
	// catalogStageRuntime identifies runtime readiness/version-pin refusal rather than interpreting it as a
	// model-list schema failure.
	// catalogStageRuntime обозначает отказ runtime readiness/version-привязки, не трактуя его как ошибку
	// схемы model-list.
	catalogStageRuntime catalogStage = "runtime_check"
	// catalogStageAPI marks failure of the managed HTTP model-list/capability intersection path without
	// exposing HTTP bodies or credential values.
	// catalogStageAPI помечает отказ управляемого HTTP model-list/capability intersection пути без раскрытия
	// HTTP body и credential значений.
	catalogStageAPI catalogStage = "api_catalog"
	// catalogStageProcess covers inability to create the private observation home or start the isolated
	// app-server process.
	// catalogStageProcess охватывает невозможность создать приватный observation home или запустить
	// изолированный app-server процесс.
	catalogStageProcess catalogStage = "process_start"
	// catalogStageInitialize identifies app-server initialize/initialized protocol failures before
	// account-token login or model enumeration.
	// catalogStageInitialize обозначает ошибки app-server initialize/initialized протокола до account-token
	// login и перечисления моделей.
	catalogStageInitialize catalogStage = "initialize"
	// catalogStageLogin marks failure or malformed type during access-token account login, without exposing
	// the login response.
	// catalogStageLogin помечает ошибку либо неверный type при access-token account login без раскрытия
	// login ответа.
	catalogStageLogin catalogStage = "login_response"
	// catalogStageListCall identifies an unsuccessful model/list RPC separately from subsequent schema and
	// capability validation.
	// catalogStageListCall обозначает неуспешный model/list RPC отдельно от последующей проверки схемы и
	// возможностей.
	catalogStageListCall catalogStage = "model_list_call"
	// catalogStageListSchema identifies missing or oversized model/list page data or an undecodable response
	// envelope.
	// catalogStageListSchema обозначает отсутствующие или слишком большие model/list page данные либо
	// неразбираемый response envelope.
	catalogStageListSchema catalogStage = "model_list_schema"
	// catalogStageListIdentity marks disagreement between the protocol's ID and model fields before
	// accepting that model's capabilities.
	// catalogStageListIdentity помечает несовпадение protocol полей ID и model до принятия возможностей
	// модели.
	catalogStageListIdentity catalogStage = "model_list_identity"
	// catalogStageListCapabilities marks invalid accumulated protocol models, duplicate/default/effort
	// constraints or invalid supplied protocol capabilities.
	// catalogStageListCapabilities помечает неверные накопленные protocol модели, ограничения
	// duplicate/default/effort либо неверные переданные protocol capabilities.
	catalogStageListCapabilities catalogStage = "model_list_capabilities"
	// catalogStageListCursor identifies empty, repeated, oversize or over-budget continuation rather than
	// silently returning a partial catalog.
	// catalogStageListCursor обозначает пустое, повторное, слишком длинное или превышающее бюджет
	// продолжение вместо молчаливой выдачи частичного каталога.
	catalogStageListCursor catalogStage = "model_list_cursor"
	// catalogStageCacheOpen identifies failure to open the exact private cache path with no-follow
	// semantics; it does not convert a missing cache into trusted capabilities.
	// catalogStageCacheOpen обозначает отказ открытия точного private cache пути без следования symlink;
	// отсутствие cache не превращается в доверенные capabilities.
	catalogStageCacheOpen catalogStage = "cache_open"
	// catalogStageCacheMetadata identifies invalid regular-file, size, owner or link-count metadata before
	// cache bytes are interpreted.
	// catalogStageCacheMetadata обозначает неверные metadata обычного файла, размера, владельца или link
	// count до интерпретации cache байтов.
	catalogStageCacheMetadata catalogStage = "cache_metadata"
	// catalogStageCacheRead marks a bounded cache read failure or oversized result independently of JSON
	// schema checks.
	// catalogStageCacheRead помечает ошибку ограниченного чтения cache либо слишком большой результат
	// отдельно от проверок JSON схемы.
	catalogStageCacheRead catalogStage = "cache_read"
	// catalogStageCacheSchema identifies undecodable cache JSON, absent model collection or a cache
	// exceeding supported model/byte bounds.
	// catalogStageCacheSchema обозначает неразбираемый cache JSON, отсутствие model collection либо
	// превышение поддерживаемых model/byte границ.
	catalogStageCacheSchema catalogStage = "cache_schema"
	// catalogStageCacheVersion identifies a cache client version different from the pinned executable
	// protocol version.
	// catalogStageCacheVersion обозначает cache client версию, отличную от закреплённой версии executable
	// протокола.
	catalogStageCacheVersion catalogStage = "cache_version"
	// catalogStageCacheFreshness identifies a cache fetch time before this observation or after the current
	// time, preventing stale/future snapshots from being treated as fresh.
	// catalogStageCacheFreshness обозначает cache fetch до этого наблюдения либо после текущего времени,
	// исключая признание устаревшего/будущего снимка свежим.
	catalogStageCacheFreshness catalogStage = "cache_freshness"
	// catalogStageCacheIdentity marks duplicate or syntactically invalid remote model slugs, not an account
	// authentication verdict.
	// catalogStageCacheIdentity помечает повторные либо синтаксически неверные remote model slugs, а не
	// результат аутентификации account.
	catalogStageCacheIdentity catalogStage = "cache_identity"
	// catalogStageCacheCapabilities identifies an invalid individual cache model's effort/default
	// representation before intersection.
	// catalogStageCacheCapabilities обозначает неверное представление effort/default отдельной cache модели
	// до пересечения.
	catalogStageCacheCapabilities catalogStage = "cache_capabilities"
	// catalogStageCapabilitiesMatch marks disagreement of default or ordered efforts between protocol
	// capabilities and the matching fresh-cache model.
	// catalogStageCapabilitiesMatch помечает несовпадение default либо упорядоченных efforts между protocol
	// capabilities и соответствующей fresh-cache моделью.
	catalogStageCapabilitiesMatch catalogStage = "capabilities_match"
	// catalogStageCleanup marks loss of a trustworthy successful outcome because process termination or
	// private-home removal failed.
	// catalogStageCleanup помечает утрату достоверного успешного исхода из-за ошибки завершения процесса
	// либо удаления private home.
	catalogStageCleanup catalogStage = "cleanup"
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
