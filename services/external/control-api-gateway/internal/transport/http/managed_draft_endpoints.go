package httptransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// SavePromptTemplateDraft validates a TEXT draft and optional prompt scope, calls the exact typed save
// RPC and checks the returned scope before shared revision/owner readback validation.
// SavePromptTemplateDraft проверяет TEXT-черновик и необязательную область промпта, вызывает точный
// типизированный RPC сохранения и сверяет возвращённую область до общей проверки ревизии и владельца.
func (server *Server) SavePromptTemplateDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.SavePromptTemplateDraftParams) {
	body, mutation, ok := readManagedDraftSave(w, r, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch, controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE)
	if !ok {
		return
	}
	scope, ok := promptScopeInput(body.PromptScope)
	if !ok {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	result, err := server.control.Command.SavePromptTemplateDraft(r.Context(), &controlplanev1.SavePromptTemplateDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef, ContentFormat: string(body.ContentFormat), Content: *body.Content, PromptScope: scope,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if !validPromptScopeReceipt(scope, result.GetRevision().GetPromptScope()) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeManagedDraftResult(r.Context(), w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE, &body)
}

// DiscardPromptTemplateDraft requires exact references and versioned idempotency, invokes only the
// prompt-template discard command and validates its unchanged revision as DISCARDED.
// DiscardPromptTemplateDraft требует точные ссылки и версионную идемпотентность, вызывает лишь команду
// удаления черновика шаблона промпта и проверяет прежнюю ревизию как DISCARDED.
func (server *Server) DiscardPromptTemplateDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.DiscardPromptTemplateDraftParams) {
	mutation, ok := managedDraftMutation(w, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.DiscardPromptTemplateDraft(r.Context(), &controlplanev1.DiscardPromptTemplateDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(r.Context(), w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE, nil)
}

// SaveRoleImageRevisionDraft validates the role-image source without requiring it to parse as a
// published recipe, invokes its exact save RPC and validates a new child draft receipt.
// SaveRoleImageRevisionDraft проверяет исходник образа роли, не требуя разбора как опубликованного
// рецепта, вызывает точный RPC сохранения и сверяет результат как новый дочерний черновик.
func (server *Server) SaveRoleImageRevisionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.SaveRoleImageRevisionDraftParams) {
	body, mutation, ok := readManagedDraftSave(w, r, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch, controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE)
	if !ok {
		return
	}
	result, err := server.control.Command.SaveRoleImageRevisionDraft(r.Context(), &controlplanev1.SaveRoleImageRevisionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef, ContentFormat: string(body.ContentFormat), Content: *body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(r.Context(), w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE, &body)
}

// DiscardRoleImageRevisionDraft dispatches the exact versioned role-image discard RPC and verifies the
// owner's discarded-revision receipt; it does not delete an image or grant admission.
// DiscardRoleImageRevisionDraft направляет точный версионный RPC удаления черновика образа роли и
// сверяет ответ владельца; образ не удаляет и допуск не выдаёт.
func (server *Server) DiscardRoleImageRevisionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.DiscardRoleImageRevisionDraftParams) {
	mutation, ok := managedDraftMutation(w, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.DiscardRoleImageRevisionDraft(r.Context(), &controlplanev1.DiscardRoleImageRevisionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(r.Context(), w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE, nil)
}

// SaveIntegrationDefinitionDraft validates the integration draft format/body and invokes its
// specialized save command; shared receipt validation compares normalized content, digest and lineage.
// SaveIntegrationDefinitionDraft проверяет формат и тело черновика интеграции и вызывает
// специализированную команду сохранения; общая проверка ответа сравнивает нормализованный текст, хеш и
// происхождение.
func (server *Server) SaveIntegrationDefinitionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.SaveIntegrationDefinitionDraftParams) {
	body, mutation, ok := readManagedDraftSave(w, r, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch, controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION)
	if !ok {
		return
	}
	result, err := server.control.Command.SaveIntegrationDefinitionDraft(r.Context(), &controlplanev1.SaveIntegrationDefinitionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef, ContentFormat: string(body.ContentFormat), Content: *body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(r.Context(), w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION, &body)
}

// DiscardIntegrationDefinitionDraft validates mutation inputs, calls only the integration-definition
// discard RPC and checks the exact discarded revision; it does not disconnect a provider.
// DiscardIntegrationDefinitionDraft проверяет ввод изменения, вызывает только RPC удаления черновика
// определения интеграции и сверяет точную ревизию; провайдера не отключает.
func (server *Server) DiscardIntegrationDefinitionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.DiscardIntegrationDefinitionDraftParams) {
	mutation, ok := managedDraftMutation(w, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.DiscardIntegrationDefinitionDraft(r.Context(), &controlplanev1.DiscardIntegrationDefinitionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(r.Context(), w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION, nil)
}

// SaveSystemSTTConfigurationDraft saves source through the exact system-STT command after shared input
// validation and checks a new child draft; no STT request or publication is performed here.
// SaveSystemSTTConfigurationDraft сохраняет исходник точной командой системной STT после общей
// проверки ввода и сверяет новый дочерний черновик; STT-запрос и публикация здесь не выполняются.
func (server *Server) SaveSystemSTTConfigurationDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.SaveSystemSTTConfigurationDraftParams) {
	body, mutation, ok := readManagedDraftSave(w, r, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch, controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT)
	if !ok {
		return
	}
	result, err := server.control.Command.SaveSystemSTTConfigurationDraft(r.Context(), &controlplanev1.SaveSystemSTTConfigurationDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef, ContentFormat: string(body.ContentFormat), Content: *body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(r.Context(), w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT, &body)
}

// DiscardSystemSTTConfigurationDraft forwards the exact versioned STT-draft discard command and
// validates its receipt, without changing a published runtime configuration directly.
// DiscardSystemSTTConfigurationDraft передаёт точную версионную команду удаления STT-черновика и
// проверяет ответ, не меняя опубликованную runtime-конфигурацию напрямую.
func (server *Server) DiscardSystemSTTConfigurationDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.DiscardSystemSTTConfigurationDraftParams) {
	mutation, ok := managedDraftMutation(w, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.DiscardSystemSTTConfigurationDraft(r.Context(), &controlplanev1.DiscardSystemSTTConfigurationDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(r.Context(), w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT, nil)
}

// managedDraftMutation validates both opaque references before requiring a bounded idempotency key and
// exact strong version ETag; these syntax checks do not establish resource ownership.
// managedDraftMutation проверяет обе непрозрачные ссылки до требования ограниченного ключа
// идемпотентности и точного сильного версионного ETag; синтаксис не доказывает владение ресурсом.
func managedDraftMutation(w http.ResponseWriter, configurationRef, revisionRef, key, etag string) (*controlplanev1.MutationContext, bool) {
	if !opaqueHTTPReference.MatchString(configurationRef) || !opaqueHTTPReference.MatchString(revisionRef) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return requireVersionedMutation(w, key, etag)
}

// readManagedDraftSave decodes the closed input, enforces kind-specific formats and prompt-scope
// placement, and allows empty but present UTF-8 content up to 256 KiB without NUL. It then validates
// references/OCC; semantic publication validation remains separate.
// readManagedDraftSave разбирает закрытый ввод, проверяет форматы по виду и допустимость области
// промпта, разрешая пустой, но присутствующий UTF-8 текст до 256 КиБ без NUL. Затем проверяет ссылки и
// OCC; смысловая проверка публикации остаётся отдельной.
func readManagedDraftSave(w http.ResponseWriter, r *http.Request, configurationRef, revisionRef, key, etag string, kind controlplanev1.ManagedConfigurationKind) (generated.ManagedConfigurationDraftSaveInput, *controlplanev1.MutationContext, bool) {
	body, ok := decodeJSON[generated.ManagedConfigurationDraftSaveInput](w, r)
	if !ok {
		return body, nil, false
	}
	validFormat := body.ContentFormat == "JSON" || body.ContentFormat == "YAML" || body.ContentFormat == "TOML"
	if body.PromptScope != nil && kind != controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return body, nil, false
	}
	if kind == controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE {
		validFormat = body.ContentFormat == "TEXT"
	}
	if kind == controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION {
		validFormat = validFormat || body.ContentFormat == "OPENAPI_IMPORT"
	}
	if !validFormat || body.Content == nil || len(*body.Content) > 256<<10 || !utf8.ValidString(*body.Content) || strings.ContainsRune(*body.Content, 0) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return body, nil, false
	}
	mutation, ok := managedDraftMutation(w, configurationRef, revisionRef, key, etag)
	return body, mutation, ok
}

// writeManagedDraftResult rejects a mismatched set/kind/UI owner/version or replaced published head,
// then checks either the exact discarded revision or a new child draft with expected normalized
// content/format/digest. Only then does shared response conversion emit success.
// writeManagedDraftResult отклоняет несоответствие набора, вида, UI-владельца, версии или заменённой
// опубликованной головы, затем проверяет точную удалённую ревизию либо новый дочерний черновик с
// ожидаемыми нормализованными текстом, форматом и хешем. Лишь после этого общее преобразование ответа
// выдаёт успех.
func writeManagedDraftResult(ctx context.Context, w http.ResponseWriter, result managedConfigurationResponse, configurationRef, revisionRef string, version int64, kind controlplanev1.ManagedConfigurationKind, saved *generated.ManagedConfigurationDraftSaveInput) {
	configuration, revision := result.GetConfiguration(), result.GetRevision()
	valid := configuration.GetRef() == configurationRef && configuration.GetVersion() == version+1 &&
		configuration.GetKind() == kind && configuration.GetManagedBy() == controlplanev1.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_UI
	if current := configuration.GetCurrentRevision(); current != nil {
		valid = valid && current.GetState() == controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_PUBLISHED && current.GetRef() != revisionRef
	}
	if saved == nil {
		valid = valid && revision.GetRef() == revisionRef && revision.GetState() == controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DISCARDED
	} else {
		content, format, err := expectedManagedDraftReceipt(ctx, kind, string(saved.ContentFormat), *saved.Content)
		if err != nil {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		digest := sha256.Sum256([]byte(content))
		valid = valid && opaqueHTTPReference.MatchString(revision.GetRef()) && revision.GetRef() != revisionRef &&
			revision.GetParentRevisionRef() == revisionRef && revision.GetState() == controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DRAFT &&
			revision.GetContentFormat() == format && revision.GetContent() == content && revision.GetDigest() == hex.EncodeToString(digest[:])
	}
	if !valid {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

// expectedManagedDraftReceipt trims source and predicts the owner's integration normalization: OpenAPI
// import errors propagate, while invalid ordinary JSON/YAML drafts retain trimmed source for editor
// diagnostics. Other kinds/formats are not parsed or admitted here.
// expectedManagedDraftReceipt обрезает внешние пробелы и рассчитывает нормализацию интеграции
// владельцем: ошибки OpenAPI-импорта возвращаются, а неверные обычные JSON/YAML черновики сохраняют
// обрезанный исходник для диагностики редактора. Другие виды и форматы здесь не разбираются и не
// допускаются.
func expectedManagedDraftReceipt(ctx context.Context, kind controlplanev1.ManagedConfigurationKind, format, content string) (string, string, error) {
	content = strings.TrimSpace(content)
	if kind != controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION {
		return content, format, nil
	}
	if format == "OPENAPI_IMPORT" {
		canonical, err := canonicalOpenAPIImport(ctx, content)
		return canonical, "JSON", err
	}
	if format != "JSON" && format != "YAML" {
		return content, format, nil
	}
	shipped, err := integrationpackage.LoadShipped()
	if err != nil {
		return "", "", err
	}
	_, canonical, err := integrationpackage.NormalizeManagedRevision([]byte(content), integrationpackage.OriginUI, shipped)
	if err != nil {
		// Keep invalid draft source unchanged for editor diagnostics before publication.
		// Сохраняем неверный черновик без канонизации для диагностики до публикации.
		return content, format, nil
	}
	return string(canonical), "JSON", nil
}

// canonicalOpenAPIImport converts the trimmed import payload into a managed integration definition and
// JSON-encodes it, propagating conversion/encoding failure rather than returning the raw OpenAPI
// source as a successful receipt.
// canonicalOpenAPIImport преобразует обрезанный ввод импорта в управляемое определение интеграции и
// кодирует JSON, возвращая ошибки преобразования или кодирования вместо успешного ответа с сырым
// OpenAPI.
func canonicalOpenAPIImport(ctx context.Context, content string) (string, error) {
	definition, err := integrationpackage.DraftOpenAPIPackageFromJSON(ctx, []byte(strings.TrimSpace(content)))
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
