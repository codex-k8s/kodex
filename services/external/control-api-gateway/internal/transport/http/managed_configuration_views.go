package httptransport

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// errManagedConfigurationShape classifies a malformed owner configuration or revision projection
// before it can be exposed as a valid HTTP result. Its diagnostic contains no source content or
// credential data.
// errManagedConfigurationShape обозначает некорректную проекцию конфигурации или ревизии владельца до
// выдачи допустимого HTTP-ответа. Диагностика не содержит исходного содержимого или реквизитов.
var errManagedConfigurationShape = errors.New("managed configuration response is invalid")

// managedConfigurationResponse exposes the two common generated response getters used by checked HTTP
// result conversion. The interface itself does not guarantee that the values are present, consistent,
// or authorized.
// managedConfigurationResponse предоставляет два общих getter сгенерированного ответа для проверяемого
// HTTP-преобразования. Сам интерфейс не гарантирует наличие, согласованность или допустимость
// значений.
type managedConfigurationResponse interface {
	// GetConfiguration returns the owner response's configuration projection for subsequent shape
	// validation, not a caller-selected resource lookup.
	// GetConfiguration возвращает проекцию конфигурации ответа владельца для последующей проверки формы, а
	// не поиск выбранного вызывающим кодом ресурса.
	GetConfiguration() *controlplanev1.ManagedConfigurationSet
	// GetRevision returns the revision projection that must pass the kind-dependent availability and
	// revision-shape checks before response encoding.
	// GetRevision возвращает проекцию ревизии, которая проходит зависимую от вида проверку доступности и
	// проверку формы до кодирования ответа.
	GetRevision() *controlplanev1.ManagedConfigurationRevision
}

// requireManagedDraftMutation rejects prompt scope on other kinds, blank or over-160-byte names, empty
// or over-256-KiB content, and an empty explicit configuration reference or a supplied reference
// without ETag. It accepts TEXT/JSON/YAML/TOML and allows OPENAPI_IMPORT only for integration
// definitions, then parses mutation headers without independently requiring a nonempty idempotency
// key.
// requireManagedDraftMutation отклоняет область промпта для иных видов, пустые имена или имена
// свыше160 байтов, пустое содержимое или содержимое свыше256КиБ и пустую явную ссылку конфигурации
// либо переданную ссылку без ETag. Принимаются TEXT/JSON/YAML/TOML и OPENAPI_IMPORT только для
// интеграций; затем разбираются заголовки изменения без отдельного требования непустого ключа
// идемпотентности.
func requireManagedDraftMutation(w http.ResponseWriter, key, etag string, body generated.ManagedConfigurationDraftInput, kind controlplanev1.ManagedConfigurationKind) (*controlplanev1.MutationContext, bool) {
	if body.PromptScope != nil && kind != controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	if strings.TrimSpace(body.Name) == "" || len(body.Name) > 160 || len(body.Content) == 0 || len(body.Content) > 256<<10 ||
		body.ConfigurationRef != nil && (*body.ConfigurationRef == "" || etag == "") {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	switch body.ContentFormat {
	case "TEXT", "JSON", "YAML", "TOML":
	case "OPENAPI_IMPORT":
		if kind != controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return nil, false
		}
	default:
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return requireMutation(w, key, etag)
}

// requireManagedDraftProject checks presence semantics only: project-scoped input needs a nonblank
// reference with no surrounding whitespace, while organization-scoped input must omit the project
// field entirely. It writes the existing 400 problem on rejection; reference grammar and project
// ownership are checked elsewhere.
// requireManagedDraftProject проверяет только присутствие: проектному входу нужна непустая ссылка без
// крайних пробелов, а общий вход полностью исключает поле проекта. Отказ пишет прежнюю проблему400;
// грамматика ссылки и владение проектом проверяются отдельно.
func requireManagedDraftProject(w http.ResponseWriter, body generated.ManagedConfigurationDraftInput, required bool) bool {
	reference := stringValue(body.ProjectRef)
	valid := body.ProjectRef == nil
	if required {
		valid = body.ProjectRef != nil && strings.TrimSpace(reference) == reference && reference != ""
	}
	if !valid {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
	}
	return valid
}

// managedConsumerInput requires a valid impact digest and one to 128 consumer selections, converts
// each closed absent-or-match shape, rejects duplicate kind/reference pairs, and preserves order. A
// local failure writes 400; accepted selectors still require owner-side resource and version
// validation.
// managedConsumerInput требует допустимый дайджест влияния и от одного до128 потребителей, преобразует
// закрытую форму отсутствия или совпадения, отклоняет одинаковые пары вид/ссылка и сохраняет порядок.
// Локальный отказ пишет400; принятый выбор ещё проверяется владельцем по ресурсам и версиям.
func managedConsumerInput(w http.ResponseWriter, input generated.ManagedConfigurationRebindInput) ([]*controlplanev1.ManagedConfigurationConsumer, bool) {
	if !validManagedDigest(input.ImpactDigest) || len(input.Consumers) == 0 || len(input.Consumers) > 128 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	result := make([]*controlplanev1.ManagedConfigurationConsumer, 0, len(input.Consumers))
	seen := make(map[string]struct{}, len(input.Consumers))
	for _, value := range input.Consumers {
		item, valid := managedConsumerSelection(value)
		if !valid {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return nil, false
		}
		key := item.Kind + "\x00" + item.Ref
		_, duplicate := seen[key]
		if duplicate {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return nil, false
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result, true
}

// managedConsumerSelection inspects raw generated oneOf fields before typed conversion, rejects
// unknown keys and nonboolean presence flags, and forbids even null or zero revision/version fields in
// the absent branch. The match branch requires a positive safe version and an eight-to-128-byte ASCII
// revision reference; this parsing does not authenticate the consumer.
// managedConsumerSelection проверяет исходные поля сгенерированного oneOf до типизации, отклоняет
// неизвестные ключи и небулевы признаки отсутствия и запрещает даже null или нулевые revision/version
// в ветви отсутствия. Ветви совпадения нужны положительная безопасная версия и ASCII-ссылка ревизии
// от8 до128 байтов; разбор не удостоверяет потребителя.
func managedConsumerSelection(value generated.ManagedConfigurationConsumerInput) (*controlplanev1.ManagedConfigurationConsumer, bool) {
	// Generated oneOf retains raw JSON without enforcing additionalProperties; inspect the closed
	// field set and explicit null pins before typed conversion.
	// Сгенерированный oneOf сохраняет исходный JSON без проверки additionalProperties; закрытый
	// набор полей и явные null-привязки проверяются до типизированного преобразования.
	raw, err := value.MarshalJSON()
	var fields map[string]json.RawMessage
	if err != nil || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, false
	}
	for key := range fields {
		switch key {
		case "kind", "ref", "expectedAbsent", "revisionRef", "version":
		default:
			return nil, false
		}
	}
	expectedAbsent := false
	if flag, present := fields["expectedAbsent"]; present {
		switch string(flag) {
		case "true":
			expectedAbsent = true
		case "false":
		default:
			return nil, false
		}
	}
	if expectedAbsent {
		if _, present := fields["revisionRef"]; present {
			return nil, false
		}
		if _, present := fields["version"]; present {
			return nil, false
		}
		selection, err := value.AsManagedConfigurationConsumerAbsent()
		if err != nil || !managedConsumerIdentity(string(selection.Kind), selection.Ref) {
			return nil, false
		}
		return &controlplanev1.ManagedConfigurationConsumer{Kind: string(selection.Kind), Ref: selection.Ref, ExpectedAbsent: true}, true
	}
	selection, err := value.AsManagedConfigurationConsumerMatch()
	if err != nil || !managedConsumerIdentity(string(selection.Kind), selection.Ref) || !validManagedVersion(selection.Version) || len(selection.RevisionRef) < 8 || len(selection.RevisionRef) > 128 {
		return nil, false
	}
	for _, char := range selection.RevisionRef {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
			return nil, false
		}
	}
	return &controlplanev1.ManagedConfigurationConsumer{Kind: string(selection.Kind), Ref: selection.Ref, RevisionRef: selection.RevisionRef, Version: selection.Version}, true
}

// managedConsumerIdentity accepts only the closed agent, continuation, workflow, schedule,
// runtime-environment, integration-connection, and STT-service kinds with a nonempty reference of at
// most 160 bytes. It performs no reference normalization, ownership lookup, or permission check.
// managedConsumerIdentity принимает только закрытые виды сотрудника, продолжения, процесса,
// расписания, окружения, подключения интеграции и STT-сервиса с непустой ссылкой максимум160 байтов.
// Нормализация, поиск владельца и проверка доступа не выполняются.
func managedConsumerIdentity(kind, ref string) bool {
	if ref == "" || len(ref) > 160 {
		return false
	}
	switch kind {
	case "AGENT", "AGENT_CONTINUATION", "WORKFLOW", "SCHEDULE", "RUNTIME_ENVIRONMENT", "INTEGRATION_CONNECTION", "STT_SERVICE":
		return true
	default:
		return false
	}
}

// writeManagedResult validates the aggregate and kind-dependent revision shapes before setting the
// quoted configuration-version ETag and writing the requested status with a typed result. Invalid
// projections fail with 502 before response publication; this helper does not compare the result to a
// particular endpoint's expected plan or path.
// writeManagedResult проверяет формы агрегата и зависящей от вида ревизии до установки ETag с версией
// в кавычках и выдачи типизированного результата с нужным статусом. Неверная проекция даёт502 до
// публикации ответа; конкретные ожидаемые план или путь endpoint здесь не сверяются.
func writeManagedResult(w http.ResponseWriter, statusCode int, value managedConfigurationResponse) {
	configuration, err := managedConfigurationView(value.GetConfiguration())
	if err != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	revision, err := managedRevisionForConfiguration(value.GetRevision(), value.GetConfiguration())
	if err != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", configuration.Version))
	writeJSON(w, statusCode, generated.ManagedConfigurationResult{Configuration: configuration, Revision: revision})
}

// managedConfigurationView first validates metadata, then conditionally validates the current revision
// for the aggregate's kind. It preserves configuration source text and enum-like content rather than
// applying generic string localization or normalization; it does not resolve owner eligibility.
// managedConfigurationView сначала проверяет метаданные, затем при наличии текущую ревизию с учётом
// вида агрегата. Исходный текст и похожее на enum содержимое сохраняются без общей локализации или
// нормализации строк; допустимость владельца не определяется.
func managedConfigurationView(value *controlplanev1.ManagedConfigurationSet) (generated.ManagedConfiguration, error) {
	result, err := managedConfigurationMetadataView(value)
	if err != nil {
		return generated.ManagedConfiguration{}, err
	}
	if value.GetCurrentRevision() != nil {
		revision, err := managedRevisionForConfiguration(value.GetCurrentRevision(), value)
		if err != nil {
			return generated.ManagedConfiguration{}, err
		}
		result.CurrentRevision = &revision
	}
	return result, nil
}

// managedConfigurationMetadataView rejects missing identity, unsafe version or time, unknown
// kind/owner, inconsistent image-source-editability presence, duplicate or unsupported actions, and
// malformed optional copy provenance. It maps metadata and validated Git source without rewriting
// names or source text; these shape checks do not verify a cryptographic source origin.
// managedConfigurationMetadataView отклоняет отсутствие личности, небезопасные версию или время,
// неизвестные вид или владельца, несогласованное присутствие редактируемости исходника образа,
// повторные или неизвестные действия и неверное происхождение копии. Метаданные и проверенный
// Git-источник переносятся без изменения имён или текста; проверка формы не удостоверяет
// криптографическое происхождение.
func managedConfigurationMetadataView(value *controlplanev1.ManagedConfigurationSet) (generated.ManagedConfiguration, error) {
	if value == nil || value.GetRef() == "" || !validManagedVersion(value.GetVersion()) || value.GetUpdatedAt() == nil || value.GetUpdatedAt().CheckValid() != nil {
		return generated.ManagedConfiguration{}, errManagedConfigurationShape
	}
	switch value.GetKind() {
	case controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE,
		controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE,
		controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION,
		controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT,
		controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_EMAIL_MAILBOX:
	default:
		return generated.ManagedConfiguration{}, errManagedConfigurationShape
	}
	if value.GetManagedBy() != controlplanev1.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_UI && value.GetManagedBy() != controlplanev1.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_GIT {
		return generated.ManagedConfiguration{}, errManagedConfigurationShape
	}
	if (value.GetKind() == controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE) != (value.SourceEditable != nil) {
		return generated.ManagedConfiguration{}, errManagedConfigurationShape
	}
	result := generated.ManagedConfiguration{
		NextActions:    make([]generated.ManagedConfigurationNextActions, 0, len(value.GetNextActions())),
		Archived:       value.GetArchived(),
		SourceEditable: value.SourceEditable,
		Ref:            value.GetRef(), Version: value.GetVersion(), Name: value.GetName(), ProjectRef: optionalManagedString(value.GetProjectRef()),
		Kind:      generated.ManagedConfigurationKind(strings.TrimPrefix(value.GetKind().String(), "MANAGED_CONFIGURATION_KIND_")),
		ManagedBy: generated.ManagedConfigurationManagedBy(strings.TrimPrefix(value.GetManagedBy().String(), "MANAGED_CONFIGURATION_OWNER_")),
		Source:    value.GetSource(), SourceRevision: value.GetSourceRevision(), UpdatedAt: value.GetUpdatedAt().AsTime(),
	}
	seenActions := make(map[string]bool)
	for _, action := range value.GetNextActions() {
		if (action != "COPY" && action != "ARCHIVE") || seenActions[action] {
			return generated.ManagedConfiguration{}, errManagedConfigurationShape
		}
		seenActions[action] = true
		result.NextActions = append(result.NextActions, generated.ManagedConfigurationNextActions(action))
	}
	var err error
	if provenance := value.GetCopyProvenance(); provenance != nil {
		origin := strings.TrimPrefix(provenance.GetOrigin().String(), "MANAGED_CONFIGURATION_COPY_ORIGIN_")
		if (origin != "SHIPPED" && origin != "UI" && origin != "GIT") ||
			strings.TrimSpace(provenance.GetSourceRef()) == "" || len(provenance.GetSourceRef()) > 2000 ||
			strings.TrimSpace(provenance.GetSourceRevision()) == "" || len(provenance.GetSourceRevision()) > 2000 ||
			!validManagedVersion(provenance.GetSourceVersion()) || !validManagedDigest(provenance.GetSourceDigest()) {
			return generated.ManagedConfiguration{}, errManagedConfigurationShape
		}
		result.CopyProvenance = &generated.ManagedConfigurationCopyProvenance{
			Origin: generated.ManagedConfigurationCopyProvenanceOrigin(origin), SourceRef: provenance.GetSourceRef(),
			SourceRevision: provenance.GetSourceRevision(), SourceVersion: provenance.GetSourceVersion(), SourceDigest: provenance.GetSourceDigest(),
		}
	}
	result.GitSource, err = managedGitSourceView(value)
	if err != nil {
		return generated.ManagedConfiguration{}, err
	}
	return result, nil
}

// managedRevisionForConfiguration requires both projections and enforces that SourceAvailable presence
// matches the ROLE_IMAGE kind before calling the revision validator. It does not independently prove
// that the revision belongs to an expected route resource or impact plan.
// managedRevisionForConfiguration требует обе проекции и соответствие присутствия SourceAvailable виду
// ROLE_IMAGE перед общей проверкой ревизии. Принадлежность ревизии ожидаемому ресурсу пути или плану
// влияния отдельно не доказывается.
func managedRevisionForConfiguration(value *controlplanev1.ManagedConfigurationRevision, configuration *controlplanev1.ManagedConfigurationSet) (generated.ManagedConfigurationRevision, error) {
	if value == nil || configuration == nil || (configuration.GetKind() == controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE) != (value.SourceAvailable != nil) {
		return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
	}
	return managedRevisionView(value)
}

// managedRevisionView checks nonempty reference, safe revision, lowercase hex digest, valid
// timestamps, closed lifecycle and content-format sets, and hidden-source consistency before
// conversion. Diagnostics become a copied nonnil slice, prompt scope is checked, and content is
// preserved verbatim; the digest is format-checked rather than recomputed from content.
// managedRevisionView проверяет непустую ссылку, безопасную ревизию, строчный hex-дайджест, время,
// закрытые состояния и форматы и согласованность скрытого исходника до преобразования. Диагностика
// копируется в ненулевой срез, область промпта проверяется, содержимое сохраняется; формат дайджеста
// проверяется без пересчёта по содержимому.
func managedRevisionView(value *controlplanev1.ManagedConfigurationRevision) (generated.ManagedConfigurationRevision, error) {
	if value == nil || value.GetRef() == "" || !validManagedVersion(value.GetRevision()) || !validManagedDigest(value.GetDigest()) || value.GetCreatedAt() == nil || value.GetCreatedAt().CheckValid() != nil {
		return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
	}
	if value.SourceAvailable != nil && !value.GetSourceAvailable() && (value.GetContent() != "" || len(value.GetValidationDiagnostics()) != 0) {
		return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
	}
	switch value.GetState() {
	case controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DRAFT,
		controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_VALID,
		controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_INVALID,
		controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_PUBLISHED,
		controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_SUPERSEDED,
		controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DISCARDED:
	default:
		return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
	}
	switch value.GetContentFormat() {
	case "TEXT", "JSON", "YAML", "TOML":
	default:
		return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
	}
	result := generated.ManagedConfigurationRevision{
		SourceAvailable: value.SourceAvailable,
		Ref:             value.GetRef(), Revision: value.GetRevision(), Digest: value.GetDigest(), Content: value.GetContent(),
		ContentFormat: generated.ManagedConfigurationRevisionContentFormat(value.GetContentFormat()),
		State:         generated.ManagedConfigurationRevisionState(strings.TrimPrefix(value.GetState().String(), "MANAGED_CONFIGURATION_STATE_")),
		CreatedAt:     value.GetCreatedAt().AsTime(), ParentRevisionRef: optionalManagedString(value.GetParentRevisionRef()),
		ValidationDiagnostics: append([]string{}, value.GetValidationDiagnostics()...),
	}
	if value.GetValidatedAt() != nil {
		if value.GetValidatedAt().CheckValid() != nil {
			return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
		}
		validated := value.GetValidatedAt().AsTime()
		result.ValidatedAt = &validated
	}
	if value.GetPublishedAt() != nil {
		if value.GetPublishedAt().CheckValid() != nil {
			return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
		}
		published := value.GetPublishedAt().AsTime()
		result.PublishedAt = &published
	}
	scope, ok := promptScopeView(value.GetPromptScope())
	if !ok {
		return generated.ManagedConfigurationRevision{}, errManagedConfigurationShape
	}
	result.PromptScope = scope
	return result, nil
}

// managedConsumerView validates a nonnil known-kind consumer with nonempty revision and safe version,
// then exposes only its read DTO fields. It neither exposes ExpectedAbsent nor treats consumer
// identity syntax as ownership proof.
// managedConsumerView проверяет ненулевого потребителя известного вида с непустой ревизией и
// безопасной версией, затем выдаёт только поля DTO чтения. ExpectedAbsent не раскрывается, синтаксис
// личности не считается доказательством владения.
func managedConsumerView(value *controlplanev1.ManagedConfigurationConsumer) (generated.ManagedConfigurationConsumer, error) {
	if value == nil || !managedConsumerIdentity(value.GetKind(), value.GetRef()) || value.GetRevisionRef() == "" || !validManagedVersion(value.GetVersion()) {
		return generated.ManagedConfigurationConsumer{}, errManagedConfigurationShape
	}
	return generated.ManagedConfigurationConsumer{Kind: generated.ManagedConfigurationConsumerKind(value.GetKind()), Ref: value.GetRef(), RevisionRef: value.GetRevisionRef(), Version: value.GetVersion()}, nil
}

// validManagedVersion accepts positive versions no greater than maximumSafeJSONInteger, preserving
// exact JSON integer representation. It does not compare that version to owner state.
// validManagedVersion принимает положительные версии не выше maximumSafeJSONInteger для точного
// целочисленного JSON-представления. Состояние владельца с этой версией не сравнивается.
func validManagedVersion(value int64) bool { return value > 0 && value <= maximumSafeJSONInteger }

// validManagedDigest accepts exactly 64 lowercase hexadecimal characters using decoding after length
// and case checks. It validates syntax, not content integrity, signature, provenance, or admission.
// validManagedDigest принимает ровно64 строчных шестнадцатеричных символа, декодируя после проверок
// длины и регистра. Проверяется синтаксис, а не целостность содержимого, подпись, происхождение или
// допуск.
func validManagedDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// optionalManagedString maps an empty value to nil and otherwise returns a pointer to the unchanged
// string value. It does not trim, localize, validate, or resolve that string.
// optionalManagedString превращает пустое значение в nil, иначе возвращает указатель на неизменённое
// строковое значение. Усечение, локализация, проверка и разрешение строки не выполняются.
func optionalManagedString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
