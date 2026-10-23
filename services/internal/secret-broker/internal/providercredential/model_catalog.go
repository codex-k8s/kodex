package providercredential

import (
	"context"
	"errors"
	"regexp"
	"time"
)

const (
	// maximumCatalogModels caps the validated combined catalog and each accepted remote cache at 128 models,
	// including duplicate/default checks across pages.
	// maximumCatalogModels ограничивает проверенный объединённый каталог и каждый принимаемый remote cache
	// 128 моделями с проверкой повторов и defaults между страницами.
	maximumCatalogModels = 128
	// maximumCatalogEfforts limits each model to 16 distinct syntactically valid reasoning effort
	// identifiers before exposing its capabilities.
	// maximumCatalogEfforts ограничивает каждую модель 16 разными синтаксически допустимыми reasoning effort
	// идентификаторами до выдачи её возможностей.
	maximumCatalogEfforts = 16
	// maximumModelCatalogBytes bounds HTTP catalog bodies, embedded capability input and remote cache reads
	// to four MiB; consumers enforce it before interpreting the complete payload.
	// maximumModelCatalogBytes ограничивает HTTP body каталога, embedded capability вход и чтение remote
	// cache четырьмя МиБ; потребители применяют границу до интерпретации полного payload.
	maximumModelCatalogBytes = 4 << 20
	// modelCatalogTimeout bounds service and process catalog observation to 15 seconds without extending a
	// shorter caller or lifecycle deadline.
	// modelCatalogTimeout ограничивает service и process наблюдение каталога 15 секундами без продления
	// более короткого caller или lifecycle дедлайна.
	modelCatalogTimeout = 15 * time.Second
	// catalogCodexVersion is the exact runtime/cache version accepted by this adapter, not a dynamically
	// discovered latest CLI version.
	// catalogCodexVersion задаёт точную runtime/cache версию, принимаемую адаптером, а не динамически
	// определяемую последнюю версию CLI.
	catalogCodexVersion = "0.160.0"
)

// ModelCatalogSource identifies the verified remote API or Codex observation route; failure results
// carry no source.
// ModelCatalogSource обозначает проверенный маршрут наблюдения через remote API либо Codex; результаты
// отказа не содержат source.
type ModelCatalogSource string

// ModelCatalogFailure is a closed safe failure classification separate from transport/context errors and
// raw provider diagnostics.
// ModelCatalogFailure задаёт закрытую безопасную классификацию отказа отдельно от transport/context
// ошибок и сырой provider диагностики.
type ModelCatalogFailure string

const (
	// CatalogRemoteAPI marks account-specific API model availability intersected with the pinned dated API
	// capability source.
	// CatalogRemoteAPI обозначает доступность API моделей для account, пересечённую с закреплённым
	// датированным API capability источником.
	CatalogRemoteAPI ModelCatalogSource = "REMOTE_API"
	// CatalogRemoteCodex marks validated app-server capabilities, additionally checked against its fresh
	// pinned-version cache unless the explicit protocol-only mode is enabled.
	// CatalogRemoteCodex обозначает проверенные app-server capabilities, дополнительно сверенные со свежим
	// cache закреплённой версии, если явно не включён protocol-only режим.
	CatalogRemoteCodex ModelCatalogSource = "REMOTE_CODEX"
	// CatalogFailureNone marks a successful observation whose source, timestamp and capability schema still
	// require validation at the service boundary.
	// CatalogFailureNone обозначает успешное наблюдение, чьи source, timestamp и схема capabilities
	// по-прежнему проверяются на service границе.
	CatalogFailureNone ModelCatalogFailure = "NONE"
	// CatalogFailureUnavailable maps unclassified provider/runtime/storage failures to a bounded outcome
	// without exposing the original error text.
	// CatalogFailureUnavailable преобразует неклассифицированные provider/runtime/storage ошибки в
	// ограниченный исход без раскрытия исходного текста ошибки.
	CatalogFailureUnavailable ModelCatalogFailure = "UNAVAILABLE"
	// CatalogFailureUnverified rejects an unproven catalog source, stale snapshot or invalid capability
	// representation without returning partial models.
	// CatalogFailureUnverified отклоняет недоказанный источник каталога, устаревший снимок или неверное
	// представление возможностей без выдачи частичных моделей.
	CatalogFailureUnverified ModelCatalogFailure = "UNVERIFIED_SOURCE"
	// CatalogFailureAuthorization reports rejected credentials or a denied token-refresh request rather than
	// implying the entire provider is unavailable.
	// CatalogFailureAuthorization сообщает об отклонённых credentials или запрещённом запросе token refresh,
	// не объявляя весь provider недоступным.
	CatalogFailureAuthorization ModelCatalogFailure = "AUTHORIZATION_REJECTED"
)

var (
	// modelCatalogIDPattern accepts a bounded ASCII model identifier with an alphanumeric first character;
	// slash and colon are data, never filesystem path authority.
	// modelCatalogIDPattern допускает ограниченный ASCII идентификатор модели с буквой или цифрой в начале;
	// slash и colon остаются данными, не полномочиями пути файловой системы.
	modelCatalogIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	// modelCatalogEffortPattern admits bounded lowercase effort identifiers without imposing a hardcoded
	// list or a numeric ordering of model capabilities.
	// modelCatalogEffortPattern допускает ограниченные lowercase effort идентификаторы без жёсткого списка
	// или числового порядка возможностей модели.
	modelCatalogEffortPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	// errModelCatalogUnverified tags source, shape and freshness failures for safe catalog classification
	// while preserving a separately assigned diagnostic stage.
	// errModelCatalogUnverified помечает ошибки источника, формы и свежести для безопасной классификации
	// каталога с сохранением отдельно назначенного diagnostic stage.
	errModelCatalogUnverified = errors.New("provider model catalog source is unverified")
	// errModelCatalogAuthorization tags rejected authentication or forbidden observation-time refresh,
	// without carrying provider payload or token values.
	// errModelCatalogAuthorization помечает отклонённую аутентификацию либо запрещённый refresh при
	// наблюдении без provider payload и token значений.
	errModelCatalogAuthorization = errors.New("provider model catalog authorization was rejected")
)

// ModelCatalog contains validated capability metadata and observation provenance or a safe failure
// outcome, never raw provider responses or credentials. The private stage can be exposed only through
// the closed DiagnosticStage mapping.
// ModelCatalog содержит проверенные metadata возможностей и происхождение наблюдения либо безопасный
// отказ без сырых provider ответов и credentials. Приватный stage выдаётся только через закрытое
// преобразование DiagnosticStage.
type ModelCatalog struct {
	stage      catalogStage
	ObservedAt time.Time
	Source     ModelCatalogSource
	Models     []CatalogModel
	Failure    ModelCatalogFailure
}

// CatalogModel records one safe model ID, ordered supported effort identifiers, its effort default and
// optional model-default flag; these fields describe observed capabilities, not account authorization.
// CatalogModel фиксирует безопасный model ID, упорядоченные supported effort идентификаторы, effort
// default и необязательный model-default признак; поля описывают наблюдённые возможности, не авторизацию
// account.
type CatalogModel struct {
	ID                     string
	DefaultReasoningEffort string
	ReasoningEfforts       []string
	IsDefault              bool
}

// validateCatalogModels bounds models/efforts, rejects duplicate IDs or efforts and multiple default
// models, and requires a declared effort default to be supported. Empty catalogs and models with no
// efforts and no default are valid.
// validateCatalogModels ограничивает модели и efforts, отклоняет повторные ID или efforts и несколько
// default моделей и требует поддержки заявленного effort default. Пустой каталог и модель без efforts и
// default допустимы.
func validateCatalogModels(models []CatalogModel) error {
	if len(models) > maximumCatalogModels {
		return errModelCatalogUnverified
	}
	ids := make(map[string]struct{}, len(models))
	defaults := 0
	for _, model := range models {
		if !modelCatalogIDPattern.MatchString(model.ID) || len(model.ReasoningEfforts) > maximumCatalogEfforts {
			return errModelCatalogUnverified
		}
		if _, exists := ids[model.ID]; exists {
			return errModelCatalogUnverified
		}
		ids[model.ID] = struct{}{}
		if model.IsDefault {
			defaults++
			if defaults > 1 {
				return errModelCatalogUnverified
			}
		}
		if len(model.ReasoningEfforts) == 0 {
			if model.DefaultReasoningEffort != "" {
				return errModelCatalogUnverified
			}
			continue
		}
		efforts := make(map[string]struct{}, len(model.ReasoningEfforts))
		for _, effort := range model.ReasoningEfforts {
			if !modelCatalogEffortPattern.MatchString(effort) {
				return errModelCatalogUnverified
			}
			if _, exists := efforts[effort]; exists {
				return errModelCatalogUnverified
			}
			efforts[effort] = struct{}{}
		}
		if _, supported := efforts[model.DefaultReasoningEffort]; !supported {
			return errModelCatalogUnverified
		}
	}
	return nil
}

// catalogFailure preserves context cancellation as an error; otherwise it returns a timestamped
// metadata-only failure with a closed category and any existing diagnostic stage, discarding provider
// payload and partial models.
// catalogFailure сохраняет context cancellation как ошибку; иначе возвращает датированный отказ только с
// metadata, закрытой категорией и существующим diagnostic stage, отбрасывая provider payload и частичные
// модели.
func catalogFailure(ctx context.Context, err error) (ModelCatalog, error) {
	if ctx.Err() != nil {
		return ModelCatalog{}, ctx.Err()
	}
	failure := CatalogFailureUnavailable
	if errors.Is(err, errModelCatalogUnverified) {
		failure = CatalogFailureUnverified
	} else if errors.Is(err, errModelCatalogAuthorization) {
		failure = CatalogFailureAuthorization
	}
	var diagnostic catalogDiagnosticError
	_ = errors.As(err, &diagnostic)
	return ModelCatalog{ObservedAt: time.Now().UTC(), Failure: failure, stage: diagnostic.stage}, nil
}
