package grpc

import (
	"encoding/json"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// maximumAssistantPlanOperationTitleRunes caps a displayed assistant operation title at 160 Unicode code points, including the final ellipsis; it does not truncate the persisted operation summary.
// maximumAssistantPlanOperationTitleRunes ограничивает отображаемое название операции помощника 160 кодовыми точками Unicode, включая завершающее многоточие; сохранённый текст операции не обрезает.
const maximumAssistantPlanOperationTitleRunes = 160

// timestamp maps a zero Go time to absent protobuf presence and otherwise constructs a Timestamp without validating its range.
// timestamp преобразует нулевое время Go в отсутствующее protobuf-поле, иначе создаёт Timestamp без проверки допустимого диапазона.
func timestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

// optionalTimestamp preserves nil timestamp presence and delegates non-nil values to timestamp, so a pointer to zero time is also absent.
// optionalTimestamp сохраняет отсутствие времени при nil, а непустой указатель передаёт timestamp, поэтому указатель на нулевое время также даёт отсутствие поля.
func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamp(*value)
}

// structure converts a dynamic map to protobuf Struct, ignoring conversion errors and returning an empty Struct when conversion yields nil; callers must validate sensitive payloads before projection.
// structure преобразует динамическую карту в protobuf Struct, игнорируя ошибки преобразования и возвращая пустой Struct при nil результате; чувствительные данные вызывающий код проверяет до проекции.
func structure(value map[string]any) *structpb.Struct {
	result, _ := structpb.NewStruct(value)
	if result == nil {
		result = &structpb.Struct{}
	}
	return result
}

// gatePreviewStructure normalizes typed gate-preview values through JSON before protobuf conversion and returns nil on any encoding/decoding/conversion error; it does not authorize the previewed effect.
// gatePreviewStructure нормализует типизированные значения предпросмотра шлюза через JSON перед преобразованием в protobuf и возвращает nil при любой ошибке кодирования, разбора или преобразования; показанное действие не разрешает.
func gatePreviewStructure(value map[string]any) *structpb.Struct {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var normalized map[string]any
	if json.Unmarshal(encoded, &normalized) != nil {
		return nil
	}
	result, err := structpb.NewStruct(normalized)
	if err != nil {
		return nil
	}
	return result
}

// nextActions projects recognized NEXT_ACTION-prefixed domain commands in input order, omitting unknown values while retaining duplicates and a non-nil empty result.
// nextActions переводит распознанные доменные команды с префиксом NEXT_ACTION в исходном порядке, пропуская неизвестные и сохраняя повторы и пустой срез вместо nil.
func nextActions(values []string) []controlplanev1.NextAction {
	result := make([]controlplanev1.NextAction, 0, len(values))
	for _, value := range values {
		if action, ok := controlplanev1.NextAction_value["NEXT_ACTION_"+value]; ok {
			result = append(result, controlplanev1.NextAction(action))
		}
	}
	return result
}

// platformRole looks up a PLATFORM_ROLE-prefixed name and returns the zero enum for unknown input; transport projection is not a role grant.
// platformRole ищет имя с префиксом PLATFORM_ROLE и возвращает нулевой enum для неизвестного ввода; транспортная проекция роль не выдаёт.
func platformRole(value string) controlplanev1.PlatformRole {
	raw := controlplanev1.PlatformRole_value["PLATFORM_ROLE_"+value]
	return controlplanev1.PlatformRole(raw)
}

// projectPermissions projects only recognized PROJECT_PERMISSION-prefixed values in order, retaining duplicates and a non-nil empty result rather than inventing permissions.
// projectPermissions переводит лишь известные значения PROJECT_PERMISSION в исходном порядке, сохраняя повторы и пустой срез вместо nil, не добавляя полномочий.
func projectPermissions(values []string) []controlplanev1.ProjectPermission {
	result := make([]controlplanev1.ProjectPermission, 0, len(values))
	for _, value := range values {
		if raw, ok := controlplanev1.ProjectPermission_value["PROJECT_PERMISSION_"+value]; ok {
			result = append(result, controlplanev1.ProjectPermission(raw))
		}
	}
	return result
}

// lifecycle maps the exact ENTITY_LIFECYCLE-prefixed domain state to its transport enum, with zero fallback and no lifecycle transition.
// lifecycle переводит точное доменное состояние с префиксом ENTITY_LIFECYCLE в транспортный enum с нулевым запасным значением, не выполняя переход жизненного цикла.
func lifecycle(value string) controlplanev1.EntityLifecycle {
	raw := controlplanev1.EntityLifecycle_value["ENTITY_LIFECYCLE_"+value]
	return controlplanev1.EntityLifecycle(raw)
}

// agentState maps the exact AGENT_STATE-prefixed state, returning zero for unknown input without deriving agent readiness.
// agentState переводит точное состояние с префиксом AGENT_STATE, возвращая ноль для неизвестного ввода без вычисления готовности сотрудника.
func agentState(value string) controlplanev1.AgentState {
	raw := controlplanev1.AgentState_value["AGENT_STATE_"+value]
	return controlplanev1.AgentState(raw)
}

// instructionState maps instruction publication/validation state through the INSTRUCTION_STATE enum table, with zero fallback rather than implicit publication.
// instructionState переводит состояние публикации или валидации инструкции через таблицу INSTRUCTION_STATE с нулевым запасным значением, не публикуя инструкцию неявно.
func instructionState(value string) controlplanev1.InstructionState {
	raw := controlplanev1.InstructionState_value["INSTRUCTION_STATE_"+value]
	return controlplanev1.InstructionState(raw)
}

// workflowState maps the exact WORKFLOW_STATE-prefixed lifecycle name with zero fallback; it does not validate or publish a workflow.
// workflowState переводит точное имя состояния WORKFLOW_STATE с нулевым запасным значением; процесс не валидирует и не публикует.
func workflowState(value string) controlplanev1.WorkflowState {
	raw := controlplanev1.WorkflowState_value["WORKFLOW_STATE_"+value]
	return controlplanev1.WorkflowState(raw)
}

// runState projects a persisted run-state name through RUN_STATE, returning zero for unknown values rather than deciding execution outcome.
// runState отображает сохранённое состояние запуска через RUN_STATE, возвращая ноль для неизвестного значения, а не определяя результат исполнения.
func runState(value string) controlplanev1.RunState {
	raw := controlplanev1.RunState_value["RUN_STATE_"+value]
	return controlplanev1.RunState(raw)
}

// runSource maps an exact RUN_SOURCE-prefixed origin name with zero fallback; this representation does not prove trusted execution lineage.
// runSource переводит точное происхождение с префиксом RUN_SOURCE с нулевым запасным значением; такое представление не доказывает доверенное происхождение исполнения.
func runSource(value string) controlplanev1.RunSource {
	raw := controlplanev1.RunSource_value["RUN_SOURCE_"+value]
	return controlplanev1.RunSource(raw)
}

// nodeType maps an exact RUN_NODE_TYPE-prefixed kind to its enum, with zero fallback and no graph-node creation.
// nodeType переводит точный вид RUN_NODE_TYPE в enum с нулевым запасным значением, не создавая узел графа.
func nodeType(value string) controlplanev1.RunNodeType {
	raw := controlplanev1.RunNodeType_value["RUN_NODE_TYPE_"+value]
	return controlplanev1.RunNodeType(raw)
}

// nodeState projects exact RUN_NODE_STATE names with zero fallback, leaving transition eligibility to the domain owner.
// nodeState отображает точные имена RUN_NODE_STATE с нулевым запасным значением, оставляя допустимость перехода доменному владельцу.
func nodeState(value string) controlplanev1.RunNodeState {
	raw := controlplanev1.RunNodeState_value["RUN_NODE_STATE_"+value]
	return controlplanev1.RunNodeState(raw)
}

// edgeType maps an exact RUN_EDGE_TYPE-prefixed relation with zero fallback; it does not create or authorize a delegation or dependency edge.
// edgeType переводит точную связь RUN_EDGE_TYPE с нулевым запасным значением; ребро делегирования или зависимости не создаёт и не разрешает.
func edgeType(value string) controlplanev1.RunEdgeType {
	raw := controlplanev1.RunEdgeType_value["RUN_EDGE_TYPE_"+value]
	return controlplanev1.RunEdgeType(raw)
}

// eventType maps a persisted RUN_EVENT_TYPE-prefixed event name to transport with zero fallback, without generating a new event.
// eventType переводит сохранённое имя RUN_EVENT_TYPE в транспортный enum с нулевым запасным значением, не создавая нового события.
func eventType(value string) controlplanev1.RunEventType {
	raw := controlplanev1.RunEventType_value["RUN_EVENT_TYPE_"+value]
	return controlplanev1.RunEventType(raw)
}

// gateState projects the OWNER_GATE_STATE name with zero fallback, without resolving or approving the owner gate.
// gateState отображает имя OWNER_GATE_STATE с нулевым запасным значением, не закрывая и не одобряя шлюз владельца.
func gateState(value string) controlplanev1.OwnerGateState {
	raw := controlplanev1.OwnerGateState_value["OWNER_GATE_STATE_"+value]
	return controlplanev1.OwnerGateState(raw)
}

// gateDecision maps an OWNER_GATE_DECISION-prefixed value with zero fallback; representing a decision does not record human approval.
// gateDecision переводит значение OWNER_GATE_DECISION с нулевым запасным значением; представление решения не фиксирует человеческое одобрение.
func gateDecision(value string) controlplanev1.OwnerGateDecision {
	raw := controlplanev1.OwnerGateDecision_value["OWNER_GATE_DECISION_"+value]
	return controlplanev1.OwnerGateDecision(raw)
}

// gateDecisions projects every supplied gate decision in order, retaining unknown values as zero enums rather than filtering them out.
// gateDecisions отображает все переданные решения шлюза в исходном порядке, сохраняя неизвестные как нулевые enum, а не отбрасывая их.
func gateDecisions(values []string) []controlplanev1.OwnerGateDecision {
	result := make([]controlplanev1.OwnerGateDecision, 0, len(values))
	for _, value := range values {
		result = append(result, gateDecision(value))
	}
	return result
}

// scanState maps stored ARTIFACT_SCAN_STATE metadata with zero fallback; it neither scans bytes nor establishes a CLEAN result.
// scanState переводит сохранённые метаданные ARTIFACT_SCAN_STATE с нулевым запасным значением; байты не сканирует и результат CLEAN не устанавливает.
func scanState(value string) controlplanev1.ArtifactScanState {
	raw := controlplanev1.ArtifactScanState_value["ARTIFACT_SCAN_STATE_"+value]
	return controlplanev1.ArtifactScanState(raw)
}

// artifactSource maps exact ARTIFACT_SOURCE metadata with zero fallback, without establishing artifact provenance or access eligibility.
// artifactSource переводит точные метаданные ARTIFACT_SOURCE с нулевым запасным значением, не устанавливая происхождение файла или право доступа.
func artifactSource(value string) controlplanev1.ArtifactSource {
	raw := controlplanev1.ArtifactSource_value["ARTIFACT_SOURCE_"+value]
	return controlplanev1.ArtifactSource(raw)
}

// artifactLifecycleState treats empty artifact lifecycle metadata as ACTIVE, maps other exact lifecycle names and leaves unknown names as the zero enum; no row is restored.
// artifactLifecycleState считает пустое состояние файла ACTIVE, переводит остальные точные имена жизненного цикла и оставляет неизвестные нулевым enum; строку не восстанавливает.
func artifactLifecycleState(value string) controlplanev1.ArtifactLifecycleState {
	if value == "" {
		value = "ACTIVE"
	}
	raw := controlplanev1.ArtifactLifecycleState_value["ARTIFACT_LIFECYCLE_STATE_"+value]
	return controlplanev1.ArtifactLifecycleState(raw)
}

// attachmentSetState maps the exact ATTACHMENT_SET_STATE name with zero fallback; it does not finalize or validate the attachment manifest.
// attachmentSetState переводит точное имя ATTACHMENT_SET_STATE с нулевым запасным значением; манифест вложений не финализирует и не валидирует.
func attachmentSetState(value string) controlplanev1.AttachmentSetState {
	return controlplanev1.AttachmentSetState(controlplanev1.AttachmentSetState_value["ATTACHMENT_SET_STATE_"+value])
}

// attachmentSetPurpose maps the ATTACHMENT_SET_PURPOSE name with zero fallback, without granting access to another attachment purpose.
// attachmentSetPurpose переводит имя ATTACHMENT_SET_PURPOSE с нулевым запасным значением, не выдавая доступа к другому назначению вложений.
func attachmentSetPurpose(value string) controlplanev1.AttachmentSetPurpose {
	return controlplanev1.AttachmentSetPurpose(controlplanev1.AttachmentSetPurpose_value["ATTACHMENT_SET_PURPOSE_"+value])
}

// scheduleState preserves ARCHIVED/NEEDS_ATTENTION/DELETED, derives ACTIVE versus PAUSED from enabled only for empty/ACTIVE lifecycle, and rejects other names to UNSPECIFIED.
// scheduleState сохраняет ARCHIVED/NEEDS_ATTENTION/DELETED, выводит ACTIVE либо PAUSED из enabled только для пустого или ACTIVE состояния, а остальные имена переводит в UNSPECIFIED.
func scheduleState(schedule entity.Schedule) controlplanev1.ScheduleState {
	switch schedule.State {
	case "ARCHIVED":
		return controlplanev1.ScheduleState_SCHEDULE_STATE_ARCHIVED
	case "NEEDS_ATTENTION":
		return controlplanev1.ScheduleState_SCHEDULE_STATE_NEEDS_ATTENTION
	case "DELETED":
		return controlplanev1.ScheduleState_SCHEDULE_STATE_DELETED
	case "", "ACTIVE":
	default:
		return controlplanev1.ScheduleState_SCHEDULE_STATE_UNSPECIFIED
	}
	if schedule.Enabled {
		return controlplanev1.ScheduleState_SCHEDULE_STATE_ACTIVE
	}
	return controlplanev1.ScheduleState_SCHEDULE_STATE_PAUSED
}

// connectionState looks up the exact CONNECTION_STATE-prefixed value and explicitly uses UNSPECIFIED when absent, without probing connection health.
// connectionState ищет точное значение CONNECTION_STATE и явно возвращает UNSPECIFIED при его отсутствии, не проверяя работоспособность подключения.
func connectionState(value string) controlplanev1.ConnectionState {
	raw, exists := controlplanev1.ConnectionState_value["CONNECTION_STATE_"+value]
	if !exists {
		return controlplanev1.ConnectionState_CONNECTION_STATE_UNSPECIFIED
	}
	return controlplanev1.ConnectionState(raw)
}

// assistantState translates STARTING to PROVISIONING and UNAVAILABLE to FAILED while preserving READY/BUSY/RECOVERING; unknown runtime metadata yields zero rather than a readiness claim.
// assistantState переводит STARTING в PROVISIONING и UNAVAILABLE в FAILED, сохраняя READY/BUSY/RECOVERING; неизвестные метаданные runtime дают ноль, а не утверждение готовности.
func assistantState(value string) controlplanev1.AssistantRuntimeState {
	mapping := map[string]string{"STARTING": "PROVISIONING", "READY": "READY", "BUSY": "BUSY", "RECOVERING": "RECOVERING", "UNAVAILABLE": "FAILED"}
	raw := controlplanev1.AssistantRuntimeState_value["ASSISTANT_RUNTIME_STATE_"+mapping[value]]
	return controlplanev1.AssistantRuntimeState(raw)
}

// castUser projects the authorized user reference, display name and already masked email hint; it does not perform email masking or actor resolution.
// castUser отображает ссылку авторизованного пользователя, имя и уже скрытую подсказку почты; сам почту не маскирует и личность автора не разрешает.
func castUser(value entity.User) *controlplanev1.UserSummary {
	return &controlplanev1.UserSummary{Ref: value.Ref, DisplayName: value.DisplayName, EmailHint: value.EmailMasked}
}

// castMembership projects exact project scope, active/version metadata, role, recognized permissions and next actions from an authorized membership without widening scope.
// castMembership отображает точную проектную область, активность, версию, роль, известные разрешения и действия авторизованного членства без расширения области.
func castMembership(value entity.Membership) *controlplanev1.Membership {
	return &controlplanev1.Membership{Ref: value.Ref, Version: value.Version, User: castUser(value.User), PlatformRole: platformRole(value.Role), ProjectPermissions: projectPermissions(value.Permissions), Active: value.Active, NextActions: nextActions(value.NextActions), ProjectRef: value.ProjectRef}
}

// castProject projects project lifecycle, counters, actions and optional activity/deletion/retention times; absent activity stays absent and readiness/eligibility is not recomputed.
// castProject отображает жизненный цикл проекта, счётчики, действия и необязательные времена активности, удаления и хранения; отсутствие активности сохраняет, готовность и доступность не пересчитывает.
func castProject(value entity.Project) *controlplanev1.Project {
	return &controlplanev1.Project{Ref: value.Ref, Version: value.Version, Name: value.Name, Purpose: value.Purpose, Language: value.Language, Lifecycle: lifecycle(value.Lifecycle), AgentCount: value.AgentCount, WorkflowCount: value.WorkflowCount, ActiveRunCount: value.ActiveRunCount, PendingGateCount: value.PendingGateCount, CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt), NextActions: nextActions(value.NextActions), LastActivityAt: optionalTimestamp(value.LastActivityAt), IntegrationState: value.IntegrationState, DeletedAt: optionalTimestamp(value.DeletedAt), PurgeAfter: optionalTimestamp(value.PurgeAfter)}
}

// castSearchResult projects search kind/reference/project/title/state/time with zero enum fallback; result eligibility must already be enforced by the search owner.
// castSearchResult отображает вид поиска, ссылку, проект, название, состояние и время с нулевым запасным enum; допустимость результата уже должен проверить владелец поиска.
func castSearchResult(value entity.SearchResult) *controlplanev1.SearchResult {
	kind := controlplanev1.SearchResultKind(controlplanev1.SearchResultKind_value["SEARCH_RESULT_KIND_"+value.Kind])
	return &controlplanev1.SearchResult{Kind: kind, Ref: value.Ref, ProjectRef: value.ProjectRef, Title: value.Title, Subtitle: value.Subtitle, State: value.State, UpdatedAt: timestamp(value.UpdatedAt)}
}

// castCapability projects capability catalog text with risk as category and derives integration-free display availability only from the platform. key prefix; that flag is not an execution grant.
// castCapability отображает текст каталога возможностей с риском как категорией и признаком работы без интеграции лишь по префиксу platform.; этот признак не выдаёт право исполнения.
func castCapability(value entity.IntegrationCapability) *controlplanev1.PlatformCapability {
	return &controlplanev1.PlatformCapability{Key: value.Key, Name: value.Name, Description: value.Description, Category: value.Risk, AvailableWithoutIntegration: strings.HasPrefix(value.Key, "platform.")}
}

// castRuntime projects the supplied runtime identity/revision/provider/model/readiness unchanged, rather than probing a runtime or choosing a model.
// castRuntime отображает переданные идентификатор, ревизию, провайдера, модель и готовность runtime без проверки runtime или выбора модели.
func castRuntime(value entity.RuntimeSelection) *controlplanev1.RuntimeSelection {
	return &controlplanev1.RuntimeSelection{Ref: value.Ref, Name: value.Name, Revision: value.RuntimeRevision, Ready: value.Ready, Provider: value.Provider, Model: value.Model}
}

// castProviderPolicy projects immutable account-policy metadata and ordered weighted account/catalog candidates, preserving pins without selecting an account.
// castProviderPolicy отображает метаданные неизменяемой политики аккаунтов и упорядоченных кандидатов с весами и привязками каталога, не выбирая аккаунт.
func castProviderPolicy(value entity.ProviderAccountPolicyVersion) *controlplanev1.ProviderAccountPolicyVersion {
	result := &controlplanev1.ProviderAccountPolicyVersion{Ref: value.Ref, Version: value.Version, Mode: value.Mode,
		Digest: value.Digest, CreatedAt: timestamp(value.CreatedAt)}
	for _, candidate := range value.AccountCandidates {
		result.AccountCandidates = append(result.AccountCandidates, &controlplanev1.ProviderAccountCandidate{AccountRef: candidate.AccountRef, Weight: candidate.Weight,
			CatalogRevision: candidate.CatalogRevision, CatalogDigest: candidate.CatalogDigest,
			ProviderDefinitionKey: candidate.ProviderDefinitionKey, DefaultReasoningEffort: candidate.DefaultReasoningEffort})
	}
	return result
}

// castAgentRuntimeConfiguration projects versioned agent runtime/model selection and nested provider policy with their digest and creation time, without materializing execution configuration.
// castAgentRuntimeConfiguration отображает версионированную конфигурацию runtime и модели сотрудника с вложенной политикой провайдера, хешем и временем создания, не материализуя конфигурацию исполнения.
func castAgentRuntimeConfiguration(value entity.AgentRuntimeConfiguration) *controlplanev1.AgentRuntimeConfiguration {
	return &controlplanev1.AgentRuntimeConfiguration{Ref: value.Ref, Version: value.Version, AgentRef: value.AgentRef,
		RuntimeProfileRef: value.RuntimeProfileRef, Provider: value.Provider, Model: value.Model,
		ProviderPolicy: castProviderPolicy(value.ProviderPolicy), Digest: value.Digest, CreatedAt: timestamp(value.CreatedAt)}
}

// castConfigOverlay preserves nil overlay presence and projects content, schema/version/digest, publication time and diagnostics in order; it does not validate or publish the overlay.
// castConfigOverlay сохраняет отсутствие наложения при nil и отображает содержимое, схему, версию, хеш, время публикации и упорядоченные диагностики; наложение не валидирует и не публикует.
func castConfigOverlay(value *entity.ConfigOverlayVersion) *controlplanev1.ConfigOverlayVersion {
	if value == nil {
		return nil
	}
	result := &controlplanev1.ConfigOverlayVersion{Ref: value.Ref, Version: value.Version, Revision: value.Revision,
		State: value.State, Content: value.Content, Digest: value.Digest, ValidationMessages: value.ValidationMessages,
		CreatedAt: timestamp(value.CreatedAt), PublishedAt: optionalTimestamp(value.PublishedAt), SchemaRevision: value.SchemaRevision, SchemaDigest: value.SchemaDigest}
	for _, item := range value.Diagnostics {
		result.Diagnostics = append(result.Diagnostics, &controlplanev1.ConfigOverlayDiagnostic{Code: item.Code, Key: item.Key, Line: item.Line, Column: item.Column, Message: item.Message})
	}
	return result
}

// castRuntimeEnvironmentVersion projects an environment revision with exact image metadata, policy, non-secret values, secret descriptors and selected tools; descriptors contain references, not loaded secret values.
// castRuntimeEnvironmentVersion отображает ревизию окружения с точными метаданными образа, политикой, несекретными значениями, описаниями секретов и выбранными инструментами; описания содержат ссылки, не загруженные значения секретов.
func castRuntimeEnvironmentVersion(value entity.RuntimeEnvironmentVersion) *controlplanev1.RuntimeEnvironmentVersion {
	result := &controlplanev1.RuntimeEnvironmentVersion{Ref: value.Ref, Version: value.Version, Revision: value.Revision,
		Digest: value.Digest, CreatedAt: timestamp(value.CreatedAt), Image: &controlplanev1.RuntimeEnvironmentImage{
			ArtifactRef: value.Image.ArtifactRef, RecipeRef: value.Image.RecipeRef, RecipeGeneration: value.Image.RecipeGeneration,
			Reference: value.Image.Reference, Digest: value.Image.Digest,
		}, Policy: castRuntimeEnvironmentPolicy(value.Policy)}
	for _, item := range value.Values {
		result.Values = append(result.Values, &controlplanev1.RuntimeEnvironmentValue{Name: item.Name, Value: item.Value})
	}
	for _, item := range value.SecretDescriptors {
		result.SecretDescriptors = append(result.SecretDescriptors, &controlplanev1.RuntimeSecretDescriptor{Name: item.Name,
			SecretRef: item.SecretRef, Namespace: item.Namespace, Revision: item.Revision,
			SecretName: item.SecretName, SecretKey: item.SecretKey, SecretUid: item.SecretUID,
			SecretResourceVersion: item.SecretResourceVersion, ContentSha256: item.ContentSHA256})
	}
	for _, item := range value.Tools {
		result.Tools = append(result.Tools, &controlplanev1.RuntimeEnvironmentTool{Name: item.Name, Command: item.Command, Description: item.Description, UsageHint: item.UsageHint})
	}
	return result
}

// castRuntimeEnvironmentPolicy projects resource/workspace limits, volumes and network/Kubernetes policy with supplied digests, copying HTTP-method lists; projection neither admits the policy nor creates cluster resources.
// castRuntimeEnvironmentPolicy отображает лимиты ресурсов и рабочего каталога, тома, сетевую и Kubernetes-политику с заданными хешами, копируя списки HTTP-методов; политику не допускает и ресурсы кластера не создаёт.
func castRuntimeEnvironmentPolicy(value runtimecontract.RuntimeEnvironmentPolicy) *controlplanev1.RuntimeEnvironmentPolicy {
	result := &controlplanev1.RuntimeEnvironmentPolicy{
		Resources: &controlplanev1.RuntimeResourcePolicy{
			CpuRequestMilli: value.Resources.CPURequestMilli, CpuLimitMilli: value.Resources.CPULimitMilli,
			MemoryRequestMib: value.Resources.MemoryRequestMiB, MemoryLimitMib: value.Resources.MemoryLimitMiB,
			EphemeralStorageRequestMib: value.Resources.EphemeralStorageRequestMiB,
			EphemeralStorageLimitMib:   value.Resources.EphemeralStorageLimitMiB,
		}, Network: &controlplanev1.RuntimeNetworkPolicy{DenyByDefault: value.Network.DenyByDefault,
			WebAccess: &controlplanev1.RuntimeWebAccess{Mode: castRuntimeWebAccessMode(value.Network.WebAccess.Mode)}},
		KubernetesAccess: &controlplanev1.RuntimeKubernetesAccessProfile{
			Kind: castRuntimeKubernetesAccessKind(value.KubernetesAccess.Kind), Namespace: value.KubernetesAccess.Namespace,
		}, ResourcesDigest: value.ResourcesDigest, VolumesDigest: value.VolumesDigest,
		NetworkDigest: value.NetworkDigest, RbacDigest: value.RBACDigest,
	}
	if limits := value.Resources.WorkspaceLimits; limits != nil {
		result.Resources.WorkspaceLimits = &controlplanev1.RuntimeWorkspaceLimits{MaxBytes: limits.MaxBytes, MaxFiles: limits.MaxFiles}
	}
	for _, volume := range value.Volumes {
		result.Volumes = append(result.Volumes, &controlplanev1.RuntimeVolume{
			Name: volume.Name, Kind: castRuntimeVolumeKind(volume.Kind), SizeMib: volume.SizeMiB, MountPath: volume.MountPath,
		})
	}
	for _, egress := range value.Network.Egress {
		result.Network.Egress = append(result.Network.Egress, &controlplanev1.RuntimeNetworkEgress{
			Destination: castRuntimeNetworkDestination(egress.Destination), Protocol: castRuntimeNetworkProtocol(egress.Protocol), Port: egress.Port,
		})
	}
	for _, rule := range value.Network.WebAccess.Rules {
		result.Network.WebAccess.Rules = append(result.Network.WebAccess.Rules, &controlplanev1.RuntimeWebAccessRule{
			DomainPattern: rule.DomainPattern, Protocol: rule.Protocol, Port: rule.Port, HttpMethods: append([]string(nil), rule.HTTPMethods...),
		})
	}
	return result
}

// castRuntimeWebAccessMode maps the four recognized runtime web-access modes and otherwise returns UNSPECIFIED, without normalizing or broadening domain rules.
// castRuntimeWebAccessMode переводит четыре известных режима веб-доступа runtime, иначе возвращает UNSPECIFIED, не нормализуя и не расширяя правила доменов.
func castRuntimeWebAccessMode(value string) controlplanev1.RuntimeWebAccessMode {
	switch value {
	case runtimecontract.RuntimeWebAccessNone:
		return controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_NONE
	case runtimecontract.RuntimeWebAccessAllowlistReadOnly:
		return controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_ALLOWLIST_READ_ONLY
	case runtimecontract.RuntimeWebAccessAllowlistFull:
		return controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_ALLOWLIST_FULL
	case runtimecontract.RuntimeWebAccessFullPublic:
		return controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_FULL_PUBLIC
	default:
		return controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_UNSPECIFIED
	}
}

// castRuntimeVolumeKind maps exact ephemeral-memory storage to memory and all other strings to ephemeral disk; callers validate the source policy before this display fallback.
// castRuntimeVolumeKind переводит точное временное хранилище в памяти в память, остальные строки — во временный диск; исходную политику проверяют до этого отображаемого запасного значения.
func castRuntimeVolumeKind(value string) controlplanev1.RuntimeVolumeKind {
	if value == runtimecontract.RuntimeVolumeEphemeralMemory {
		return controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_MEMORY
	}
	return controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_DISK
}

// castRuntimeNetworkDestination maps only DNS, runtime-callback and provider-proxy destinations, using UNSPECIFIED for everything else rather than allowing arbitrary egress.
// castRuntimeNetworkDestination переводит только назначения DNS, обратного вызова runtime и прокси провайдера, используя UNSPECIFIED для остальных вместо разрешения произвольного исходящего доступа.
func castRuntimeNetworkDestination(value string) controlplanev1.RuntimeNetworkDestination {
	switch value {
	case runtimecontract.RuntimeEgressDNS:
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS
	case runtimecontract.RuntimeEgressRuntimeCallback:
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK
	case runtimecontract.RuntimeEgressProviderProxy:
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY
	default:
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_UNSPECIFIED
	}
}

// castRuntimeNetworkProtocol maps exact UDP to UDP and all other strings to TCP; source protocol validation is not performed by this transport fallback.
// castRuntimeNetworkProtocol переводит точный UDP в UDP, остальные строки — в TCP; этот транспортный запасной вариант исходный протокол не валидирует.
func castRuntimeNetworkProtocol(value string) controlplanev1.RuntimeNetworkProtocol {
	if value == runtimecontract.RuntimeProtocolUDP {
		return controlplanev1.RuntimeNetworkProtocol_RUNTIME_NETWORK_PROTOCOL_UDP
	}
	return controlplanev1.RuntimeNetworkProtocol_RUNTIME_NETWORK_PROTOCOL_TCP
}

// castRuntimeKubernetesAccessKind projects only the NONE access profile and returns UNSPECIFIED for every other kind, without assigning Kubernetes privileges.
// castRuntimeKubernetesAccessKind отображает только профиль доступа NONE и возвращает UNSPECIFIED для остальных видов, не назначая полномочия Kubernetes.
func castRuntimeKubernetesAccessKind(value string) controlplanev1.RuntimeKubernetesAccessKind {
	if value != runtimecontract.RuntimeKubernetesAccessNone {
		return controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_UNSPECIFIED
	}
	return controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE
}

// castRuntimeEnvironment projects environment scope, current revision, readiness blockers and next actions exactly as supplied by the owner; metadata mapping is not admission or a health probe.
// castRuntimeEnvironment отображает область окружения, текущую ревизию, причины неготовности и действия ровно как передал владелец данных; преобразование метаданных не является допуском или проверкой здоровья.
func castRuntimeEnvironment(value entity.RuntimeEnvironmentSet) *controlplanev1.RuntimeEnvironmentSet {
	return &controlplanev1.RuntimeEnvironmentSet{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef,
		ScopeKind: roleImageScopeKind(value.ScopeKind), OrganizationRef: value.OrganizationRef,
		Name: value.Name, Description: value.Description, State: value.State,
		CurrentVersion: castRuntimeEnvironmentVersion(value.CurrentVersion), UpdatedAt: timestamp(value.UpdatedAt),
		Ready: value.Ready, ReadinessBlockers: value.ReadinessBlockers, NextActions: nextActions(value.NextActions)}
}

// castRuntimeConfigurationView assembles runtime configuration, published/draft overlays, exact environment binding, safe effective config/schema and ordered skill/memory bindings without fetching secrets.
// castRuntimeConfigurationView собирает конфигурацию runtime, опубликованное и черновое наложения, точную привязку окружения, безопасную итоговую конфигурацию и схему и упорядоченные привязки навыков и памяти без загрузки секретов.
func castRuntimeConfigurationView(value entity.AgentRuntimeConfigurationView) *controlplanev1.AgentRuntimeConfigurationView {
	result := &controlplanev1.AgentRuntimeConfigurationView{Configuration: castAgentRuntimeConfiguration(value.Configuration),
		PublishedOverlay: castConfigOverlay(&value.PublishedOverlay), DraftOverlay: castConfigOverlay(value.DraftOverlay),
		EnvironmentBinding: &controlplanev1.AgentRuntimeEnvironmentBinding{Ref: value.EnvironmentBinding.Ref,
			Version: value.EnvironmentBinding.Version, AgentRef: value.EnvironmentBinding.AgentRef,
			EnvironmentRef: value.EnvironmentBinding.EnvironmentRef, Digest: value.EnvironmentBinding.Digest, VersionRef: value.EnvironmentBinding.VersionRef},
		Environment: castRuntimeEnvironment(value.Environment), SafeEffectiveConfig: value.SafeEffectiveConfig,
		AgentVersion: value.AgentVersion, OverlaySchema: castConfigOverlaySchema(value.OverlaySchema)}
	for _, binding := range value.SkillBindings {
		result.SkillBindings = append(result.SkillBindings, castContextBinding(binding))
	}
	for _, binding := range value.MemoryBindings {
		result.MemoryBindings = append(result.MemoryBindings, castContextBinding(binding))
	}
	return result
}

// castIntegrationCapability projects operation/risk/schema metadata, allowed approval policies and input fields; approval-required display is policy-not-NONE, not a recorded human decision.
// castIntegrationCapability отображает метаданные операции, риска, схемы, допустимые политики подтверждения и поля ввода; отображаемое требование подтверждения означает политику не NONE, не записанное решение человека.
func castIntegrationCapability(value entity.IntegrationCapability) *controlplanev1.IntegrationCapability {
	result := &controlplanev1.IntegrationCapability{
		Key: value.Key, Name: value.Name, Description: value.Description, Operation: value.Operation,
		Risk: value.Risk, TypedRisk: integrationRisk(value.Risk), ApprovalRequired: value.ApprovalPolicy != "NONE",
		ApprovalPolicy: integrationApprovalPolicy(value.ApprovalPolicy), ResourceKind: integrationResourceKind(value.ResourceKind),
		InputSchema: value.InputSchema, InputSchemaSha256: value.InputSchemaSHA256,
	}
	for _, policy := range value.AllowedApprovalPolicies {
		result.AllowedApprovalPolicies = append(result.AllowedApprovalPolicies, integrationApprovalPolicy(policy))
	}
	for _, field := range value.InputFields {
		result.InputFields = append(result.InputFields, castIntegrationField(field))
	}
	return result
}

// integrationRisk maps an exact INTEGRATION_RISK-prefixed catalog value with zero fallback, without classifying a new external operation.
// integrationRisk переводит точное значение каталога INTEGRATION_RISK с нулевым запасным значением, не классифицируя новую внешнюю операцию.
func integrationRisk(value string) controlplanev1.IntegrationRisk {
	return controlplanev1.IntegrationRisk(controlplanev1.IntegrationRisk_value["INTEGRATION_RISK_"+value])
}

// integrationApprovalPolicy maps the INTEGRATION_APPROVAL_POLICY name with zero fallback, without issuing approval or changing enforcement policy.
// integrationApprovalPolicy переводит имя INTEGRATION_APPROVAL_POLICY с нулевым запасным значением, не выдавая подтверждение и не меняя исполняемую политику.
func integrationApprovalPolicy(value string) controlplanev1.IntegrationApprovalPolicy {
	return controlplanev1.IntegrationApprovalPolicy(controlplanev1.IntegrationApprovalPolicy_value["INTEGRATION_APPROVAL_POLICY_"+value])
}

// integrationResourceKind maps exact INTEGRATION_RESOURCE_KIND metadata with zero fallback; it does not resolve the resource owner or authorize its use.
// integrationResourceKind переводит точные метаданные INTEGRATION_RESOURCE_KIND с нулевым запасным значением; владельца ресурса не разрешает и использование не допускает.
func integrationResourceKind(value string) controlplanev1.IntegrationResourceKind {
	return controlplanev1.IntegrationResourceKind(controlplanev1.IntegrationResourceKind_value["INTEGRATION_RESOURCE_KIND_"+value])
}

// castIntegrationField projects input-field constraints and copies allowed values, preserving presence flags for zero minimum/maximum rather than conflating zero with absence.
// castIntegrationField отображает ограничения поля ввода и копирует допустимые значения, сохраняя признаки наличия нулевых минимума и максимума вместо смешения нуля с отсутствием.
func castIntegrationField(value entity.IntegrationConfigurationField) *controlplanev1.IntegrationConfigurationField {
	result := &controlplanev1.IntegrationConfigurationField{Key: value.Key, Label: value.Label, Help: value.Help, ValueType: value.ValueType, Required: value.Required, Placeholder: value.Placeholder,
		Format: value.Format, AllowedValues: append([]string(nil), value.AllowedValues...), MaximumLength: value.MaximumLength}
	if value.Minimum != nil {
		result.Minimum = *value.Minimum
		result.HasMinimum = true
	}
	if value.Maximum != nil {
		result.Maximum = *value.Maximum
		result.HasMaximum = true
	}
	return result
}

// castIntegrationCredential projects credential revision/reference, secret UID/resource-version, hash and creation time only; it does not load or expose raw credential material.
// castIntegrationCredential отображает лишь ревизию и ссылку реквизитов, UID и версию ресурса секрета, хеш и время создания; исходные реквизиты не загружает и не раскрывает.
func castIntegrationCredential(value entity.IntegrationCredentialRevision) *controlplanev1.IntegrationCredentialRevision {
	return &controlplanev1.IntegrationCredentialRevision{
		Ref: value.Ref, Revision: value.Revision, SecretRef: value.SecretRef, SecretUid: value.SecretUID,
		SecretResourceVersion: value.SecretResourceVersion, ContentSha256: value.ContentSHA256, CreatedAt: timestamp(value.CreatedAt),
	}
}

// castInstruction preserves nil instruction presence and projects content, publication/validation state, version and diagnostics with optional publication time, without publishing it.
// castInstruction сохраняет отсутствие инструкции при nil и отображает содержимое, состояние публикации или валидации, версию, диагностики и необязательное время публикации, не публикуя её.
func castInstruction(value *entity.InstructionVersion) *controlplanev1.InstructionVersion {
	if value == nil {
		return nil
	}
	return &controlplanev1.InstructionVersion{Ref: value.Ref, Version: int64(value.VersionNumber), Revision: value.VersionNumber, State: instructionState(value.State), Content: value.Content, ValidationMessages: value.ValidationProblems, CreatedAt: timestamp(value.CreatedAt), PublishedAt: optionalTimestamp(value.PublishedAt)}
}

// castInstructionBinding preserves nil binding presence and projects exact binding/revision references, version and effective flag; it does not activate another instruction revision.
// castInstructionBinding сохраняет отсутствие привязки при nil и отображает точные ссылки привязки и ревизии, версию и признак действия; другую ревизию инструкции не активирует.
func castInstructionBinding(value *entity.AgentInstructionsBinding) *controlplanev1.AgentInstructionsBinding {
	if value == nil {
		return nil
	}
	return &controlplanev1.AgentInstructionsBinding{Ref: value.Ref, Version: value.Version, RevisionRef: value.RevisionRef, Effective: value.Effective}
}

// castAgent projects authorized agent/runtime/instruction/capability metadata and artifact-or-fallback avatar; runtime Ready is derived only from agent state READY and is not a live provider probe.
// castAgent отображает авторизованные метаданные сотрудника, runtime, инструкций и возможностей и аватар из файла либо запасной; Ready runtime выводит только из состояния сотрудника READY, не из живой проверки провайдера.
func castAgent(value entity.Agent) *controlplanev1.Agent {
	capabilities := make([]*controlplanev1.PlatformCapability, 0, len(value.Capabilities))
	for _, key := range value.Capabilities {
		capabilities = append(capabilities, &controlplanev1.PlatformCapability{Key: key, Name: key, AvailableWithoutIntegration: strings.HasPrefix(key, "platform.")})
	}
	avatar := &controlplanev1.AgentAvatar{Source: controlplanev1.AgentAvatar_SOURCE_FALLBACK}
	if value.Avatar.ArtifactRef != "" {
		avatar.Source = controlplanev1.AgentAvatar_SOURCE_ARTIFACT
		avatar.ArtifactRef = value.Avatar.ArtifactRef
		avatar.ArtifactRevision = value.Avatar.ArtifactRevision
		avatar.ContentPath = value.Avatar.ContentPath
	}
	return &controlplanev1.Agent{InstructionBinding: castInstructionBinding(value.InstructionBinding), Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, RoleDefinitionRef: value.RoleDefinitionRef, RoleDefinitionName: value.RoleDefinitionName, Name: value.Name, Purpose: value.Purpose, RoleDescription: value.RoleDescription, AvatarUrl: value.AvatarURL, Avatar: avatar, State: agentState(value.State), Enabled: value.Enabled, System: value.System, Runtime: &controlplanev1.RuntimeSelection{Ref: value.RuntimeKey, Name: value.RuntimeName, Revision: value.RuntimeRevision, Ready: value.State == "READY", Provider: value.Provider, Model: value.Model}, PublishedInstructions: castInstruction(value.PublishedInstructions), DraftInstructions: castInstruction(value.DraftInstructions), Capabilities: capabilities, IntegrationGrantRefs: value.IntegrationGrantRefs, KnowledgeArtifactRefs: value.KnowledgeArtifactRefs, UpdatedAt: timestamp(value.UpdatedAt), NextActions: nextActions(value.NextActions), CurrentRunRef: value.CurrentRunRef}
}

// castWorkflowVersion preserves nil version presence, copies input options and capability lists, derives missing step positions from order and zero step timeouts from workflow timeout, without validating execution readiness.
// castWorkflowVersion сохраняет отсутствие версии при nil, копирует варианты ввода и списки возможностей, выводит отсутствующие позиции шагов из порядка и нулевые тайм-ауты шагов из процесса без проверки готовности исполнения.
func castWorkflowVersion(value *entity.WorkflowVersion, state, coordinatorAgentRef string) *controlplanev1.WorkflowVersion {
	if value == nil {
		return nil
	}
	inputs := make([]*controlplanev1.WorkflowInputField, 0, len(value.Inputs))
	for _, input := range value.Inputs {
		inputs = append(inputs, &controlplanev1.WorkflowInputField{Key: input.Key, Label: input.Label, Description: input.Help, ValueType: input.Type, Required: input.Required, Options: append([]string(nil), input.Options...)})
	}
	steps := make([]*controlplanev1.WorkflowStep, 0, len(value.Steps))
	for index, step := range value.Steps {
		position := step.Position
		if position == 0 {
			position = int32(index + 1)
		}
		timeoutSeconds := step.TimeoutSeconds
		if timeoutSeconds == 0 {
			timeoutSeconds = int32(value.TimeoutSeconds)
		}
		steps = append(steps, &controlplanev1.WorkflowStep{
			Ref: step.Key, Position: position, Name: step.Name, Purpose: step.Instructions, AgentRef: step.AgentRef,
			Parallel: step.Parallel, ParallelGroup: step.ParallelGroup, TimeoutSeconds: timeoutSeconds,
			ExpectedResult: step.ExpectedResult, HumanGate: step.HumanGateAfter,
			ApprovalTimeoutSeconds: step.ApprovalTimeoutSeconds,
			DependsOn:              append([]string(nil), step.DependsOn...), ResultRequirements: castWorkflowResultRequirements(step.ResultRequirements),
			GateDecisions: gateDecisions(step.GateDecisions), RequiredCapabilityKeys: append([]string(nil), step.RequiredCapabilityKeys...),
		})
	}
	return &controlplanev1.WorkflowVersion{ExecutionMode: value.ExecutionMode, Ref: value.Ref, Version: int64(value.VersionNumber), Revision: value.VersionNumber, State: workflowState(state), CoordinatorAgentRef: coordinatorAgentRef, InputFields: inputs, Steps: steps, MaxConcurrency: value.Concurrency, TimeoutSeconds: int32(value.TimeoutSeconds), CompletionCriteria: value.CompletionCriteria}
}

// castWorkflow projects published/draft workflow versions and optional card/readiness snapshots, preserving absent activity and authoritative readiness metadata rather than recomputing counts or eligibility.
// castWorkflow отображает опубликованную и черновую версии процесса и необязательные снимки карточки и готовности, сохраняя отсутствие активности и авторитетные метаданные готовности без пересчёта счётчиков и допустимости.
func castWorkflow(value entity.Workflow) *controlplanev1.Workflow {
	result := &controlplanev1.Workflow{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, Name: value.Name, Purpose: value.Purpose, State: workflowState(value.State), PublishedVersion: castWorkflowVersion(value.Published, "PUBLISHED", value.CoordinatorAgentRef), DraftVersion: castWorkflowVersion(value.Draft, value.State, value.CoordinatorAgentRef), UpdatedAt: timestamp(value.UpdatedAt), NextActions: nextActions(value.NextActions)}
	if value.CardSummary != nil {
		card := value.CardSummary
		result.CardSummary = &controlplanev1.WorkflowCardSummary{StageCount: card.StageCount, UniqueAgentCount: card.UniqueAgentCount, ParallelGroupCount: card.ParallelGroupCount, HasHumanGate: card.HasHumanGate, ActiveRunCount: card.ActiveRunCount, PendingGateCount: card.PendingGateCount, LastActivityAt: optionalTimestamp(card.LastActivityAt)}
	}
	if value.LaunchReadiness != nil {
		r := value.LaunchReadiness
		result.LaunchReadiness = &controlplanev1.WorkflowLaunchReadiness{AllowedToSubmit: r.AllowedToSubmit, Reason: r.Reason, WorkflowVersion: r.WorkflowVersion, RevisionRef: r.RevisionRef, ContextDigest: r.ContextDigest, OperationalState: r.OperationalState}
	}
	return result
}

// castRunTarget selects workflow or system-assistant oneof branches for those exact kinds and otherwise uses the agent branch, preserving supplied name/ref/version without resolving a target.
// castRunTarget выбирает ветки oneof процесса либо системного помощника для этих точных видов, иначе ветку сотрудника, сохраняя название, ссылку и версию без разрешения цели.
func castRunTarget(value entity.RunTarget) *controlplanev1.RunTarget {
	target := &controlplanev1.RunTarget{DisplayName: value.Name, TargetVersion: value.Version}
	if value.Type == "WORKFLOW" {
		target.Target = &controlplanev1.RunTarget_WorkflowRef{WorkflowRef: value.Ref}
	} else if value.Type == "SYSTEM_ASSISTANT" {
		target.Target = &controlplanev1.RunTarget_SystemAssistantRef{SystemAssistantRef: value.Ref}
	} else {
		target.Target = &controlplanev1.RunTarget_AgentRef{AgentRef: value.Ref}
	}
	return target
}

// castTokenUsage copies all typed token counters and context-window metadata to transport without aggregation, pricing or financial-eligibility decisions.
// castTokenUsage копирует все типизированные счётчики токенов и метаданные окна контекста в транспорт без агрегирования, расчёта цены или решения финансовой допустимости.
func castTokenUsage(value entity.TokenUsage) *controlplanev1.TokenUsage {
	return &controlplanev1.TokenUsage{Completeness: controlplanev1.UsageCompleteness(value.Completeness), TotalTokens: value.TotalTokens, InputTokens: value.InputTokens, CachedInputTokens: value.CachedInputTokens, CacheWriteInputTokens: value.CacheWriteInputTokens, OutputTokens: value.OutputTokens, ReasoningOutputTokens: value.ReasoningOutputTokens, ModelContextWindow: value.ModelContextWindow}
}

// usageFromProto maps nil usage to a zero domain value and otherwise copies every typed counter/context window through protobuf getters without interpreting provider limits.
// usageFromProto преобразует nil потребление в нулевое доменное значение, иначе копирует все типизированные счётчики и окно контекста через protobuf getters без интерпретации лимитов провайдера.
func usageFromProto(value *controlplanev1.TokenUsage) entity.TokenUsage {
	if value == nil {
		return entity.TokenUsage{}
	}
	quality := runtimecontract.UsageCompleteness(value.GetCompleteness())
	if value.GetCompleteness() < 0 || value.GetCompleteness() > controlplanev1.UsageCompleteness_USAGE_COMPLETENESS_COMPLETE {
		quality = runtimecontract.UsageCompleteness(255)
	}
	return entity.TokenUsage{Completeness: quality, TotalTokens: value.GetTotalTokens(), InputTokens: value.GetInputTokens(), CachedInputTokens: value.GetCachedInputTokens(), CacheWriteInputTokens: value.GetCacheWriteInputTokens(), OutputTokens: value.GetOutputTokens(), ReasoningOutputTokens: value.GetReasoningOutputTokens(), ModelContextWindow: value.GetModelContextWindow()}
}

// castRun projects run/session/root/retry lineage, usage, incidents, attachment/gate refs, readiness and optional assistant pin from the authorized snapshot; it does not create provenance or mutate execution state.
// castRun отображает происхождение запуска, сессии, корня и повтора, потребление, инциденты, ссылки вложений и шлюзов, готовность и необязательную привязку помощника из авторизованного снимка; происхождение не создаёт и исполнение не меняет.
func castRun(value entity.Run) *controlplanev1.Run {
	result := &controlplanev1.Run{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, SessionRef: value.SessionRef, RootRunRef: value.RootRunRef, ParentRunRef: value.ParentRunRef, RetryOfRunRef: value.RetryOfRunRef, Target: castRunTarget(value.Target), Title: value.Title, TitleSource: value.TitleSource, ActivitySummary: value.ActivitySummary, InputSummary: value.Task, State: runState(value.State), Source: runSource(value.Source), Initiator: &controlplanev1.UserSummary{DisplayName: value.InitiatorName}, Attempt: value.Attempt, GraphRevision: value.GraphRevision, LastEventSequence: value.EventSequence, ResultSummary: value.ResultSummary, SafeErrorCode: value.SafeErrorCode, SafeErrorMessage: value.SafeErrorMessage, Usage: castTokenUsage(value.Usage), InputAttachmentSetRef: value.InputAttachmentSetRef, ArtifactRefs: value.ArtifactRefs, GateRefs: value.GateRefs, CreatedAt: timestamp(value.CreatedAt), StartedAt: optionalTimestamp(value.StartedAt), FinishedAt: optionalTimestamp(value.FinishedAt), NextActions: nextActions(value.NextActions)}
	for _, incident := range value.Incidents {
		result.Incidents = append(result.Incidents, castIncident(incident))
	}
	result.SessionReadiness = castRunSessionReadiness(value.SessionReadiness)
	if pin := value.AssistantPin; pin != nil {
		result.AssistantPin = &controlplanev1.AssistantRunPin{
			Scope: controlplanev1.AssistantScope(controlplanev1.AssistantScope_value["ASSISTANT_SCOPE_"+pin.Scope]), OrganizationRef: pin.OrganizationRef,
			ConversationRef: pin.ConversationRef, AssistantRef: pin.AssistantRef,
			ProjectRef: pin.ProjectRef, ProfileRef: pin.ProfileRef,
		}
	}
	return result
}

// castNode projects graph-node execution/turn/attempt, safe progress/errors and child/artifact refs, deriving Planned only from materialization state; it does not infer new child authority.
// castNode отображает исполнение, ход и попытку узла графа, безопасные прогресс и ошибки и ссылки детей и файлов, выводя Planned лишь из состояния материализации; новые полномочия детей не выводит.
func castNode(value entity.RunNode) *controlplanev1.RunNode {
	return &controlplanev1.RunNode{WorkflowResults: castWorkflowAttemptResults(value.WorkflowResults), Ref: value.Ref, RunRef: value.RunRef, ParentNodeRef: value.ParentNodeRef, Type: nodeType(value.Type), State: nodeState(value.State), DisplayName: value.DisplayName, Role: value.Role, AgentRef: value.AgentRef, TurnRef: value.TurnRef, Attempt: value.Attempt, InputSummary: value.InputSummary, ProgressSummary: value.ProgressSummary, IntegrationNames: value.IntegrationNames, ArtifactRefs: value.ArtifactRefs, ChildRunRefs: value.ChildRunRefs, CallbackSummary: value.CallbackSummary, SafeErrorCode: value.SafeErrorCode, SafeErrorMessage: value.SafeErrorMessage, CreatedAt: timestamp(value.CreatedAt), StartedAt: optionalTimestamp(value.StartedAt), FinishedAt: optionalTimestamp(value.FinishedAt), NextActions: nextActions(value.NextActions), Planned: value.MaterializationState == "PLANNED"}
}

// castEdge projects stored edge/root endpoints, relation type and label; returning IDs does not authorize creation or traversal of a new relation.
// castEdge отображает сохранённые концы ребра и корня, вид связи и подпись; возврат ID не разрешает создание или обход новой связи.
func castEdge(value entity.RunEdge) *controlplanev1.RunEdge {
	return &controlplanev1.RunEdge{Ref: value.Ref, RunRef: value.RunRef, SourceNodeRef: value.SourceNodeRef, TargetNodeRef: value.TargetNodeRef, Type: edgeType(value.Type), Label: value.Label}
}

// castRunDelta preserves absent run delta and maps versioned lifecycle/graph/event/usage/result metadata without merging it into an existing state.
// castRunDelta сохраняет отсутствие изменения запуска и переводит версионированные состояние, граф, последовательность событий, потребление и результат без слияния с существующим состоянием.
func castRunDelta(value *entity.RunDelta) *controlplanev1.RunDelta {
	if value == nil {
		return nil
	}
	return &controlplanev1.RunDelta{Ref: value.Ref, Version: value.Version, State: runState(value.State), GraphRevision: value.GraphRevision, LastEventSequence: value.EventSequence, ResultSummary: value.ResultSummary, SafeErrorCode: value.SafeErrorCode, SafeErrorMessage: value.SafeErrorMessage, Usage: castTokenUsage(value.Usage), ArtifactRefs: value.ArtifactRefs, GateRefs: value.GateRefs, StartedAt: optionalTimestamp(value.StartedAt), FinishedAt: optionalTimestamp(value.FinishedAt), NextActions: nextActions(value.NextActions)}
}

// castEvent projects event actor, message kind, graph sequence and optional typed deltas/tool call/execution/message, preserving zero historical tool revision; payload safety and lineage are checked by producers.
// castEvent отображает автора события, вид сообщения, последовательность графа и необязательные типизированные изменения, вызов инструмента, исполнение и сообщение, сохраняя нулевую историческую ревизию инструмента; безопасность данных и происхождение проверяют производители.
func castEvent(value entity.RunEvent) *controlplanev1.RunEvent {
	event := &controlplanev1.RunEvent{Ref: value.Ref, RunRef: value.RunRef, Sequence: value.Sequence, Type: eventType(value.Type), NodeRef: value.NodeRef, EdgeRef: value.EdgeRef, GateRef: value.GateRef, ArtifactRef: value.ArtifactRef, Summary: value.Summary, Progress: value.Progress, RunState: runState(value.RunState), NodeState: nodeState(value.NodeState), OccurredAt: timestamp(value.OccurredAt), GraphRevision: value.GraphRevision, Run: castRunDelta(value.Delta.Run), Actor: &controlplanev1.RunEventActor{Kind: controlplanev1.RunEventActorKind(controlplanev1.RunEventActorKind_value["RUN_EVENT_ACTOR_KIND_"+value.Actor.Kind]), Ref: value.Actor.Ref, Name: value.Actor.Name}, MessageKind: controlplanev1.RunEventMessageKind(controlplanev1.RunEventMessageKind_value["RUN_EVENT_MESSAGE_KIND_"+value.MessageKind])}
	if value.Delta.IntegrationInvocationRef != "" {
		event.IntegrationInvocationRef = &value.Delta.IntegrationInvocationRef
	}
	if value.ToolCall != nil {
		// Нулевая revision допустима только при чтении старого terminal receipt;
		// новые команды проходят строгую owner lifecycle validation.
		event.ToolCall = &controlplanev1.RunToolCall{Ref: value.ToolCall.Ref, Tool: value.ToolCall.Tool, SafeParameters: structure(value.ToolCall.SafeParameters), CapabilityRef: value.ToolCall.CapabilityRef, GrantRef: value.ToolCall.GrantRef, State: controlplanev1.RunToolCallState(controlplanev1.RunToolCallState_value["RUN_TOOL_CALL_STATE_"+value.ToolCall.State]), DurationMs: value.ToolCall.DurationMS, SafeResult: value.ToolCall.SafeResult, AuditRef: value.ToolCall.AuditRef}
	}
	if value.ToolCall != nil {
		event.ToolCall.Revision = value.ToolCall.Revision
	}
	if execution := value.Delta.Execution; execution != nil {
		event.Execution = &controlplanev1.RunEventExecution{RunRef: execution.RunRef, NodeRef: execution.NodeRef, SessionRef: execution.SessionRef,
			TurnRef: execution.TurnRef, TurnNumber: execution.TurnNumber, Attempt: execution.Attempt}
	}
	if message := value.Delta.Message; message != nil {
		event.Message = &controlplanev1.RunMessage{Ref: message.Ref, Phase: controlplanev1.RunMessagePhase(controlplanev1.RunMessagePhase_value["RUN_MESSAGE_PHASE_"+message.Phase]), Revision: message.Revision, Text: message.Text, Source: castMessageSource(message.Source)}
	}
	if value.Delta.Node != nil {
		event.Node = castNode(*value.Delta.Node)
	}
	if value.Delta.Edge != nil {
		event.Edge = castEdge(*value.Delta.Edge)
	}
	if value.Delta.Gate != nil {
		event.Gate = castGate(*value.Delta.Gate)
	}
	if value.Delta.Artifact != nil {
		event.Artifact = castArtifact(*value.Delta.Artifact)
	}
	if value.Delta.Incident != nil {
		event.Incident = castIncident(*value.Delta.Incident)
	}
	return event
}

// castGraph projects graph header and ordered node/edge lists through their distinct typed casters, without reconstructing lineage or adding authorization.
// castGraph отображает заголовок графа и упорядоченные списки узлов и рёбер через разные типизированные преобразователи без восстановления происхождения или добавления полномочий.
func castGraph(value entity.RunGraph) *controlplanev1.RunGraph {
	result := &controlplanev1.RunGraph{RunRef: value.RunRef, Revision: value.Revision, Sequence: value.Sequence}
	for _, node := range value.Nodes {
		result.Nodes = append(result.Nodes, castNode(node))
	}
	for _, edge := range value.Edges {
		result.Edges = append(result.Edges, castEdge(edge))
	}
	return result
}

// castGate projects exact gate scope, actors, decision/consequence metadata, attachments and optional JSON-normalized integration intent; this read does not resolve a gate or execute its previewed effect.
// castGate отображает точную область шлюза, авторов, метаданные решения и последствий, вложения и необязательное намерение интеграции с JSON-нормализованным предпросмотром; это чтение шлюз не закрывает и показанное действие не исполняет.
func castGate(value entity.OwnerGate) *controlplanev1.OwnerGate {
	gate := &controlplanev1.OwnerGate{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, RunRef: value.RunRef, NodeRef: value.NodeRef, Title: value.Title, ContextSummary: value.ContextSummary, ConsequencesSummary: value.Prompt, RequestedBy: &controlplanev1.UserSummary{Ref: value.RequestedByRef, DisplayName: value.RequestedByName}, State: gateState(value.State), AllowedDecisions: gateDecisions(value.AllowedDecisions), Decision: gateDecision(value.Decision), DecisionComment: value.DecisionComment, OpenedAt: timestamp(value.CreatedAt), DecidedAt: optionalTimestamp(value.ResolvedAt), ExpiresAt: optionalTimestamp(value.ExpiresAt), ResolutionAttachmentSetRef: value.ResolutionAttachmentSetRef, NextActions: nextActions(value.NextActions)}
	if value.ResolvedByName != "" {
		gate.DecidedBy = &controlplanev1.UserSummary{DisplayName: value.ResolvedByName}
	}
	gate.SourceAttachmentSetRef = value.SourceAttachmentSetRef
	gate.ScopeKind = runtimeSecretScopeKind(value.ScopeKind)
	gate.OrganizationRef = value.OrganizationRef
	for _, consequence := range value.DecisionConsequences {
		gate.DecisionConsequences = append(gate.DecisionConsequences, &controlplanev1.OwnerGateDecisionConsequence{Decision: gateDecision(consequence.Decision), SafeSummary: consequence.SafeSummary, ExecutesExternalEffect: consequence.ExecutesExternalEffect, TerminalForRun: consequence.TerminalForRun})
	}
	if intent := value.IntegrationIntent; intent != nil {
		gate.IntegrationIntent = &controlplanev1.IntegrationIntent{ConnectionRef: intent.ConnectionRef, ConnectionName: intent.ConnectionName, DefinitionKey: intent.DefinitionKey, CapabilityKey: intent.CapabilityKey, Operation: intent.Operation, EffectKey: intent.EffectKey, EffectPreview: gatePreviewStructure(intent.EffectPreview), ResourceScope: &controlplanev1.IntegrationResourceScope{Kind: integrationResourceKind(intent.ResourceKind), Values: intent.ResourceScope, Digest: intent.ResourceScopeDigest}}
	}
	return gate
}

// castArtifact projects authorized artifact lifecycle/scan/source/revision metadata and derives preview availability only from AVAILABLE; it returns no content and performs no new scan.
// castArtifact отображает авторизованные метаданные жизненного цикла, проверки, источника и ревизии файла, выводя доступность предпросмотра только из AVAILABLE; содержимое не возвращает и новую проверку не выполняет.
func castArtifact(value entity.Artifact) *controlplanev1.Artifact {
	return &controlplanev1.Artifact{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, RunRef: value.RunRef, SessionRef: value.SessionRef, FileName: value.FileName, MediaType: value.MediaType, SizeBytes: value.SizeBytes, ScanState: scanState(value.ScanState), Source: artifactSource(value.Source), Revision: int32(value.Revision), AgentBindings: value.Bindings, PreviewAvailable: value.PreviewState == "AVAILABLE", CreatedAt: timestamp(value.CreatedAt), NextActions: nextActions(value.NextActions), Digest: value.Digest, LifecycleState: artifactLifecycleState(value.LifecycleState), DeletedAt: optionalTimestamp(value.DeletedAt), PurgeAfter: optionalTimestamp(value.PurgeAfter), CurrentRevisionRef: value.CurrentRevisionRef}
}

// castArtifactRevision projects one exact historical artifact revision and supplied scan/source/preview metadata, without replacing it with the current artifact or reading content.
// castArtifactRevision отображает одну точную историческую ревизию файла и переданные метаданные проверки, источника и предпросмотра без замены текущим файлом и чтения содержимого.
func castArtifactRevision(value entity.ArtifactRevision) *controlplanev1.ArtifactRevision {
	return &controlplanev1.ArtifactRevision{Ref: value.Ref, ArtifactRef: value.ArtifactRef, Revision: value.Revision,
		FileName: value.FileName, MediaType: value.MediaType, SizeBytes: value.SizeBytes, Digest: value.Digest,
		ScanState: value.ScanState, Source: value.Source, PreviewAvailable: value.PreviewAvailable, CreatedAt: timestamp(value.CreatedAt)}
}

// castAttachmentSet projects exact family/revision/manifest metadata and ordered item pins including artifact version/digest/source, without finalizing the set or downloading attachments.
// castAttachmentSet отображает точные семейство, ревизию и манифест и упорядоченные привязки элементов, включая версию, хеш и источник файла, без финализации набора и скачивания вложений.
func castAttachmentSet(value entity.AttachmentSet) *controlplanev1.AttachmentSet {
	result := &controlplanev1.AttachmentSet{Ref: value.Ref, FamilyRef: value.FamilyRef, Revision: value.Revision,
		Version: value.Version, ProjectRef: value.ProjectRef, State: attachmentSetState(value.State),
		Purpose: attachmentSetPurpose(value.Purpose), Source: value.Source, ItemCount: value.ItemCount,
		TotalSizeBytes: value.TotalSizeBytes, ManifestDigest: value.ManifestDigest, CreatedAt: timestamp(value.CreatedAt),
		FinalizedAt: optionalTimestamp(value.FinalizedAt), Superseded: value.Superseded}
	for _, item := range value.Items {
		result.Items = append(result.Items, &controlplanev1.AttachmentSetItem{ArtifactRef: item.ArtifactRef,
			ArtifactRevision: item.ArtifactRevision, ArtifactVersion: item.ArtifactVersion,
			DisplayName: item.DisplayName, MediaType: item.MediaType, SizeBytes: item.SizeBytes,
			Digest: item.Digest, Source: artifactSource(item.Source), Position: item.Position})
	}
	return result
}

// castSchedule projects schedule timing, policies, input/prompt maps, revision and continuation metadata, overriding target-version display with the schedule's pinned version; it does not calculate a next occurrence.
// castSchedule отображает время расписания, правила, карты ввода и промпта, ревизию и продолжение, заменяя отображаемую версию цели закреплённой версией расписания; следующий экземпляр запуска не рассчитывает.
func castSchedule(value entity.Schedule) *controlplanev1.Schedule {
	target := castRunTarget(value.Target)
	target.TargetVersion = value.TargetVersion
	return &controlplanev1.Schedule{
		Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, Name: value.Name,
		Target: target, State: scheduleState(value), Preset: value.Preset,
		CronExpression: value.CronExpression, Timezone: value.Timezone, Input: structure(value.Input),
		SessionPolicy: value.SessionPolicy, NotificationPolicy: value.NotificationPolicy,
		NextRunAt: optionalTimestamp(value.NextRunAt), NextActions: nextActions(value.NextActions),
		TimeOfDay: value.TimeOfDay, DayOfWeek: value.DayOfWeek,
		CurrentRevision:    castScheduleRevision(value.CurrentRevision),
		ContinueSessionRef: value.ContinueSessionRef,
		LastOutcome:        value.LastOutcome,
		DstGapPolicy:       value.DSTGapPolicy, DstFoldPolicy: value.DSTFoldPolicy,
		MisfirePolicy: value.MisfirePolicy, OverlapPolicy: value.OverlapPolicy,
		TargetVersion: value.TargetVersion, TargetDigest: value.TargetDigest,
		AutomationText: value.AutomationText, PromptInputs: structure(value.PromptInputs),
	}
}

// castDefinition projects a shipped integration catalog definition with version/digest/adapter readiness, authoritative connection counters and typed capability/input-field lists; BuiltIn does not itself grant execution rights.
// castDefinition отображает поставляемое определение интеграции с версией, хешем, готовностью адаптера, авторитетными счётчиками подключений и типизированными списками возможностей и полей; BuiltIn сам не выдаёт право исполнения.
func castDefinition(value entity.IntegrationDefinition) *controlplanev1.IntegrationDefinition {
	result := &controlplanev1.IntegrationDefinition{
		Version: value.Version, NextActions: append([]string{}, value.NextActions...),
		Key: value.Key, Name: value.Name, Description: value.Description, Category: value.Category, BuiltIn: true, Available: value.Enabled,
		SchemaVersion: value.SchemaVersion, DefinitionVersion: value.DefinitionVersion,
		Origin: controlplanev1.IntegrationDefinitionOrigin_INTEGRATION_DEFINITION_ORIGIN_SHIPPED,
		Digest: value.Digest, Adapter: value.Adapter, CredentialSecretKey: value.CredentialSecretKey,
		AdapterOwner: value.AdapterOwner, ExecutionRoute: value.ExecutionRoute, AdapterReadiness: value.AdapterReadiness,
		ConnectionCount: value.ConnectionCount, HealthyConnectionCount: value.HealthyConnectionCount,
	}
	for _, capability := range value.Capabilities {
		result.Capabilities = append(result.Capabilities, castIntegrationCapability(capability))
	}
	for _, field := range value.ConfigurationFields {
		result.ConfigurationFields = append(result.ConfigurationFields, castIntegrationField(field))
	}
	return result
}

// castGrant projects exact integration-grant policy/risk/resource-scope metadata and copied approval-scope paths, selecting agent only for AGENT and workflow otherwise; it does not issue or widen the grant.
// castGrant отображает точные правила, риск и область ресурса разрешения интеграции и копию путей подтверждения, выбирая сотрудника лишь для AGENT, иначе процесс; разрешение не выдаёт и не расширяет.
func castGrant(value entity.IntegrationGrant) *controlplanev1.IntegrationGrant {
	grant := &controlplanev1.IntegrationGrant{
		Ref: value.Ref, Version: value.Version, ConnectionVersion: value.ConnectionVersion, CapabilityKey: value.CapabilityKey, TargetName: value.TargetName, Enabled: value.Enabled,
		Risk: value.Risk, TypedRisk: integrationRisk(value.Risk), ApprovalPolicy: integrationApprovalPolicy(value.ApprovalPolicy),
		ResourceScope:      &controlplanev1.IntegrationResourceScope{Kind: integrationResourceKind(value.ResourceKind), Values: value.ResourceScope, Digest: value.ResourceScopeDigest},
		ApprovalScopePaths: append([]string(nil), value.ApprovalScopePaths...),
	}
	if value.TargetType == "AGENT" {
		grant.AgentRef = value.TargetRef
	} else {
		grant.WorkflowRef = value.TargetRef
	}
	return grant
}

// castConnection projects public connection config, masked credential status/hint, optional credential and definition bindings, capabilities and grants; CONFIGURED is metadata, not successful connectivity or human approval.
// castConnection отображает публичную конфигурацию подключения, скрытый статус реквизитов и подсказку, необязательные привязки реквизитов и определения, возможности и разрешения; CONFIGURED — метаданные, не успешное соединение и не одобрение человека.
func castConnection(value entity.IntegrationConnection) *controlplanev1.IntegrationConnection {
	credentialsHint := "i18n:INTEGRATION_CREDENTIAL_NOT_CONFIGURED"
	if value.MaskedCredentialsState == "CONFIGURED" {
		credentialsHint = "i18n:INTEGRATION_CREDENTIAL_CONFIGURED"
	} else if value.MaskedCredentialsState == "INVALID" {
		credentialsHint = "i18n:INTEGRATION_CREDENTIAL_INVALID"
	}
	result := &controlplanev1.IntegrationConnection{
		Ref: value.Ref, Version: value.Version, DefinitionKey: value.DefinitionKey, Name: value.Name,
		DefinitionVersion: value.DefinitionVersion, DefinitionDigest: value.DefinitionDigest,
		State: connectionState(value.State), CredentialsConfigured: value.MaskedCredentialsState == "CONFIGURED",
		CredentialsHint: credentialsHint, LastTestedAt: optionalTimestamp(value.LastTestedAt), LastTestOutcome: value.LastTestSummary,
		PublicConfiguration: structure(value.PublicConfiguration), NextActions: nextActions(value.NextActions),
		CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt),
	}
	if value.CredentialRevision != nil {
		result.CredentialRevision = castIntegrationCredential(*value.CredentialRevision)
	}
	if value.DefinitionConfigurationBinding != nil {
		binding := value.DefinitionConfigurationBinding
		result.DefinitionConfigurationBinding = &controlplanev1.IntegrationDefinitionConfigurationBinding{
			State: controlplanev1.IntegrationDefinitionConfigurationBinding_State(
				controlplanev1.IntegrationDefinitionConfigurationBinding_State_value["STATE_"+binding.State]),
			ConfigurationRef: binding.ConfigurationRef, RevisionRef: binding.RevisionRef, BindingVersion: binding.BindingVersion,
		}
	}
	for _, capability := range value.Capabilities {
		result.Capabilities = append(result.Capabilities, castIntegrationCapability(capability))
	}
	for _, grant := range value.Grants {
		result.Grants = append(result.Grants, castGrant(grant))
	}
	return result
}

// castPlan preserves nil plan presence and projects revision/receipt/validation plus typed operations, falling back to Input when Parameters is nil and removing top-level file content; only VALID plans advertise APPLY_PLAN, without authorizing apply.
// castPlan сохраняет отсутствие плана при nil и отображает ревизию, квитанцию, валидацию и типизированные операции, используя Input при nil Parameters и удаляя верхнеуровневое содержимое файла; APPLY_PLAN показывает лишь VALID плану, не разрешая применение.
func castPlan(value *entity.AssistantPlan) *controlplanev1.AssistantPlan {
	if value == nil {
		return nil
	}
	rawState := controlplanev1.AssistantPlanState_value["ASSISTANT_PLAN_STATE_"+value.State]
	result := &controlplanev1.AssistantPlan{Ref: value.Ref, Version: value.Version, ConversationRef: value.ConversationRef,
		ProjectRef: value.ProjectRef, AuditSummary: value.Summary, Applied: value.State == "APPLIED",
		State: controlplanev1.AssistantPlanState(rawState), Revision: value.Revision, ValidatedRevision: value.ValidatedRevision,
		ContentDigest: value.ContentDigest, ValidationProblems: append([]string(nil), value.ValidationProblems...),
		ValidatedAt: optionalTimestamp(value.ValidatedAt), AppliedAt: optionalTimestamp(value.AppliedAt),
		Receipt: castPlanReceipt(value.Receipt)}
	for _, operation := range value.Operations {
		raw := controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+operation.Type]
		rawAction := controlplanev1.AssistantPlanOperation_Action_value["ACTION_"+operation.Action]
		parameters := operation.Parameters
		if parameters == nil {
			parameters = operation.Input
		}
		after := operation.After
		if operation.Type == "CREATE_PROJECT_FILE" || operation.Type == "CREATE_PROJECT_FILE_REVISION" {
			parameters = assistantFilePublicFields(parameters)
			after = assistantFilePublicFields(after)
		}
		result.Operations = append(result.Operations, &controlplanev1.AssistantPlanOperation{
			Ref: operation.Key, Type: controlplanev1.AssistantPlanOperation_Type(raw), Action: controlplanev1.AssistantPlanOperation_Action(rawAction),
			Title: assistantPlanOperationTitle(operation), Summary: operation.Summary, TargetKind: operation.Target.Kind,
			TargetRef: operation.Target.Ref, TargetName: operation.Target.Name, TargetVersion: operation.Target.Version, ExpectedVersion: operation.ExpectedVersion,
			Parameters: structure(parameters), Before: structure(operation.Before), After: structure(after), Selected: operation.Selected,
			Permitted: operation.Permitted, UnavailableReason: operation.UnavailableReason,
			ValidationProblems: append([]string(nil), operation.ValidationProblems...),
		})
	}
	if value.State == "VALID" {
		result.NextActions = []controlplanev1.NextAction{controlplanev1.NextAction_NEXT_ACTION_APPLY_PLAN}
	}
	return result
}

// assistantFilePublicFields returns a shallow copy excluding only the top-level content key, including for historical/idempotent readback, without mutating the persisted map or recursively sanitizing other keys.
// assistantFilePublicFields возвращает поверхностную копию без одного верхнеуровневого ключа content, в том числе для исторического и идемпотентного чтения, не меняя сохранённую карту и не очищая остальные ключи рекурсивно.
func assistantFilePublicFields(fields map[string]any) map[string]any {
	result := make(map[string]any, len(fields))
	for key, value := range fields {
		if key != "content" {
			result[key] = value
		}
	}
	return result
}

// assistantPlanOperationTitle uses the operation-kind-specific name/fileName/title string from Parameters or nil-only Input fallback, then Summary for blank values, and bounds the displayed title without altering persisted text.
// assistantPlanOperationTitle берёт строку name/fileName/title по виду операции из Parameters либо Input только при nil Parameters, затем Summary для пустого значения и ограничивает отображаемое название без изменения сохранённого текста.
func assistantPlanOperationTitle(operation entity.AssistantPlanOperation) string {
	field := ""
	switch operation.Type {
	case "CREATE_PROJECT", "CREATE_AGENT", "CREATE_WORKFLOW", "CREATE_SCHEDULE", "CREATE_INTEGRATION_CONNECTION", "CREATE_RUNTIME_ENVIRONMENT_DRAFT", "CREATE_ROLE_IMAGE_RECIPE", "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION":
		field = "name"
	case "CREATE_PROJECT_FILE":
		field = "fileName"
	case "LAUNCH_RUN":
		field = "title"
	}

	title := ""
	if field != "" {
		parameters := operation.Parameters
		if parameters == nil {
			parameters = operation.Input
		}
		title, _ = parameters[field].(string)
	}
	if strings.TrimSpace(title) == "" {
		title = operation.Summary
	}
	return boundedAssistantPlanOperationTitle(title)
}

// boundedAssistantPlanOperationTitle trims outer whitespace and caps a title by Unicode code points, reserving the last position for an ellipsis only when the original exceeds the limit.
// boundedAssistantPlanOperationTitle удаляет крайние пробелы и ограничивает название по кодовым точкам Unicode, выделяя последнюю позицию многоточию только при превышении исходного лимита.
func boundedAssistantPlanOperationTitle(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maximumAssistantPlanOperationTitleRunes {
		return string(runes)
	}
	return string(runes[:maximumAssistantPlanOperationTitleRunes-1]) + "…"
}

// castConversation projects exact assistant/context metadata and public turn roles, interleaving fully loaded saved plans after the first following assistant reply by creation time; latest-plan fallback and remaining plans append after history without rewriting persisted sequences.
// castConversation отображает точные метаданные помощника и контекста и публичные роли ходов, вставляя полностью загруженные планы после первого следующего по времени ответа помощника; запасной последний план и оставшиеся планы добавляет после истории без перезаписи сохранённых номеров.
func castConversation(value entity.AssistantConversation) *controlplanev1.AssistantConversation {
	context := &controlplanev1.AssistantContextDescriptor{Route: value.Context.Route, EntityKind: value.Context.EntityKind,
		EntityRef: value.Context.EntityRef, EntityName: value.Context.EntityName, EntityVersion: value.Context.EntityVersion}
	for _, operation := range value.Context.AllowedOperations {
		context.AllowedOperations = append(context.AllowedOperations, controlplanev1.AssistantPlanOperation_Type(controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+operation]))
	}
	result := &controlplanev1.AssistantConversation{Ref: value.Ref, Version: value.Version, Title: value.Title,
		TitleSource: value.TitleSource, TitleRevision: value.TitleRevision, ProjectRef: value.ProjectRef,
		AssistantScope: controlplanev1.AssistantScope(controlplanev1.AssistantScope_value["ASSISTANT_SCOPE_"+value.AssistantScope]),
		AssistantRef:   value.AssistantRef, AssistantProfileRef: value.AssistantProfileRef,
		Context: context, UpdatedAt: timestamp(value.UpdatedAt), State: controlplanev1.AssistantConversationState(controlplanev1.AssistantConversationState_value["ASSISTANT_CONVERSATION_STATE_"+value.State])}
	nextSequence := int64(1)
	plans := value.Plans
	allPlansLoaded := len(plans) > 0
	if !allPlansLoaded && value.LatestPlan != nil {
		plans = []entity.AssistantPlan{*value.LatestPlan}
	}
	nextPlan := 0
	appendPlan := func(plan *entity.AssistantPlan, sequence int64) {
		result.Turns = append(result.Turns, &controlplanev1.AssistantTurn{Ref: plan.Ref, Sequence: sequence, Role: "ASSISTANT", Content: plan.Summary, State: "COMPLETED", Plan: castPlan(plan), CreatedAt: timestamp(plan.CreatedAt), Source: castMessageSource(entity.MessageSource{Origin: "ORDINARY"})})
	}
	for _, turn := range value.Turns {
		result.Turns = append(result.Turns, &controlplanev1.AssistantTurn{Ref: turn.Ref, Sequence: turn.Sequence, Role: publicAssistantTurnRole(turn.Actor), Content: turn.Content, State: turn.State, AttachmentSetRef: turn.AttachmentSetRef, RunRef: turn.RunRef, RunVersion: turn.RunVersion, CreatedAt: timestamp(turn.CreatedAt), Source: castMessageSource(turn.Source)})
		if turn.Sequence >= nextSequence {
			nextSequence = turn.Sequence + 1
		}
		// План создаётся во время tool-вызова, а итоговый текст ответа сохраняется
		// несколькими мгновениями позже. Поэтому показываем каждый сохранённый
		// вариант сразу после первого следующего ответа помощника, а не общей
		// пачкой в конце истории.
		if allPlansLoaded && publicAssistantTurnRole(turn.Actor) == "ASSISTANT" {
			for nextPlan < len(plans) && !plans[nextPlan].CreatedAt.IsZero() && !plans[nextPlan].CreatedAt.After(turn.CreatedAt) {
				appendPlan(&plans[nextPlan], turn.Sequence)
				nextPlan++
			}
		}
	}
	for nextPlan < len(plans) {
		appendPlan(&plans[nextPlan], nextSequence)
		nextSequence++
		nextPlan++
	}
	return result
}

// castMessageSource maps the exact MESSAGE_ORIGIN-prefixed source to its enum with zero fallback, without creating source provenance.
// castMessageSource переводит точный источник MESSAGE_ORIGIN в enum с нулевым запасным значением, не создавая происхождение источника.
func castMessageSource(source entity.MessageSource) *controlplanev1.MessageSource {
	return &controlplanev1.MessageSource{Origin: controlplanev1.MessageOrigin(controlplanev1.MessageOrigin_value["MESSAGE_ORIGIN_"+source.Origin])}
}

// publicAssistantTurnRole preserves USER/ASSISTANT/SYSTEM_RECEIPT, maps SYSTEM_ASSISTANT to ASSISTANT and maps every unknown actor to SYSTEM_RECEIPT rather than presenting it as a user.
// publicAssistantTurnRole сохраняет USER/ASSISTANT/SYSTEM_RECEIPT, переводит SYSTEM_ASSISTANT в ASSISTANT, а неизвестного автора в SYSTEM_RECEIPT, не показывая его пользователем.
func publicAssistantTurnRole(role string) string {
	switch role {
	case "USER", "ASSISTANT", "SYSTEM_RECEIPT":
		return role
	case "SYSTEM_ASSISTANT":
		return "ASSISTANT"
	default:
		return "SYSTEM_RECEIPT"
	}
}

// castPlanReceipt preserves nil receipt presence and projects operation/resource/audit pins, optional artifact revisions and conflicts; unsupported conflict values may remain nil protobuf Values because conversion errors are ignored.
// castPlanReceipt сохраняет отсутствие квитанции при nil и отображает привязки операций, ресурсов и аудита, необязательные ревизии файлов и конфликты; неподдерживаемые значения конфликта могут остаться nil protobuf Value, поскольку ошибки преобразования игнорируются.
func castPlanReceipt(value *entity.AssistantPlanReceipt) *controlplanev1.AssistantPlanReceipt {
	if value == nil {
		return nil
	}
	result := &controlplanev1.AssistantPlanReceipt{Ref: value.Ref, PlanRef: value.PlanRef, PlanRevision: value.PlanRevision,
		Outcome: value.Outcome, AuditRefs: append([]string(nil), value.AuditRefs...), CreatedResourceRefs: append([]string(nil), value.CreatedResourceRefs...), CreatedAt: timestamp(value.CreatedAt)}
	for _, operation := range value.Operations {
		receipt := &controlplanev1.AssistantPlanOperationReceipt{OperationRef: operation.OperationRef,
			ResourceRef: operation.ResourceRef, Outcome: operation.Outcome, AuditRef: operation.AuditRef}
		if operation.ArtifactRevision != nil {
			receipt.ArtifactRevision = castArtifactRevision(*operation.ArtifactRevision)
		}
		result.Operations = append(result.Operations, receipt)
	}
	for _, conflict := range value.Conflicts {
		expected, _ := structpb.NewValue(conflict.Expected)
		actual, _ := structpb.NewValue(conflict.Actual)
		result.Conflicts = append(result.Conflicts, &controlplanev1.AssistantPlanConflict{OperationRef: conflict.OperationRef,
			TargetRef: conflict.TargetRef, Field: conflict.Field, Expected: expected, Actual: actual})
	}
	return result
}

// castAssistant projects supplied assistant runtime/instruction metadata and derives a display summary with BUSY before Ready before recovering fallback; it does not probe health or change runtime state.
// castAssistant отображает переданные метаданные runtime и инструкций помощника и выводит подпись с приоритетом BUSY, затем Ready, затем запасным восстановлением; здоровье не проверяет и состояние runtime не меняет.
func castAssistant(value entity.SystemAssistant) *controlplanev1.SystemAssistant {
	summary := "i18n:ASSISTANT_RECOVERING"
	if value.RuntimeState == "BUSY" {
		summary = "i18n:ASSISTANT_EXECUTING"
	} else if value.Ready {
		summary = "i18n:ASSISTANT_READY"
	}
	return &controlplanev1.SystemAssistant{Ref: value.Ref, Version: value.Version, Name: value.Name, SystemLabel: "i18n:SYSTEM_ASSISTANT_LABEL", CorePromptRevision: value.CorePromptRevision, OwnerInstructions: value.OwnerInstructions, OwnerInstructionsRevision: int32(value.Version), RuntimeState: assistantState(value.RuntimeState), RuntimeRevision: value.RuntimeRevision, WarmSessionRef: value.WarmSessionRef, ReadinessSummary: summary, LastHeartbeatAt: optionalTimestamp(value.LastHeartbeatAt), NextActions: nextActions(value.NextActions)}
}

// castAudit projects authorized audit actor/executor/resource/outcome metadata and safe summary without creating an audit record or exposing internal actor IDs.
// castAudit отображает авторизованные метаданные автора, исполнителя, ресурса и результата аудита и безопасный текст без создания записи аудита и раскрытия внутренних ID автора.
func castAudit(value entity.AuditEvent) *controlplanev1.AuditEvent {
	return &controlplanev1.AuditEvent{Ref: value.Ref, ProjectRef: value.ProjectRef, Initiator: &controlplanev1.UserSummary{Ref: value.ActorRef, DisplayName: value.ActorName}, Executor: value.Executor, Source: value.Source, Action: value.Action, ResourceType: value.ResourceKind, ResourceRef: value.ResourceRef, ResourceName: value.ResourceName, Outcome: value.Outcome, SafeSummary: value.Summary, OccurredAt: timestamp(value.OccurredAt)}
}

// castIncident projects an incident's scope, severity, state and supplied safe summary/next step; classification and remediation remain with the incident owner.
// castIncident отображает область, серьёзность, состояние инцидента и переданные безопасные описание и следующий шаг; классификацию и исправление выполняет владелец инцидента.
func castIncident(value entity.Incident) *controlplanev1.Incident {
	return &controlplanev1.Incident{Ref: value.Ref, ProjectRef: value.ProjectRef, RunRef: value.RunRef, Category: value.Category, Severity: value.Severity, State: value.State, SafeSummary: value.SafeSummary, SafeNextStep: value.SafeNextStep, CoreAffected: value.CoreAffected, CreatedAt: timestamp(value.CreatedAt)}
}

// castBootstrap projects initialization/onboarding, resolved actor/role and assistant readiness, while forwarding STT eligibility/reason with Available explicitly false; speech readiness is not inferred here.
// castBootstrap отображает инициализацию, первичную настройку, разрешённых пользователя и роль и готовность помощника, передавая допустимость и причину STT с явно ложным Available; готовность распознавания речи здесь не выводит.
func castBootstrap(value repository.BootstrapState) *controlplanev1.BootstrapState {
	return &controlplanev1.BootstrapState{OrganizationRef: value.OrganizationRef, Initialized: value.Bootstrapped, OnboardingComplete: value.OnboardingCompleted, WebOnlyReady: value.Assistant.Ready, Assistant: castAssistant(value.Assistant), CurrentUser: castUser(value.Actor), PlatformRole: platformRole(value.PlatformRole), NextActions: nextActions(value.NextActions),
		SpeechTranscription: &controlplanev1.SpeechTranscriptionAvailability{Eligible: value.SpeechTranscription.Eligible, Available: false, Reason: value.SpeechTranscription.Reason}}
}

// castOverview projects owner-supplied overview counters and ordered active-run/pending-gate/recent-artifact lists without performing new queries or recomputing eligibility.
// castOverview отображает счётчики обзора и упорядоченные списки активных запусков, ожидающих шлюзов и недавних файлов, переданные владельцем данных, без новых запросов или пересчёта допустимости.
func castOverview(value repository.Overview) *controlplanev1.Overview {
	result := &controlplanev1.Overview{ProjectCount: value.ProjectCount, AgentCount: value.AgentCount, ActiveRunCount: value.ActiveRunCount, PendingGateCount: value.PendingGateCount}
	for _, item := range value.ActiveRuns {
		result.ActiveRuns = append(result.ActiveRuns, castRun(item))
	}
	for _, item := range value.PendingGates {
		result.PendingGates = append(result.PendingGates, castGate(item))
	}
	for _, item := range value.RecentArtifacts {
		result.RecentArtifacts = append(result.RecentArtifacts, castArtifact(item))
	}
	return result
}

// castAdministration projects observed core/profile/assistant status and ordered optional adapter/incident lists; it neither performs readiness probes nor executes administration actions.
// castAdministration отображает наблюдённые состояние ядра, профиль и помощника и упорядоченные списки необязательных адаптеров и инцидентов; готовность не проверяет и административные действия не выполняет.
func castAdministration(value repository.Administration) *controlplanev1.AdministrationState {
	result := &controlplanev1.AdministrationState{Profile: value.Profile, CoreReady: value.CoreReady, CoreSummary: value.CoreSummary, Assistant: castAssistant(value.Assistant), ObservedAt: timestamp(value.ObservedAt)}
	for _, item := range value.OptionalAdapters {
		result.OptionalAdapters = append(result.OptionalAdapters, castDefinition(item))
	}
	for _, item := range value.Incidents {
		result.Incidents = append(result.Incidents, castIncident(item))
	}
	return result
}
