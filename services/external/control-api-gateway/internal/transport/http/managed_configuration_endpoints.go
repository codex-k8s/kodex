package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// CreatePromptTemplateDraft decodes editable input, requires project scope, converts and checks prompt
// scope, maps draft mutation metadata, and attaches the project reference before the exact RPC. It
// verifies returned prompt-scope pins before the shared 201 response and ETag conversion; owner
// authorization is not inferred from body fields.
// CreatePromptTemplateDraft читает редактируемый вход, требует область проекта, преобразует и
// проверяет область промпта, переносит метаданные изменения и добавляет ссылку проекта перед точным
// RPC. Возвращённые привязки промпта сверяются до общего ответа201 и ETag; полномочия владельца не
// выводятся из полей тела.
func (server *Server) CreatePromptTemplateDraft(w http.ResponseWriter, r *http.Request, p generated.CreatePromptTemplateDraftParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationDraftInput](w, r)
	if !ok {
		return
	}
	if !requireManagedDraftProject(w, body, true) {
		return
	}
	scope, ok := promptScopeInput(body.PromptScope)
	if !ok {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireManagedDraftMutation(w, p.IdempotencyKey, stringValue(p.IfMatch), body, controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE)
	if !ok {
		return
	}
	r, ok = withProjectReference(w, r, *body.ProjectRef)
	if !ok {
		return
	}
	result, err := server.control.Command.CreatePromptTemplateDraft(r.Context(), &controlplanev1.CreatePromptTemplateDraftRequest{
		Mutation: mutation, ConfigurationRef: stringValue(body.ConfigurationRef), ProjectRef: stringValue(body.ProjectRef),
		Name: body.Name, ContentFormat: string(body.ContentFormat), Content: body.Content, PromptScope: scope,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if !validPromptScopeReceipt(scope, result.GetRevision().GetPromptScope()) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeManagedResult(w, http.StatusCreated, result)
}

// ValidatePromptTemplateDraft maps mutation metadata and exact configuration and revision path
// selectors into the prompt-validation RPC. RPC failures use the common problem boundary; a valid
// owner result is returned through checked managed-result conversion, not published here.
// ValidatePromptTemplateDraft переносит метаданные изменения и точные ссылки конфигурации и ревизии
// пути в RPC проверки промпта. Ошибки RPC проходят общую границу проблем; корректный результат
// владельца возвращается через проверенное преобразование без публикации.
func (server *Server) ValidatePromptTemplateDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.ValidatePromptTemplateDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.ValidatePromptTemplateDraft(r.Context(), &controlplanev1.ValidatePromptTemplateDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

// PublishPromptTemplateDraft validates the explicit impact selection, invokes only prompt publication,
// and checks the applied plan's kind, identity, version, source version, selection count, published
// revision, and target digest against the request and returned configuration. Mismatches fail with 502
// before ETag or response publication.
// PublishPromptTemplateDraft проверяет явный выбор влияния, вызывает только публикацию промпта и
// сверяет вид, личность, версию, исходную версию, число выбранных элементов, опубликованную ревизию и
// целевой дайджест плана с запросом и конфигурацией. Несовпадение даёт502 до выдачи ETag и ответа.
func (server *Server) PublishPromptTemplateDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.PublishPromptTemplateDraftParams) {
	body, ok := decodeJSON[generated.RevisionImpactPublicationInput](w, r)
	if !ok {
		return
	}
	if !validRevisionImpactSelection(body.PlanRef, body.SelectedItemRefs) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.PublishPromptTemplateDraft(r.Context(), &controlplanev1.PublishPromptTemplateDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef, PlanRef: body.PlanRef, SelectedItemRefs: body.SelectedItemRefs,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	plan, ok := revisionImpactPlanView(result.GetPlan())
	configuration, configErr := managedConfigurationView(result.GetConfiguration())
	revision, revisionErr := managedRevisionForConfiguration(result.GetRevision(), result.GetConfiguration())
	if !ok || configErr != nil || revisionErr != nil || plan.Kind != "PROMPT_TEMPLATE" || plan.Ref != body.PlanRef || stringValue(plan.SourceRef) != configurationRef || plan.SourceVersion != mutation.GetExpectedVersion() || plan.DraftRef != revisionRef || plan.State != "APPLIED" || plan.Version != 2 || int64(len(body.SelectedItemRefs)) > plan.Total || configuration.Ref != configurationRef || configuration.Version <= plan.SourceVersion || revision.Ref != revisionRef || revision.State != "PUBLISHED" || revision.Digest != plan.TargetDigest {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", configuration.Version))
	writeJSON(w, http.StatusOK, generated.PromptTemplatePublicationResult{Configuration: configuration, Revision: revision, Plan: plan})
}

// RebindPromptTemplateConsumers reads the shared validated consumer and impact input and mapped
// mutation metadata, then retains the exact prompt-specific RPC and path selectors. Owner failures
// remain mapped through writeRPCProblem, and the typed result is checked by writeManagedResult.
// RebindPromptTemplateConsumers читает общий проверенный выбор потребителей и влияния и
// преобразованные метаданные изменения, сохраняя точный RPC промпта и ссылки пути. Ошибки владельца
// проходят writeRPCProblem, типизированный результат проверяет writeManagedResult.
func (server *Server) RebindPromptTemplateConsumers(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.RebindPromptTemplateConsumersParams) {
	input, ok := readManagedRebindRequest(w, r, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.RebindPromptTemplateConsumers(r.Context(), &controlplanev1.RebindPromptTemplateConsumersRequest{
		Mutation: input.mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
		ImpactDigest: input.impactDigest, Consumers: input.consumers,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

// CreateRoleImageRevisionDraft requires a project-scoped draft, maps its kind-specific mutation
// metadata, and attaches the project reference to the request context before the exact image RPC. The
// checked 201 response represents a draft, not a built or admitted image.
// CreateRoleImageRevisionDraft требует проектный черновик, переносит метаданные изменения
// соответствующего вида и добавляет ссылку проекта в контекст перед точным RPC образа. Проверенный
// ответ201 представляет черновик, а не собранный или допущенный образ.
func (server *Server) CreateRoleImageRevisionDraft(w http.ResponseWriter, r *http.Request, p generated.CreateRoleImageRevisionDraftParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationDraftInput](w, r)
	if !ok {
		return
	}
	if !requireManagedDraftProject(w, body, true) {
		return
	}
	mutation, ok := requireManagedDraftMutation(w, p.IdempotencyKey, stringValue(p.IfMatch), body, controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE)
	if !ok {
		return
	}
	r, ok = withProjectReference(w, r, *body.ProjectRef)
	if !ok {
		return
	}
	result, err := server.control.Command.CreateRoleImageRevisionDraft(r.Context(), &controlplanev1.CreateRoleImageRevisionDraftRequest{
		Mutation: mutation, ConfigurationRef: stringValue(body.ConfigurationRef), ProjectRef: stringValue(body.ProjectRef),
		Name: body.Name, ContentFormat: string(body.ContentFormat), Content: body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusCreated, result)
}

// ValidateRoleImageRevisionDraft dispatches the exact image-revision validation with mapped mutation
// metadata and path references. It uses the shared error and managed-result boundaries without
// treating recipe validation as image admission or promotion.
// ValidateRoleImageRevisionDraft направляет точную проверку ревизии образа с преобразованными
// метаданными изменения и ссылками пути. Общие границы ошибок и результата не приравнивают проверку
// рецепта к допуску или публикации образа.
func (server *Server) ValidateRoleImageRevisionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.ValidateRoleImageRevisionDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.ValidateRoleImageRevisionDraft(r.Context(), &controlplanev1.ValidateRoleImageRevisionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

// PublishRoleImageRevisionDraft routes only the selected managed image revision to its typed
// publication RPC and checked 200 result. This configuration transition remains distinct from image
// build, supply-chain admission, and promotion.
// PublishRoleImageRevisionDraft направляет только выбранную управляемую ревизию образа в
// типизированный RPC публикации и проверенный ответ200. Переход конфигурации остаётся отдельным от
// сборки, допуска цепочки поставки и публикации образа.
func (server *Server) PublishRoleImageRevisionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.PublishRoleImageRevisionDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.PublishRoleImageRevisionDraft(r.Context(), &controlplanev1.PublishRoleImageRevisionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

// RebindRoleImageConsumers validates exact references, digest, and a present bounded unique
// impact-item selection before the image RPC. It checks the returned applied image plan, expected
// configuration version, published target revision and digest, then emits the typed plan result and
// new ETag. Its plan-based input is not the generic consumer-list format.
// RebindRoleImageConsumers проверяет точные ссылки, дайджест и присутствующий ограниченный уникальный
// выбор элементов влияния перед RPC образа. Сверяются применённый план, ожидаемая версия конфигурации,
// опубликованная целевая ревизия и дайджест; затем выдаются типизированный план и новый ETag. Вход на
// основе плана отличается от общего списка потребителей.
func (server *Server) RebindRoleImageConsumers(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.RebindRoleImageConsumersParams) {
	body, ok := decodeJSON[generated.RoleImageRebindInput](w, r)
	if !ok {
		return
	}
	if !fileTargetRef(configurationRef) || !fileTargetRef(revisionRef) || !fileTargetRef(body.PlanRef) || !validManagedDigest(body.ImpactDigest) || body.SelectedItemRefs == nil || len(body.SelectedItemRefs) > 1000 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	seen := map[string]bool{}
	for _, ref := range body.SelectedItemRefs {
		if !fileTargetRef(ref) || seen[ref] {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		seen[ref] = true
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.RebindRoleImageConsumers(r.Context(), &controlplanev1.RebindRoleImageConsumersRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
		ImpactDigest: body.ImpactDigest, PlanRef: body.PlanRef, SelectedItemRefs: body.SelectedItemRefs,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	plan, valid := roleImageImpactPlanView(result.GetPlan())
	configuration, configurationErr := managedConfigurationView(result.GetConfiguration())
	revision, revisionErr := managedRevisionForConfiguration(result.GetRevision(), result.GetConfiguration())
	if !valid || configurationErr != nil || revisionErr != nil || plan.Ref != body.PlanRef || plan.Digest != body.ImpactDigest || plan.State != "APPLIED" || plan.ConfigurationRef != configurationRef || plan.RevisionRef != revisionRef || plan.ConfigurationVersion != mutation.GetExpectedVersion() || configuration.Ref != configurationRef || configuration.Version <= plan.ConfigurationVersion || revision.Ref != revisionRef || revision.Digest != plan.RevisionDigest || configuration.Kind != "ROLE_IMAGE" || revision.State != "PUBLISHED" {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", configuration.Version))
	writeJSON(w, http.StatusOK, generated.RoleImageRebindResult{Configuration: configuration, Revision: revision, Plan: plan})
}

// CreateIntegrationDefinitionDraft requires organization scope without a project field and invokes the
// exact integration-definition create RPC. For OPENAPI_IMPORT it recomputes canonical content and
// SHA-256 and checks the returned UI-owned definition and JSON revision before returning 201; it does
// not fetch arbitrary external references here.
// CreateIntegrationDefinitionDraft требует общую область без поля проекта и вызывает точный RPC
// создания определения интеграции. Для OPENAPI_IMPORT пересчитываются каноническое содержимое и
// SHA-256, проверяются определение владельца UI и JSON-ревизия до ответа201; произвольные внешние
// ссылки здесь не загружаются.
func (server *Server) CreateIntegrationDefinitionDraft(w http.ResponseWriter, r *http.Request, p generated.CreateIntegrationDefinitionDraftParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationDraftInput](w, r)
	if !ok {
		return
	}
	if !requireManagedDraftProject(w, body, false) {
		return
	}
	mutation, ok := requireManagedDraftMutation(w, p.IdempotencyKey, stringValue(p.IfMatch), body, controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION)
	if !ok {
		return
	}
	result, err := server.control.Command.CreateIntegrationDefinitionDraft(r.Context(), &controlplanev1.CreateIntegrationDefinitionDraftRequest{
		Mutation: mutation, ConfigurationRef: stringValue(body.ConfigurationRef), ProjectRef: stringValue(body.ProjectRef),
		Name: body.Name, ContentFormat: string(body.ContentFormat), Content: body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if body.ContentFormat == "OPENAPI_IMPORT" {
		canonical, err := canonicalOpenAPIImport(r.Context(), body.Content)
		digest := sha256.Sum256([]byte(canonical))
		if err != nil || result.GetConfiguration().GetKind() != controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION ||
			result.GetConfiguration().GetManagedBy() != controlplanev1.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_UI ||
			result.GetRevision().GetContentFormat() != "JSON" || result.GetRevision().GetContent() != canonical ||
			result.GetRevision().GetDigest() != hex.EncodeToString(digest[:]) {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
	}
	writeManagedResult(w, http.StatusCreated, result)
}

// ValidateIntegrationDefinitionDraft maps mutation metadata and path selectors into the exact
// integration-definition validation command. It returns only the checked managed result and does not
// create connection grants or execute provider operations.
// ValidateIntegrationDefinitionDraft переносит метаданные изменения и ссылки пути в точную команду
// проверки определения интеграции. Возвращается только проверенный управляемый результат без создания
// разрешений подключений или операций провайдера.
func (server *Server) ValidateIntegrationDefinitionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.ValidateIntegrationDefinitionDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.ValidateIntegrationDefinitionDraft(r.Context(), &controlplanev1.ValidateIntegrationDefinitionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

// PublishIntegrationDefinitionDraft keeps the integration-specific publication RPC separate from other
// configuration kinds while using common mutation parsing, error mapping, and checked response
// conversion. Publication is not capability assignment to an agent.
// PublishIntegrationDefinitionDraft сохраняет отдельный RPC публикации интеграции с общими разбором
// изменения, преобразованием ошибок и проверенным ответом. Публикация не является назначением
// возможности сотруднику.
func (server *Server) PublishIntegrationDefinitionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.PublishIntegrationDefinitionDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.PublishIntegrationDefinitionDraft(r.Context(), &controlplanev1.PublishIntegrationDefinitionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

// RebindIntegrationDefinitionConsumers reuses only transport preparation of impact, consumers, and
// mutation metadata, then sends the unchanged integration-specific typed request. Resource authority
// and the current impact snapshot are still verified by the owner.
// RebindIntegrationDefinitionConsumers переиспользует только подготовку транспорта для влияния,
// потребителей и метаданных изменения, отправляя прежний типизированный запрос интеграции. Полномочия
// ресурса и текущий снимок влияния по-прежнему проверяет владелец.
func (server *Server) RebindIntegrationDefinitionConsumers(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.RebindIntegrationDefinitionConsumersParams) {
	input, ok := readManagedRebindRequest(w, r, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.RebindIntegrationDefinitionConsumers(r.Context(), &controlplanev1.RebindIntegrationDefinitionConsumersRequest{
		Mutation: input.mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
		ImpactDigest: input.impactDigest, Consumers: input.consumers,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

// CreateSystemSTTConfigurationDraft requires organization scope, maps STT-specific draft mutation
// metadata, and sends the exact configuration input to the STT create RPC. The checked 201 result does
// not start recording or transcription.
// CreateSystemSTTConfigurationDraft требует общую область, переносит метаданные изменения черновика
// STT и отправляет точный вход в RPC создания STT. Проверенный ответ201 не запускает запись или
// распознавание.
func (server *Server) CreateSystemSTTConfigurationDraft(w http.ResponseWriter, r *http.Request, p generated.CreateSystemSTTConfigurationDraftParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationDraftInput](w, r)
	if !ok {
		return
	}
	if !requireManagedDraftProject(w, body, false) {
		return
	}
	mutation, ok := requireManagedDraftMutation(w, p.IdempotencyKey, stringValue(p.IfMatch), body, controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT)
	if !ok {
		return
	}
	result, err := server.control.Command.CreateSystemSTTConfigurationDraft(r.Context(), &controlplanev1.CreateSystemSTTConfigurationDraftRequest{
		Mutation: mutation, ConfigurationRef: stringValue(body.ConfigurationRef), ProjectRef: stringValue(body.ProjectRef),
		Name: body.Name, ContentFormat: string(body.ContentFormat), Content: body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusCreated, result)
}

// ValidateSystemSTTConfigurationDraft dispatches validation of the selected system-STT revision with
// mapped mutation metadata and checked result conversion. It does not exercise a real provider or
// prove audio-path readiness.
// ValidateSystemSTTConfigurationDraft направляет проверку выбранной системной ревизии STT с переносом
// метаданных изменения и проверкой результата. Реальный провайдер не вызывается, готовность пути аудио
// не доказывается.
func (server *Server) ValidateSystemSTTConfigurationDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.ValidateSystemSTTConfigurationDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.ValidateSystemSTTConfigurationDraft(r.Context(), &controlplanev1.ValidateSystemSTTConfigurationDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

// PublishSystemSTTConfigurationDraft routes the exact STT publication command and returns the checked
// configuration and revision. It does not reuse an image or integration publication method or bypass
// owner readiness and credential checks.
// PublishSystemSTTConfigurationDraft направляет точную команду публикации STT и возвращает проверенные
// конфигурацию и ревизию. Методы образа или интеграции не подменяются, проверки готовности и
// реквизитов владельца не обходятся.
func (server *Server) PublishSystemSTTConfigurationDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.PublishSystemSTTConfigurationDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.PublishSystemSTTConfigurationDraft(r.Context(), &controlplanev1.PublishSystemSTTConfigurationDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

// RebindSystemSTTConsumers shares only JSON/consumer preparation and mutation parsing before the exact
// STT rebind RPC. It retains the original path identity, owner-error response, and checked
// managed-result output.
// RebindSystemSTTConsumers разделяет только подготовку JSON и потребителей и разбор изменения перед
// точным RPC перепривязки STT. Сохраняются прежние ссылки пути, ответ ошибки владельца и проверенный
// управляемый результат.
func (server *Server) RebindSystemSTTConsumers(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.RebindSystemSTTConsumersParams) {
	input, ok := readManagedRebindRequest(w, r, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.RebindSystemSTTConsumers(r.Context(), &controlplanev1.RebindSystemSTTConsumersRequest{
		Mutation: input.mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
		ImpactDigest: input.impactDigest, Consumers: input.consumers,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

// DetachGitManagedConfiguration maps mutation metadata into the explicit Git-detach RPC, validates the
// returned aggregate shape, and writes the detachment response. A malformed upstream result fails with
// 502; this method does not silently update Git-owned content.
// DetachGitManagedConfiguration переносит метаданные изменения в явный RPC отключения Git, проверяет
// форму возвращённого агрегата и выдаёт ответ отключения. Неверный ответ upstream даёт502; метод не
// изменяет незаметно Git-содержимое.
func (server *Server) DetachGitManagedConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.DetachGitManagedConfigurationParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.DetachGitManagedConfiguration(r.Context(), &controlplanev1.DetachGitManagedConfigurationRequest{Mutation: mutation, ConfigurationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	configuration, err := managedConfigurationView(result.GetConfiguration())
	if err != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	writeJSON(w, http.StatusOK, generated.ManagedConfigurationDetachment{Configuration: configuration})
}

// CopyGitManagedConfiguration decodes a proposed copy name, maps mutation metadata, and invokes the
// exact copy command for the selected configuration. The shared 201 response verifies the returned
// configuration and revision rather than modifying source ownership locally.
// CopyGitManagedConfiguration читает предлагаемое имя копии, переносит метаданные изменения и вызывает
// точную команду копирования выбранной конфигурации. Общий ответ201 проверяет конфигурацию и ревизию,
// а не меняет локально владение исходником.
func (server *Server) CopyGitManagedConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.CopyGitManagedConfigurationParams) {
	body, mutation, ok := decodeMutationBody[generated.ManagedConfigurationCopyInput](w, r, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.CopyGitManagedConfiguration(r.Context(), &controlplanev1.CopyGitManagedConfigurationRequest{Mutation: mutation, ConfigurationRef: ref, Name: body.Name})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusCreated, result)
}

// ListManagedConfigurationHistory validates the reference and page, then checks the owner response's
// configuration identity, safe counts, fifty-row limit, bounded UTF-8 cursor, and unique revision
// references. Revision-to-configuration consistency is checked before emitting the ordered nonnil item
// list.
// ListManagedConfigurationHistory проверяет ссылку и страницу, затем личность конфигурации ответа
// владельца, безопасные количества, предел50 строк, ограниченный UTF-8 курсор и уникальность ревизий.
// Согласованность ревизии с конфигурацией проверяется до выдачи упорядоченного ненулевого списка.
func (server *Server) ListManagedConfigurationHistory(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.ListManagedConfigurationHistoryParams) {
	if !opaqueHTTPReference.MatchString(ref) || !validHTTPPage(p.PageSize, p.PageToken) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	result, err := server.control.Query.ListManagedConfigurationHistory(r.Context(), &controlplanev1.ListManagedConfigurationHistoryRequest{ConfigurationRef: ref, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	configuration, err := managedConfigurationView(result.GetConfiguration())
	if err != nil || configuration.Ref != ref || result.GetTotal() < int64(len(result.GetRevisions())) || result.GetTotal() > maximumSafeJSONInteger || len(result.GetRevisions()) > 50 || len(result.GetPage().GetNextPageToken()) > 512 || !utf8.ValidString(result.GetPage().GetNextPageToken()) {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	output := generated.ManagedConfigurationHistory{Configuration: configuration, Total: result.GetTotal(), Items: make([]generated.ManagedConfigurationRevision, 0, len(result.GetRevisions()))}
	seen := map[string]bool{}
	for _, revision := range result.GetRevisions() {
		item, err := managedRevisionForConfiguration(revision, result.GetConfiguration())
		if err != nil || seen[item.Ref] {
			writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
			return
		}
		seen[item.Ref] = true
		output.Items = append(output.Items, item)
	}
	output.NextPageToken = optionalManagedString(result.GetPage().GetNextPageToken())
	writeJSON(w, http.StatusOK, output)
}

// GetManagedConfigurationImpact validates exact references, page, and bounded search, then verifies
// the owner impact's identities, digest, page size, safe total, and cursor before converting
// consumers. The output is impact evidence, not approval to rebind those consumers.
// GetManagedConfigurationImpact проверяет точные ссылки, страницу и ограниченный поиск, затем
// личности, дайджест, размер страницы, безопасное количество и курсор влияния владельца до
// преобразования потребителей. Ответ является свидетельством влияния, а не подтверждением
// перепривязки.
func (server *Server) GetManagedConfigurationImpact(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.GetManagedConfigurationImpactParams) {
	if !opaqueHTTPReference.MatchString(ref) || !opaqueHTTPReference.MatchString(revisionRef) || !validHTTPPage(p.PageSize, p.PageToken) || !validSearchText(stringValue(p.Query), 0, 200) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	result, err := server.control.Query.GetManagedConfigurationImpact(r.Context(), &controlplanev1.GetManagedConfigurationImpactRequest{ConfigurationRef: ref, RevisionRef: revisionRef, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	impact := result.GetImpact()
	if impact == nil || impact.GetConfigurationRef() != ref || impact.GetTargetRevisionRef() != revisionRef || !validManagedDigest(impact.GetDigest()) || len(impact.GetConsumers()) > int(page(p.PageSize, p.PageToken).PageSize) ||
		impact.GetTotal() < int64(len(impact.GetConsumers())) || impact.GetTotal() > maximumSafeJSONInteger || len(impact.GetPage().GetNextPageToken()) > 512 || !utf8.ValidString(impact.GetPage().GetNextPageToken()) {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	output := generated.ManagedConfigurationImpact{ConfigurationRef: ref, TargetRevisionRef: revisionRef, Digest: impact.GetDigest(), Total: impact.GetTotal(), NextPageToken: optionalManagedString(impact.GetPage().GetNextPageToken()), Consumers: make([]generated.ManagedConfigurationConsumer, 0, len(impact.GetConsumers()))}
	for _, consumer := range impact.GetConsumers() {
		item, err := managedConsumerView(consumer)
		if err != nil {
			writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
			return
		}
		output.Consumers = append(output.Consumers, item)
	}
	writeJSON(w, http.StatusOK, output)
}

// GetSystemSTTConfiguration reads the owner projection and validates the specification, revision,
// safely representable credential generation, and exact platform.stt.use permission before returning
// readiness and limits. It copies blockers into a nonnil slice and does not expose credential bytes or
// perform transcription.
// GetSystemSTTConfiguration читает проекцию владельца и проверяет спецификацию, ревизию, безопасно
// представимое поколение реквизитов и точное разрешение platform.stt.use до выдачи готовности и
// лимитов. Препятствия копируются в ненулевой срез; байты реквизитов не раскрываются, распознавание не
// выполняется.
func (server *Server) GetSystemSTTConfiguration(w http.ResponseWriter, r *http.Request) {
	result, err := server.control.Query.GetSystemSTTConfiguration(r.Context(), &controlplanev1.GetSystemSTTConfigurationRequest{})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	value := result.GetConfiguration()
	specification, validSpecification := systemSTTSpecificationView(value)
	if value == nil || !validSpecification || !validManagedVersion(value.GetRevision()) || value.GetProviderCredentialGeneration() > uint64(maximumSafeJSONInteger) || value.GetPermissionKey() != "platform.stt.use" {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	writeJSON(w, http.StatusOK, generated.SystemSTTConfiguration{
		ConfigurationRef: value.GetConfigurationRef(), RevisionRef: value.GetRevisionRef(), Revision: value.GetRevision(), Digest: value.GetDigest(),
		ProviderAccountRef: value.GetProviderAccountRef(), ProviderCredentialGeneration: int64(value.GetProviderCredentialGeneration()),
		Model: value.GetModel(), Language: value.GetLanguage(), PermissionKey: generated.SystemSTTConfigurationPermissionKey(value.GetPermissionKey()),
		Ready: value.GetReady(), ReadinessBlockers: append([]string{}, value.GetReadinessBlockers()...),
		Enabled: specification.Enabled, Parameters: specification.Parameters, MaximumAudioBytes: specification.MaximumAudioBytes,
		MaximumAudioDurationMilliseconds: specification.MaximumAudioDurationMilliseconds, ProviderTimeoutMilliseconds: specification.ProviderTimeoutMilliseconds,
	})
}
