package runtimecontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// RunnerInputSchemaV9 pins the closed wire layout materialized for agent execution.
	// RunnerInputSchemaV9 закрепляет закрытую wire-структуру материализованного входа агента.
	RunnerInputSchemaV9 = "kodex.agent-runner-input.v9"
	// RunnerModeTurn selects an exact leased execution attempt with task and callback bindings.
	// RunnerModeTurn выбирает точную арендованную попытку исполнения с задачей и callback-привязками.
	RunnerModeTurn = "TURN"
	// RunnerModeWarm selects a system assistant runtime without per-turn execution authority.
	// RunnerModeWarm выбирает runtime системного помощника без полномочий конкретного хода.
	RunnerModeWarm = "WARM"
	// MaximumRunnerInputBytes bounds the complete serialized execution input.
	// MaximumRunnerInputBytes ограничивает полный сериализованный вход исполнения.
	MaximumRunnerInputBytes = 2 << 20
	// MaximumInputArtifactBytes bounds one declared immutable input artifact.
	// MaximumInputArtifactBytes ограничивает один объявленный неизменяемый входной артефакт.
	MaximumInputArtifactBytes = 512 << 20
	// MaximumInputArtifactTotal bounds the combined declared size of the input artifact catalog.
	// MaximumInputArtifactTotal ограничивает общий объявленный размер каталога входных артефактов.
	MaximumInputArtifactTotal = 512 << 20
	// MaximumCompletionBytes caps all inline completion artifacts together.
	// MaximumCompletionBytes ограничивает суммарный объём встроенных артефактов завершения.
	MaximumCompletionBytes = 16 << 20
	// MaximumCompletionFiles caps the number of artifacts returned by one completion.
	// MaximumCompletionFiles ограничивает количество артефактов одного завершения.
	MaximumCompletionFiles = 32
	// MaximumSessionSourceBytes bounds the declared source session archive size.
	// MaximumSessionSourceBytes ограничивает объявленный размер исходного архива сессии.
	MaximumSessionSourceBytes = 64 << 20
	// MaximumProgressTextBytes bounds the safe progress text published by a runner.
	// MaximumProgressTextBytes ограничивает безопасный текст прогресса, публикуемый runner.
	MaximumProgressTextBytes = 2 << 10
	// MaximumProviderAuthBytes bounds refreshed managed authentication bytes on the isolated relay path.
	// MaximumProviderAuthBytes ограничивает обновлённые байты авторизации на изолированном relay-пути.
	MaximumProviderAuthBytes = 1 << 20
	// ArtifactCapability names managed artifact write/catalog access, not an implicit input-file read grant.
	// ArtifactCapability называет право записи и каталога артефактов, а не неявное чтение входного файла.
	ArtifactCapability = "platform.artifact.manage"
)

// opaqueReferencePattern requires a bounded kind-prefixed reference without interpreting it as authority.
// opaqueReferencePattern требует ограниченную ссылку с префиксом вида, не трактуя её как полномочие.
var opaqueReferencePattern = regexp.MustCompile(`^[a-z][a-z0-9]{1,11}_[A-Za-z0-9_-]{8,84}$`)

// runtimeProfileKeyPattern validates the stable profile key used by RuntimeSelection.ref,
// whose external contract is distinct from a kind-prefixed aggregate reference.
// runtimeProfileKeyPattern проверяет постоянный ключ профиля RuntimeSelection.ref,
// внешний контракт которого отличается от ссылки агрегата с префиксом вида.
var runtimeProfileKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// validRuntimeProfileKey applies the stable profile-key contract without coercing it to an aggregate ID.
// validRuntimeProfileKey проверяет постоянный ключ профиля без преобразования в идентификатор агрегата.
func validRuntimeProfileKey(value string) bool { return runtimeProfileKeyPattern.MatchString(value) }

// imageDigestPattern requires an immutable sha256-prefixed lowercase image digest.
// imageDigestPattern требует неизменный строчный дайджест образа с префиксом sha256.
var imageDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// systemRuntimeRevisionPattern admits only the registered system-assistant revision naming forms.
// systemRuntimeRevisionPattern допускает лишь зарегистрированные формы имён версии системного помощника.
var systemRuntimeRevisionPattern = regexp.MustCompile(`^system-assistant-(?:core-v[1-9][0-9]*|runtime-[a-f0-9]{64})$`)

// workflowStepKeyPattern bounds optional delegation step locators to the workflow key alphabet.
// workflowStepKeyPattern ограничивает необязательные ссылки шага делегирования алфавитом workflow-ключа.
var workflowStepKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,95}$`)

// integrationDefinitionVersionPattern requires a positive semantic major with explicit minor and patch components.
// integrationDefinitionVersionPattern требует положительную основную версию с явными minor и patch.
var integrationDefinitionVersionPattern = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+\.[0-9]+$`)

// RuntimeTLSBinding pins the callback transport profile, exact server name, and mounted TLS files.
// RuntimeTLSBinding закрепляет профиль callback-транспорта, точное имя сервера и смонтированные TLS-файлы.
type RuntimeTLSBinding struct {
	Profile         string `json:"profile,omitempty"`
	ServerName      string `json:"server_name"`
	CAFile          string `json:"ca_file"`
	CertificateFile string `json:"certificate_file"`
	PrivateKeyFile  string `json:"private_key_file"`
}

// RunnerSessionMessage carries a bounded role/content entry from authoritative session history.
// RunnerSessionMessage содержит ограниченную запись роли и текста авторитетной истории сессии.
type RunnerSessionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RunnerDelegationTarget describes one owner-selected child agent and optional workflow step;
// its locator does not independently authorize delegation.
// RunnerDelegationTarget описывает выбранного владельцем дочернего агента и необязательный шаг workflow;
// сама ссылка не разрешает делегирование.
type RunnerDelegationTarget struct {
	Ref              string `json:"ref"`
	Name             string `json:"name"`
	Purpose          string `json:"purpose"`
	RoleDescription  string `json:"role_description"`
	WorkflowStepKey  string `json:"workflow_step_key,omitempty"`
	WorkflowStepName string `json:"workflow_step_name,omitempty"`
	Instructions     string `json:"instructions,omitempty"`
	ExpectedResult   string `json:"expected_result,omitempty"`
}

// RunnerIntegrationGrant pins one typed capability to exact connection/definition/schema versions
// with a declared risk and approval policy, excluding integration credential values.
// RunnerIntegrationGrant закрепляет типизированную возможность за версиями подключения, определения и схемы,
// указывает риск и подтверждение, исключая значения учётных данных интеграции.
type RunnerIntegrationGrant struct {
	Ref                   string `json:"ref"`
	GrantVersion          int64  `json:"grant_version"`
	ConnectionRef         string `json:"connection_ref"`
	ConnectionVersion     int64  `json:"connection_version"`
	ApprovalPolicy        string `json:"approval_policy"`
	DefinitionKey         string `json:"definition_key"`
	ConnectionName        string `json:"connection_name"`
	CapabilityKey         string `json:"capability_key"`
	CapabilityName        string `json:"capability_name"`
	CapabilityDescription string `json:"capability_description"`
	Risk                  string `json:"risk"`
	DefinitionVersion     string `json:"definition_version"`
	DefinitionDigest      string `json:"definition_digest"`
	Operation             string `json:"operation"`
	InputSchema           string `json:"input_schema"`
	InputSchemaSHA256     string `json:"input_schema_sha256"`
}

// RunnerAssistantContext records the safe route/resource snapshot and owner-resolved plan operations
// for one assistant turn without granting authority through the selected screen.
// RunnerAssistantContext хранит безопасный снимок экрана и ресурса и разрешённые владельцем операции плана
// одного хода помощника, не выдавая полномочий через выбранный экран.
type RunnerAssistantContext struct {
	Route             string   `json:"route"`
	EntityKind        string   `json:"entity_kind"`
	EntityRef         string   `json:"entity_ref"`
	EntityName        string   `json:"entity_name"`
	EntityVersion     *int64   `json:"entity_version,omitempty"`
	AllowedOperations []string `json:"allowed_operations"`
}

// RunnerAttachmentSet binds one finalized set to its exact place in RuntimeRevision;
// scope, provenance, and turn association are supplied by the control plane.
// RunnerAttachmentSet связывает завершённый набор с точным местом в RuntimeRevision;
// область, происхождение и связь с ходом назначает control plane.
type RunnerAttachmentSet struct {
	Ref            string `json:"ref"`
	ManifestDigest string `json:"manifest_digest"`
	Purpose        string `json:"purpose"`
	Scope          string `json:"scope"`
	Provenance     string `json:"provenance"`
	TurnRef        string `json:"turn_ref,omitempty"`
}

// RunnerInputArtifact pins an input or knowledge artifact's exact revision, content, and provenance;
// bytes are delivered separately through the execution-scoped callback and verified by the runner.
// RunnerInputArtifact закрепляет точные версию, содержимое и происхождение входного или справочного артефакта;
// байты передаются отдельно через callback исполнения и проверяются runner.
type RunnerInputArtifact struct {
	Ref               string `json:"ref"`
	FileName          string `json:"file_name"`
	MediaType         string `json:"media_type"`
	Digest            string `json:"digest"`
	SizeBytes         int64  `json:"size_bytes"`
	Revision          int64  `json:"revision"`
	Version           int64  `json:"version"`
	Scope             string `json:"scope"`
	Position          int64  `json:"position"`
	Source            string `json:"source"`
	AttachmentSetRef  string `json:"attachment_set_ref,omitempty"`
	AttachmentPurpose string `json:"attachment_purpose"`
	Provenance        string `json:"provenance"`
}

// RunnerInput is the immutable materialization of one turn or warm system runtime, binding configuration,
// context, environment, provider, and callback pins; resource references are not bearer authority or secret values.
// RunnerInput — неизменная материализация хода или warm runtime со связями конфигурации, контекста,
// окружения, провайдера и callback; ссылки ресурсов не являются bearer-полномочиями или значениями секретов.
type RunnerInput struct {
	Schema                            string                    `json:"schema"`
	Mode                              string                    `json:"mode"`
	WorkloadInstance                  string                    `json:"workload_instance"`
	OrganizationRef                   string                    `json:"organization_ref"`
	RunRef                            string                    `json:"run_ref,omitempty"`
	ProjectRef                        string                    `json:"project_ref,omitempty"`
	NodeRef                           string                    `json:"node_ref,omitempty"`
	SessionRef                        string                    `json:"session_ref"`
	TurnRef                           string                    `json:"turn_ref,omitempty"`
	AgentRef                          string                    `json:"agent_ref"`
	Attempt                           int32                     `json:"attempt,omitempty"`
	LeaseRef                          string                    `json:"lease_ref,omitempty"`
	LeaseFence                        string                    `json:"lease_fence,omitempty"`
	LeaseGeneration                   int64                     `json:"lease_generation,omitempty"`
	InputDigest                       string                    `json:"input_digest,omitempty"`
	RuntimeRevisionRef                string                    `json:"runtime_revision_ref"`
	RuntimeRevisionVersion            int64                     `json:"runtime_revision_version"`
	RuntimeRevisionDigest             string                    `json:"runtime_revision_digest"`
	ImageReference                    string                    `json:"image_reference"`
	ImageManifestDigest               string                    `json:"image_manifest_digest"`
	EnvironmentImage                  RuntimeEnvironmentImage   `json:"environment_image"`
	EnvironmentTools                  []RuntimeEnvironmentTool  `json:"environment_tools,omitempty"`
	RoleRuntimeContractRevision       uint64                    `json:"role_runtime_contract_revision"`
	RoleRuntimeContractSHA256         string                    `json:"role_runtime_contract_sha256"`
	RoleDefinitionRef                 string                    `json:"role_definition_ref"`
	RuntimeProfileRef                 string                    `json:"runtime_profile_ref"`
	RuntimeProfileRevision            string                    `json:"runtime_profile_revision"`
	InstructionRef                    string                    `json:"instruction_ref"`
	InstructionDigest                 string                    `json:"instruction_digest"`
	PromptTemplateRef                 string                    `json:"prompt_template_ref"`
	PromptTemplateDigest              string                    `json:"prompt_template_digest"`
	PromptMaterializationDigest       string                    `json:"prompt_materialization_digest"`
	PromptServiceTemplateRevision     string                    `json:"prompt_service_template_revision,omitempty"`
	PromptServiceTemplateDigest       string                    `json:"prompt_service_template_digest,omitempty"`
	PromptTargetKind                  string                    `json:"prompt_target_kind,omitempty"`
	SystemSTTConfigurationRef         string                    `json:"system_stt_configuration_ref,omitempty"`
	SystemSTTConfigurationRevisionRef string                    `json:"system_stt_configuration_revision_ref,omitempty"`
	SystemSTTConfigurationVersion     int64                     `json:"system_stt_configuration_version,omitempty"`
	SystemSTTConfigurationDigest      string                    `json:"system_stt_configuration_digest,omitempty"`
	ExecutionBindingDigest            string                    `json:"execution_binding_digest,omitempty"`
	MCPBindingDigest                  string                    `json:"mcp_binding_digest,omitempty"`
	AssistantScope                    AssistantScope            `json:"assistant_scope"`
	AssistantProfileRef               string                    `json:"assistant_profile_ref,omitempty"`
	Instructions                      string                    `json:"instructions"`
	Task                              string                    `json:"task,omitempty"`
	BoundedInput                      map[string]any            `json:"bounded_input,omitempty"`
	SessionContext                    []RunnerSessionMessage    `json:"session_context,omitempty"`
	DelegationTargets                 []RunnerDelegationTarget  `json:"delegation_targets,omitempty"`
	IntegrationGrants                 []RunnerIntegrationGrant  `json:"integration_grants,omitempty"`
	ManagedMCPProfiles                []ManagedMCPProfile       `json:"managed_mcp_profiles,omitempty"`
	AssistantContext                  *RunnerAssistantContext   `json:"assistant_context,omitempty"`
	AttachmentSetRef                  string                    `json:"attachment_set_ref,omitempty"`
	AttachmentSetManifestDigest       string                    `json:"attachment_set_manifest_digest,omitempty"`
	AttachmentContext                 string                    `json:"attachment_context,omitempty"`
	AttachmentSets                    []RunnerAttachmentSet     `json:"attachment_sets,omitempty"`
	InputArtifacts                    []RunnerInputArtifact     `json:"input_artifacts,omitempty"`
	ContextSnapshot                   *RuntimeContextSnapshot   `json:"context_snapshot,omitempty"`
	FileCatalog                       *RuntimeFileCatalog       `json:"file_catalog,omitempty"`
	Capabilities                      []string                  `json:"capabilities,omitempty"`
	Provider                          string                    `json:"provider"`
	Model                             string                    `json:"model"`
	ProviderAccountRef                string                    `json:"provider_account_ref"`
	ProviderCredentialRef             string                    `json:"provider_credential_revision_ref"`
	ProviderCredentialRevision        int64                     `json:"provider_credential_revision"`
	ProviderCredentialSHA256          string                    `json:"provider_credential_sha256"`
	RuntimeConfigRef                  string                    `json:"runtime_config_ref"`
	RuntimeConfigVersion              int64                     `json:"runtime_config_version"`
	RuntimeConfigDigest               string                    `json:"runtime_config_digest"`
	ProviderPolicyRef                 string                    `json:"provider_policy_ref"`
	ProviderPolicyVersion             int64                     `json:"provider_policy_version"`
	ProviderPolicyDigest              string                    `json:"provider_policy_digest"`
	ConfigOverlayRef                  string                    `json:"config_overlay_ref"`
	ConfigOverlayVersion              int64                     `json:"config_overlay_version"`
	ConfigOverlayDigest               string                    `json:"config_overlay_digest"`
	ConfigOverlay                     string                    `json:"config_overlay"`
	EffectiveReasoningEffort          string                    `json:"effective_reasoning_effort"`
	ReasoningMode                     string                    `json:"reasoning_mode"`
	RuntimeEnvironmentRef             string                    `json:"runtime_environment_ref"`
	RuntimeEnvironmentVersion         int64                     `json:"runtime_environment_version"`
	RuntimeEnvironmentDigest          string                    `json:"runtime_environment_digest"`
	EnvironmentBindingRef             string                    `json:"environment_binding_ref"`
	EnvironmentBindingVersion         int64                     `json:"environment_binding_version"`
	EnvironmentBindingDigest          string                    `json:"environment_binding_digest"`
	EnvironmentValues                 []RuntimeEnvironmentValue `json:"environment_values,omitempty"`
	SecretProjections                 []RuntimeSecretProjection `json:"secret_projections,omitempty"`
	EnvironmentPolicy                 RuntimeEnvironmentPolicy  `json:"environment_policy"`
	WorkspacePolicy                   RuntimeWorkspacePolicy    `json:"workspace_policy"`
	ExecutionDeadline                 *RuntimeExecutionDeadline `json:"execution_deadline,omitempty"`
	EffectiveKubernetesAccess         RuntimeKubernetesAccess   `json:"effective_kubernetes_access"`
	CodexSandbox                      string                    `json:"codex_sandbox"`
	CodexApprovalPolicy               string                    `json:"codex_approval_policy"`
	CodexSessionID                    string                    `json:"codex_session_id,omitempty"`
	CallbackURL                       string                    `json:"callback_url"`
	CallbackTLS                       RuntimeTLSBinding         `json:"callback_tls"`
	ExecutionTicketFile               string                    `json:"execution_ticket_file"`
	ProviderAuthFile                  string                    `json:"provider_auth_file"`
	ProviderAuthSHA256File            string                    `json:"provider_auth_sha256_file"`
	WorkspaceRoot                     string                    `json:"workspace_root"`
	OutboxRoot                        string                    `json:"outbox_root"`
	CodexHome                         string                    `json:"codex_home"`
}

// RunnerProviderCredentialRefreshRequest carries refreshed managed authentication through the isolated
// UDS relay and execution callback; the control plane receives only metadata of the controller-created Secret.
// RunnerProviderCredentialRefreshRequest передаёт обновлённую управляемую авторизацию через изолированный
// UDS relay и callback исполнения; control plane получает лишь метаданные Secret, созданного контроллером.
type RunnerProviderCredentialRefreshRequest struct {
	RuntimeRevisionDigest         string `json:"runtime_revision_digest"`
	PreviousCredentialRevisionRef string `json:"previous_credential_revision_ref"`
	PreviousContentSHA256         string `json:"previous_content_sha256"`
	Authentication                []byte `json:"authentication"`
}

// Validate checks exact revision pins and bounded nonempty JSON-object authentication bytes before relay use.
// Validate проверяет точные версии и ограниченные непустые JSON-байты объекта авторизации до использования relay.
func (request RunnerProviderCredentialRefreshRequest) Validate() error {
	trimmed := bytes.TrimSpace(request.Authentication)
	if !sha256Pattern.MatchString(request.RuntimeRevisionDigest) ||
		!opaqueReferencePattern.MatchString(request.PreviousCredentialRevisionRef) ||
		!sha256Pattern.MatchString(request.PreviousContentSHA256) ||
		len(request.Authentication) == 0 || len(request.Authentication) > MaximumProviderAuthBytes ||
		len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return errors.New("provider credential refresh request is invalid")
	}
	return nil
}

// Validate checks the complete materialization: scope, immutable pins, prompt/context, environment,
// callback, grants, and mode-specific execution identity. Current owner eligibility is verified separately.
// Validate проверяет полную материализацию: область, неизменные привязки, prompt и контекст, окружение,
// callback, разрешения и идентичность режима. Текущую допустимость владельца проверяют отдельно.
func (input RunnerInput) Validate() error {
	if input.validateAssistantScope() != nil {
		return errors.New("runner assistant scope binding is invalid")
	}
	if input.PromptServiceTemplateRevision != "" {
		if _, err := DecodePromptService(input); err != nil {
			return err
		}
	} else if input.PromptServiceTemplateDigest != "" || input.PromptTargetKind != "" {
		return errPromptService
	}
	if input.ContextSnapshot != nil && input.ContextSnapshot.ValidateFor(input, time.Now()) != nil {
		return ErrRuntimeContext
	}
	if input.Schema != RunnerInputSchemaV9 || (input.Mode != RunnerModeTurn && input.Mode != RunnerModeWarm) ||
		input.WorkloadInstance == "" || len(input.WorkloadInstance) > 128 || !opaqueReferencePattern.MatchString(input.OrganizationRef) ||
		!opaqueReferencePattern.MatchString(input.SessionRef) || !opaqueReferencePattern.MatchString(input.AgentRef) ||
		!(opaqueReferencePattern.MatchString(input.RuntimeRevisionRef) || systemRuntimeRevisionPattern.MatchString(input.RuntimeRevisionRef)) || input.RuntimeRevisionVersion < 1 ||
		!sha256Pattern.MatchString(input.RuntimeRevisionDigest) || !validPinnedImage(input.ImageReference, input.ImageManifestDigest) ||
		input.RoleRuntimeContractRevision == 0 || !sha256Pattern.MatchString(input.RoleRuntimeContractSHA256) ||
		!opaqueReferencePattern.MatchString(input.RoleDefinitionRef) || !validRuntimeProfileKey(input.RuntimeProfileRef) ||
		input.RuntimeProfileRevision == "" || len(input.RuntimeProfileRevision) > 128 ||
		!opaqueReferencePattern.MatchString(input.InstructionRef) || !sha256Pattern.MatchString(input.InstructionDigest) ||
		!opaqueReferencePattern.MatchString(input.PromptTemplateRef) || !sha256Pattern.MatchString(input.PromptTemplateDigest) ||
		!sha256Pattern.MatchString(input.PromptMaterializationDigest) ||
		strings.TrimSpace(input.Instructions) == "" || len(input.Instructions) > 1<<20 ||
		input.Provider == "" || len(input.Provider) > 64 || input.Model == "" || len(input.Model) > 128 ||
		!opaqueReferencePattern.MatchString(input.ProviderAccountRef) ||
		!opaqueReferencePattern.MatchString(input.ProviderCredentialRef) ||
		input.ProviderCredentialRevision < 1 || !sha256Pattern.MatchString(input.ProviderCredentialSHA256) ||
		!opaqueReferencePattern.MatchString(input.RuntimeConfigRef) || input.RuntimeConfigVersion < 1 || !sha256Pattern.MatchString(input.RuntimeConfigDigest) ||
		!opaqueReferencePattern.MatchString(input.ProviderPolicyRef) || input.ProviderPolicyVersion < 1 || !sha256Pattern.MatchString(input.ProviderPolicyDigest) ||
		!opaqueReferencePattern.MatchString(input.ConfigOverlayRef) || input.ConfigOverlayVersion < 1 || !sha256Pattern.MatchString(input.ConfigOverlayDigest) ||
		!opaqueReferencePattern.MatchString(input.RuntimeEnvironmentRef) || input.RuntimeEnvironmentVersion < 1 || !sha256Pattern.MatchString(input.RuntimeEnvironmentDigest) ||
		!opaqueReferencePattern.MatchString(input.EnvironmentBindingRef) || input.EnvironmentBindingVersion < 1 || !sha256Pattern.MatchString(input.EnvironmentBindingDigest) ||
		(input.CodexSandbox != "read-only" && input.CodexSandbox != "workspace-write") ||
		(input.CodexApprovalPolicy != "untrusted" && input.CodexApprovalPolicy != "on-request" && input.CodexApprovalPolicy != "never") ||
		(input.CodexSessionID != "" && !uuidPattern.MatchString(input.CodexSessionID)) ||
		input.ValidateCallbackTransport() != nil ||
		!validSecretFile(input.ExecutionTicketFile) || !validSecretFile(input.ProviderAuthFile) ||
		!validSecretFile(input.ProviderAuthSHA256File) || input.WorkspaceRoot != "/workspace" ||
		input.OutboxRoot != "/workspace/.kodex/outbox" || input.CodexHome != "/workspace/.kodex/state/codex-home" ||
		len(input.SessionContext) > 128 || len(input.DelegationTargets) > 128 || len(input.IntegrationGrants) > 256 || len(input.AttachmentSets) > 128 ||
		len(input.BoundedInput) > 256 || len(input.CodexSessionID) > 255 ||
		!validSessionContext(input) || !validIntegrationGrants(input.IntegrationGrants) ||
		!validCapabilities(input.Capabilities) || ValidateRuntimeEnvironment(input.EnvironmentValues, input.SecretProjections) != nil ||
		input.EnvironmentImage.Reference != input.ImageReference || input.EnvironmentImage.Digest != input.ImageManifestDigest ||
		(input.EnvironmentImage.ArtifactRef == "" || input.EnvironmentImage.RecipeRef == "" || input.EnvironmentImage.RecipeGeneration < 1) {
		return errors.New("runner input is invalid")
	}
	if err := ValidateManagedMCPProfiles(input); err != nil {
		return err
	}
	if err := input.ExecutionDeadline.Validate(); err != nil {
		return err
	}
	if input.ExecutionDeadline != nil && (input.Mode != RunnerModeTurn || input.AssistantScope != AssistantScopeNone) {
		return errors.New("workflow execution deadline scope is invalid")
	}
	usesSTT := containsString(input.Capabilities, "platform.stt.use")
	if usesSTT != (opaqueReferencePattern.MatchString(input.SystemSTTConfigurationRef) && opaqueReferencePattern.MatchString(input.SystemSTTConfigurationRevisionRef) && input.SystemSTTConfigurationVersion > 0 && sha256Pattern.MatchString(input.SystemSTTConfigurationDigest)) ||
		!usesSTT && (input.SystemSTTConfigurationRef != "" || input.SystemSTTConfigurationRevisionRef != "" || input.SystemSTTConfigurationVersion != 0 || input.SystemSTTConfigurationDigest != "") {
		return errors.New("runner STT configuration binding is invalid")
	}
	canonicalOverlay, overlayDigest, err := CanonicalConfigOverlay(input.ConfigOverlay)
	if ValidateEffectiveReasoningEffort(input.ConfigOverlay, input.EffectiveReasoningEffort, input.ReasoningMode) != nil {
		return errors.New("runner effective reasoning effort is invalid")
	}
	if err != nil || canonicalOverlay != input.ConfigOverlay || overlayDigest != input.ConfigOverlayDigest {
		return errors.New("runner config overlay binding is invalid")
	}
	normalizedPolicy, policyErr := NormalizeRuntimeEnvironmentPolicy(input.EnvironmentPolicy)
	if policyErr != nil || normalizedPolicy.ResourcesDigest != input.EnvironmentPolicy.ResourcesDigest ||
		normalizedPolicy.VolumesDigest != input.EnvironmentPolicy.VolumesDigest || normalizedPolicy.NetworkDigest != input.EnvironmentPolicy.NetworkDigest ||
		normalizedPolicy.RBACDigest != input.EnvironmentPolicy.RBACDigest || ValidateRuntimeKubernetesAccess(input.EffectiveKubernetesAccess) != nil ||
		input.EffectiveKubernetesAccess.Profile != normalizedPolicy.KubernetesAccess {
		return errors.New("runner environment policy binding is invalid")
	}
	expectedWorkspace, workspaceErr := RuntimeWorkspacePolicyWithLimits(normalizedPolicy.Resources.WorkspaceLimits)
	if workspaceErr != nil || input.WorkspacePolicy.Validate() != nil ||
		input.WorkspacePolicy.MaximumWritableBytes != expectedWorkspace.MaximumWritableBytes ||
		input.WorkspacePolicy.MaximumFileCount != expectedWorkspace.MaximumFileCount {
		return errors.New("runner workspace policy binding is invalid")
	}
	if input.FileCatalog != nil && (input.FileCatalog.Validate() != nil || input.Mode != RunnerModeTurn || input.ProjectRef == "") {
		return errors.New("runner file catalog binding is invalid")
	}
	environmentDigest, err := RuntimeEnvironmentDigest(input.EnvironmentValues, input.SecretProjections, input.EnvironmentImage, input.EnvironmentTools, normalizedPolicy)
	if err != nil || environmentDigest != input.RuntimeEnvironmentDigest {
		return errors.New("runner environment binding is invalid")
	}
	// Exact owner-selected artifacts и manifest являются отдельным immutable
	// read context grant; ArtifactCapability разрешает managed write/catalog.
	for _, grant := range input.IntegrationGrants {
		if !containsString(input.Capabilities, grant.CapabilityKey) {
			return errors.New("runner integration grant is outside effective capabilities")
		}
	}
	if input.ProjectRef != "" && !opaqueReferencePattern.MatchString(input.ProjectRef) {
		return errors.New("runner project binding is invalid")
	}
	if input.Mode == RunnerModeTurn {
		if !opaqueReferencePattern.MatchString(input.RunRef) || !opaqueReferencePattern.MatchString(input.NodeRef) ||
			!opaqueReferencePattern.MatchString(input.TurnRef) || input.Attempt < 1 ||
			!opaqueReferencePattern.MatchString(input.LeaseRef) || input.LeaseFence == "" ||
			len(input.LeaseFence) > 128 || input.LeaseGeneration < 1 || !sha256Pattern.MatchString(input.InputDigest) ||
			strings.TrimSpace(input.Task) == "" || len(input.Task) > 1<<20 {
			return errors.New("runner turn binding is invalid")
		}
		inputDigest, err := RuntimeBoundedInputDigest(input.BoundedInput)
		if err != nil || inputDigest != input.InputDigest {
			return errors.New("runner bounded input digest is invalid")
		}
		executionDigest, mcpDigest, err := RuntimeExecutionBindingDigests(input)
		if err != nil || input.ExecutionBindingDigest != executionDigest || input.MCPBindingDigest != mcpDigest {
			return errors.New("runner execution binding digest is invalid")
		}
	} else if input.RunRef != "" || input.NodeRef != "" || input.TurnRef != "" || input.LeaseRef != "" ||
		input.LeaseFence != "" || input.LeaseGeneration != 0 || input.Attempt != 0 || input.Task != "" || !input.IsSystemAssistant() ||
		input.ExecutionBindingDigest != "" || input.MCPBindingDigest != "" {
		return errors.New("warm runner binding is invalid")
	}
	for _, target := range input.DelegationTargets {
		if !opaqueReferencePattern.MatchString(target.Ref) || strings.TrimSpace(target.Name) == "" || !validRunnerHumanText(target.Name, 160) ||
			!validRunnerHumanText(target.Purpose, 2000) || !validRunnerHumanText(target.RoleDescription, 2000) || !validRunnerHumanText(target.WorkflowStepName, 160) ||
			!validRunnerHumanText(target.Instructions, 1000) || !validRunnerHumanText(target.ExpectedResult, 1000) ||
			(target.WorkflowStepKey != "" && !workflowStepKeyPattern.MatchString(target.WorkflowStepKey)) {
			return errors.New("runner delegation catalog is invalid")
		}
	}
	if input.AssistantContext != nil {
		context := input.AssistantContext
		if !input.IsAssistant() || len(context.Route) > 500 || len(context.EntityKind) > 80 ||
			len(context.EntityRef) > 96 || !validRunnerHumanText(context.EntityName, 300) || len(context.AllowedOperations) > 32 ||
			(context.EntityKind == "") != (context.EntityRef == "") || context.EntityVersion != nil && *context.EntityVersion < 1 {
			return errors.New("runner assistant context is invalid")
		}
		seen := make(map[string]struct{}, len(context.AllowedOperations))
		for _, operation := range context.AllowedOperations {
			if operation == "" || len(operation) > 80 {
				return errors.New("runner assistant context is invalid")
			}
			if _, duplicate := seen[operation]; duplicate {
				return errors.New("runner assistant context is invalid")
			}
			seen[operation] = struct{}{}
		}
	}
	currentSet, err := validateRunnerAttachmentSets(input.AttachmentSets, input.InputArtifacts)
	if err != nil {
		return err
	}
	if (currentSet != nil) != (opaqueReferencePattern.MatchString(input.AttachmentSetRef) && sha256Pattern.MatchString(input.AttachmentSetManifestDigest)) {
		return errors.New("runner attachment set binding is invalid")
	}
	if currentSet == nil && (input.AttachmentSetRef != "" || input.AttachmentSetManifestDigest != "") {
		return errors.New("runner attachment set binding is invalid")
	}
	if currentSet != nil && (currentSet.Ref != input.AttachmentSetRef || currentSet.ManifestDigest != input.AttachmentSetManifestDigest || currentSet.Purpose != input.AttachmentContext) {
		return errors.New("runner attachment set binding is invalid")
	}
	if currentSet != nil && !containsString([]string{"ASSISTANT_MESSAGE", "SESSION_TURN", "RUN_INPUT", "WORKFLOW_INPUT", "OWNER_GATE_MESSAGE"}, input.AttachmentContext) ||
		currentSet == nil && input.AttachmentContext != "" {
		return errors.New("runner attachment context is invalid")
	}
	return nil
}

// validRunnerHumanText counts Unicode code points while preserving empty optional values;
// invalid UTF-8 and NUL fail without normalization, independently of outer byte budgets.
// validRunnerHumanText считает Unicode-символы, сохраняя пустые необязательные значения;
// неверный UTF-8 и NUL отклоняются без нормализации независимо от внешних байтовых лимитов.
func validRunnerHumanText(text string, maximumCharacters int) bool {
	return utf8.ValidString(text) && !strings.ContainsRune(text, 0) && utf8.RuneCountInString(text) <= maximumCharacters
}

// containsString performs exact membership comparison without sorting or modifying the caller's slice.
// containsString проверяет точное вхождение без сортировки или изменения среза вызывающего кода.
func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

// validSessionContext applies the authoritative history/continuation shape validator as a boolean guard.
// validSessionContext применяет валидатор формы истории и продолжения как булеву проверку.
func validSessionContext(input RunnerInput) bool {
	_, err := ValidateRuntimeSessionContext(input)
	return err == nil
}

// validIntegrationGrants checks exact grant/schema pins, risk/approval compatibility, and unique
// grant identities and connection/capability pairs without resolving current owner permissions.
// validIntegrationGrants проверяет версии разрешений и схем, совместимость риска и подтверждения,
// уникальность разрешений и пар подключения и возможности без разрешения текущих прав владельца.
func validIntegrationGrants(grants []RunnerIntegrationGrant) bool {
	refs := make(map[string]struct{}, len(grants))
	bindings := make(map[string]struct{}, len(grants))
	for _, grant := range grants {
		if !opaqueReferencePattern.MatchString(grant.Ref) || !opaqueReferencePattern.MatchString(grant.ConnectionRef) ||
			grant.GrantVersion < 1 || grant.ConnectionVersion < 1 ||
			!containsString([]string{"NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"}, grant.ApprovalPolicy) ||
			grant.Risk == "READ" && grant.ApprovalPolicy != "NONE" ||
			grant.DefinitionKey == "" || len(grant.DefinitionKey) > 128 ||
			grant.ConnectionName == "" || len(grant.ConnectionName) > 160 ||
			grant.CapabilityKey == "" || len(grant.CapabilityKey) > 255 ||
			grant.CapabilityName == "" || len(grant.CapabilityName) > 160 ||
			len(grant.CapabilityDescription) > 2000 ||
			!integrationDefinitionVersionPattern.MatchString(grant.DefinitionVersion) || !sha256Pattern.MatchString(grant.DefinitionDigest) ||
			grant.Operation == "" || len(grant.Operation) > 255 || !sha256Pattern.MatchString(grant.InputSchemaSHA256) ||
			!validIntegrationInputSchema(grant.InputSchema, grant.InputSchemaSHA256) ||
			!containsString([]string{"READ", "WRITE", "SENSITIVE", "DESTRUCTIVE"}, grant.Risk) {
			return false
		}
		binding := grant.ConnectionRef + "\x00" + grant.CapabilityKey
		if _, duplicate := refs[grant.Ref]; duplicate {
			return false
		}
		if _, duplicate := bindings[binding]; duplicate {
			return false
		}
		refs[grant.Ref] = struct{}{}
		bindings[binding] = struct{}{}
	}
	return true
}

// validIntegrationInputSchema bounds valid JSON, requires a closed object-root declaration,
// and compares the exact supplied schema bytes with their pinned digest.
// validIntegrationInputSchema ограничивает корректный JSON, требует объявление закрытого корневого объекта
// и сравнивает точные переданные байты схемы с закреплённым дайджестом.
func validIntegrationInputSchema(raw, expectedDigest string) bool {
	if len(raw) == 0 || len(raw) > 64<<10 || !json.Valid([]byte(raw)) {
		return false
	}
	var schema map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&schema) != nil || schema["type"] != "object" || schema["additionalProperties"] != false {
		return false
	}
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:]) == expectedDigest
}

// validCapabilities limits count and byte length and rejects empty or duplicate capability keys.
// validCapabilities ограничивает количество и длину, отклоняя пустые или повторные ключи возможностей.
func validCapabilities(capabilities []string) bool {
	if len(capabilities) > 256 {
		return false
	}
	unique := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if capability == "" || len(capability) > 255 {
			return false
		}
		if _, exists := unique[capability]; exists {
			return false
		}
		unique[capability] = struct{}{}
	}
	return true
}

// validArtifactFileName accepts a bounded basename without separators, dot traversal names, or control delimiters.
// validArtifactFileName допускает ограниченное базовое имя без разделителей, имён перехода через точки и управляющих разделителей.
func validArtifactFileName(value string) bool {
	return value != "" && len(value) <= 255 && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00\r\n")
}

// EncodeRunnerInput serializes only a fully validated immutable execution materialization.
// EncodeRunnerInput сериализует лишь полностью проверенную неизменную материализацию исполнения.
func EncodeRunnerInput(input RunnerInput) ([]byte, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(input)
}

// DecodeRunnerInput bounds input bytes, rejects unknown fields and trailing values, then validates all bindings.
// DecodeRunnerInput ограничивает входные байты, отклоняет неизвестные поля и лишние значения и проверяет все привязки.
func DecodeRunnerInput(raw []byte) (RunnerInput, error) {
	if len(raw) == 0 || len(raw) > MaximumRunnerInputBytes {
		return RunnerInput{}, errors.New("runner input size is invalid")
	}
	var input RunnerInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) || input.Validate() != nil {
		return RunnerInput{}, errors.New("runner input is invalid")
	}
	return input, nil
}

// WarmCompatibilityDigest pins a system runtime's immutable environment and session boundary;
// turn-specific execution input remains part of the separate complete RuntimeRevision digest.
// WarmCompatibilityDigest закрепляет неизменное окружение системного runtime и границу сессии;
// вход конкретного хода остаётся частью отдельного полного дайджеста RuntimeRevision.
func WarmCompatibilityDigest(input RunnerInput) (string, error) {
	if !input.IsSystemAssistant() || input.validateAssistantScope() != nil {
		return "", errors.New("warm runtime compatibility requires system assistant")
	}
	capabilities := append([]string(nil), input.Capabilities...)
	sort.Strings(capabilities)
	payload := struct {
		AssistantScope                AssistantScope
		OrganizationRef               string
		SessionRef                    string
		AgentRef                      string
		ImageReference                string
		ImageManifestDigest           string
		EnvironmentImage              RuntimeEnvironmentImage
		EnvironmentTools              []RuntimeEnvironmentTool
		RoleRuntimeContractRevision   uint64
		RoleRuntimeContractSHA256     string
		RoleDefinitionRef             string
		RuntimeProfileRef             string
		RuntimeProfileRevision        string
		Instructions                  string
		InstructionRef                string
		InstructionDigest             string
		PromptTemplateRef             string
		PromptTemplateDigest          string
		PromptMaterializationDigest   string
		PromptServiceTemplateRevision string `json:",omitempty"`
		PromptServiceTemplateDigest   string `json:",omitempty"`
		PromptTargetKind              string `json:",omitempty"`
		SystemSTTConfigurationRef     string
		SystemSTTRevisionRef          string
		SystemSTTVersion              int64
		SystemSTTDigest               string
		Provider                      string
		Model                         string
		ProviderAccountRef            string
		ProviderCredentialRef         string
		ProviderCredentialRevision    int64
		ProviderCredentialSHA256      string
		CodexSandbox                  string
		CodexApprovalPolicy           string
		Capabilities                  []string
		RuntimeConfigRef              string
		RuntimeConfigVersion          int64
		RuntimeConfigDigest           string
		ProviderPolicyRef             string
		ProviderPolicyVersion         int64
		ProviderPolicyDigest          string
		ConfigOverlayRef              string
		ConfigOverlayVersion          int64
		ConfigOverlayDigest           string
		ConfigOverlay                 string
		EffectiveReasoningEffort      string
		ReasoningMode                 string
		RuntimeEnvironmentRef         string
		RuntimeEnvironmentVersion     int64
		RuntimeEnvironmentDigest      string
		EnvironmentBindingRef         string
		EnvironmentBindingVersion     int64
		EnvironmentBindingDigest      string
		EnvironmentValues             []RuntimeEnvironmentValue
		SecretProjections             []RuntimeSecretProjection
		EnvironmentPolicy             RuntimeEnvironmentPolicy
		WorkspacePolicy               RuntimeWorkspacePolicy
		ContextSnapshot               *RuntimeContextSnapshot
		KubernetesAccessProfile       RuntimeKubernetesAccessProfile
	}{
		AssistantScope:  input.AssistantScope,
		OrganizationRef: input.OrganizationRef, SessionRef: input.SessionRef, AgentRef: input.AgentRef,
		ImageReference: input.ImageReference, ImageManifestDigest: input.ImageManifestDigest,
		EnvironmentImage: input.EnvironmentImage, EnvironmentTools: input.EnvironmentTools,
		RoleRuntimeContractRevision: input.RoleRuntimeContractRevision,
		RoleRuntimeContractSHA256:   input.RoleRuntimeContractSHA256,
		RoleDefinitionRef:           input.RoleDefinitionRef, RuntimeProfileRef: input.RuntimeProfileRef,
		RuntimeProfileRevision: input.RuntimeProfileRevision, Instructions: input.Instructions,
		InstructionRef: input.InstructionRef, InstructionDigest: input.InstructionDigest,
		PromptTemplateRef: input.PromptTemplateRef, PromptTemplateDigest: input.PromptTemplateDigest,
		PromptMaterializationDigest:   input.PromptMaterializationDigest,
		PromptServiceTemplateRevision: input.PromptServiceTemplateRevision,
		PromptServiceTemplateDigest:   input.PromptServiceTemplateDigest,
		PromptTargetKind:              input.PromptTargetKind,
		SystemSTTConfigurationRef:     input.SystemSTTConfigurationRef, SystemSTTRevisionRef: input.SystemSTTConfigurationRevisionRef,
		SystemSTTVersion: input.SystemSTTConfigurationVersion, SystemSTTDigest: input.SystemSTTConfigurationDigest,
		Provider: input.Provider, Model: input.Model,
		ProviderAccountRef: input.ProviderAccountRef, ProviderCredentialRef: input.ProviderCredentialRef,
		ProviderCredentialRevision: input.ProviderCredentialRevision,
		ProviderCredentialSHA256:   input.ProviderCredentialSHA256,
		CodexSandbox:               input.CodexSandbox, CodexApprovalPolicy: input.CodexApprovalPolicy,
		Capabilities:     capabilities,
		RuntimeConfigRef: input.RuntimeConfigRef, RuntimeConfigVersion: input.RuntimeConfigVersion, RuntimeConfigDigest: input.RuntimeConfigDigest,
		ProviderPolicyRef: input.ProviderPolicyRef, ProviderPolicyVersion: input.ProviderPolicyVersion, ProviderPolicyDigest: input.ProviderPolicyDigest,
		ConfigOverlayRef: input.ConfigOverlayRef, ConfigOverlayVersion: input.ConfigOverlayVersion, ConfigOverlayDigest: input.ConfigOverlayDigest, ConfigOverlay: input.ConfigOverlay,
		EffectiveReasoningEffort: input.EffectiveReasoningEffort,
		ReasoningMode:            input.ReasoningMode,
		RuntimeEnvironmentRef:    input.RuntimeEnvironmentRef, RuntimeEnvironmentVersion: input.RuntimeEnvironmentVersion, RuntimeEnvironmentDigest: input.RuntimeEnvironmentDigest,
		EnvironmentBindingRef: input.EnvironmentBindingRef, EnvironmentBindingVersion: input.EnvironmentBindingVersion, EnvironmentBindingDigest: input.EnvironmentBindingDigest,
		EnvironmentValues: input.EnvironmentValues, SecretProjections: input.SecretProjections,
		EnvironmentPolicy: input.EnvironmentPolicy, WorkspacePolicy: input.WorkspacePolicy,
		ContextSnapshot:         input.ContextSnapshot,
		KubernetesAccessProfile: input.EffectiveKubernetesAccess.Profile,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", errors.New("encode warm runtime compatibility")
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// validate requires a cluster DNS server name and mounted secret paths, allowing only the CA file
// to come from the fixed configuration mount subtree.
// validate требует кластерное DNS-имя сервера и пути смонтированных секретов, допуская лишь CA-файл
// из фиксированной папки смонтированной конфигурации.
func (binding RuntimeTLSBinding) validate() error {
	if binding.ServerName == "" || len(binding.ServerName) > 253 || net.ParseIP(binding.ServerName) != nil ||
		!strings.HasSuffix(binding.ServerName, ".svc.cluster.local") {
		return errors.New("runtime TLS server name is invalid")
	}
	for _, path := range []string{binding.CAFile, binding.CertificateFile, binding.PrivateKeyFile} {
		if !validSecretFile(path) && !(path == binding.CAFile && filepath.IsAbs(path) && strings.HasPrefix(path, "/var/run/config/")) {
			return errors.New("runtime TLS file is invalid")
		}
	}
	return nil
}

// validCallbackURL checks the HTTPS callback origin and explicit port without userinfo, path, query, or fragment.
// validCallbackURL проверяет HTTPS-origin callback и явный порт без userinfo, пути, query и fragment.
func validCallbackURL(raw, serverName string) bool {
	parsed, err := url.Parse(raw)
	host := parsed.Hostname()
	return len(raw) <= 512 && err == nil && parsed.Scheme == "https" && (host == serverName || net.ParseIP(host) != nil) && parsed.Port() != "" &&
		parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.Path == ""
}

// validSecretFile restricts credential locators to bounded clean absolute paths in registered secret mounts.
// validSecretFile ограничивает ссылки учётных данных чистыми абсолютными путями внутри известных secret-mount.
func validSecretFile(path string) bool {
	return len(path) <= 512 && !strings.ContainsAny(path, "\x00\r\n") && filepath.IsAbs(path) && filepath.Clean(path) == path &&
		(strings.HasPrefix(path, "/var/run/secrets/") || strings.HasPrefix(path, "/run/secrets/"))
}

// validPinnedImage requires an immutable image reference ending in the exact canonical digest without template syntax.
// validPinnedImage требует неизменную ссылку образа с точным каноническим дайджестом без синтаксиса шаблонов.
func validPinnedImage(reference, digest string) bool {
	return len(reference) >= 73 && len(reference) <= 512 && !strings.ContainsAny(reference, " \t\r\n") &&
		imageDigestPattern.MatchString(digest) && strings.HasSuffix(reference, "@"+digest) &&
		!strings.Contains(reference, "$") && !strings.Contains(reference, "{") && !strings.Contains(reference, "}")
}

// RunnerProgressRequest binds safe progress and optional typed observations to one runtime revision digest.
// RunnerProgressRequest связывает безопасный прогресс и необязательные типизированные наблюдения с дайджестом runtime.
type RunnerProgressRequest struct {
	RuntimeRevisionDigest string                        `json:"runtime_revision_digest"`
	Progress              string                        `json:"progress"`
	Message               *RuntimeAgentMessage          `json:"message,omitempty"`
	ProviderProcess       *ProviderProcessObservation   `json:"provider_process,omitempty"`
	Financial             *ProviderFinancialObservation `json:"financial,omitempty"`
}

// RunnerArtifact carries one inline completion artifact with a basename, media type, and exact content digest.
// RunnerArtifact содержит встроенный артефакт завершения с базовым именем, типом и точным дайджестом содержимого.
type RunnerArtifact struct {
	FileName  string `json:"file_name"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
	Content   []byte `json:"content"`
}

// TokenUsage records measured per-turn provider usage; cached/cache-write input and reasoning output
// are subsets of input/output counters and are not added to the total again.
// TokenUsage хранит измеренный расход провайдера за ход; cached/cache-write и reasoning являются
// подмножествами input/output и не прибавляются к общему расходу повторно.
type TokenUsage struct {
	TotalTokens           int64 `json:"total_tokens"`
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	ModelContextWindow    int64 `json:"model_context_window"`
}

// Validate requires nonnegative counters, exact input-plus-output total, and bounded subcounters.
// Validate требует неотрицательные счётчики, точную сумму input и output и ограниченные подсчётчики.
func (usage TokenUsage) Validate() error {
	if usage.TotalTokens < 0 || usage.InputTokens < 0 || usage.CachedInputTokens < 0 ||
		usage.CacheWriteInputTokens < 0 || usage.OutputTokens < 0 || usage.ReasoningOutputTokens < 0 ||
		usage.ModelContextWindow < 0 || usage.TotalTokens != usage.InputTokens+usage.OutputTokens ||
		usage.CachedInputTokens > usage.InputTokens || usage.CacheWriteInputTokens > usage.InputTokens ||
		usage.ReasoningOutputTokens > usage.OutputTokens {
		return errors.New("token usage is invalid")
	}
	return nil
}

// RunnerCompletionRequest binds outcome, usage, inline artifacts, optional archive, and failure diagnostic
// to one exact runtime revision and attempt.
// RunnerCompletionRequest связывает исход, расход, встроенные артефакты, необязательный архив и диагностику
// ошибки с точной версией runtime и попыткой.
type RunnerCompletionRequest struct {
	RuntimeRevisionDigest string                     `json:"runtime_revision_digest"`
	Attempt               int32                      `json:"attempt"`
	Success               bool                       `json:"success"`
	ResultSummary         string                     `json:"result_summary"`
	SafeErrorCode         string                     `json:"safe_error_code,omitempty"`
	Usage                 TokenUsage                 `json:"usage"`
	Artifacts             []RunnerArtifact           `json:"artifacts,omitempty"`
	CodexSessionID        string                     `json:"codex_session_id,omitempty"`
	ArchiveRelativePath   string                     `json:"archive_relative_path,omitempty"`
	ArchiveSHA256         string                     `json:"archive_sha256,omitempty"`
	ArchiveSizeBytes      int64                      `json:"archive_size_bytes,omitempty"`
	ProviderDiagnostic    *ProviderFailureDiagnostic `json:"provider_diagnostic,omitempty"`
}

// Validate checks outcome/diagnostic consistency, token accounting, complete optional archive identity,
// artifact digests and names, and the aggregate completion byte budget.
// Validate проверяет согласованность исхода и диагностики, расход токенов, полную необязательную привязку архива,
// дайджесты и имена артефактов и общий байтовый бюджет завершения.
func (request RunnerCompletionRequest) Validate() error {
	if request.ProviderDiagnostic != nil && (request.Success || request.ProviderDiagnostic.Validate() != nil ||
		request.ProviderDiagnostic.RuntimeRevisionDigest != request.RuntimeRevisionDigest || request.ProviderDiagnostic.Attempt != request.Attempt) {
		return errProviderDiagnostic
	}
	if !sha256Pattern.MatchString(request.RuntimeRevisionDigest) || request.Attempt < 1 || len(request.ResultSummary) > 64<<10 ||
		len(request.SafeErrorCode) > 128 || len(request.Artifacts) > MaximumCompletionFiles ||
		(request.Success && strings.TrimSpace(request.ResultSummary) == "") || (!request.Success && request.SafeErrorCode == "") ||
		request.Usage.Validate() != nil {
		return errors.New("runner completion is invalid")
	}
	hasArchiveBinding := request.CodexSessionID != "" || request.ArchiveRelativePath != "" || request.ArchiveSHA256 != "" || request.ArchiveSizeBytes != 0
	if hasArchiveBinding && (ValidateCodexArchiveIdentity(request.CodexSessionID, request.ArchiveRelativePath) != nil ||
		!sha256Pattern.MatchString(request.ArchiveSHA256) ||
		request.ArchiveSizeBytes <= 0 || request.ArchiveSizeBytes > MaximumSessionSourceBytes) {
		return errors.New("runner completion archive binding is invalid")
	}
	total := 0
	for _, artifact := range request.Artifacts {
		digest := sha256Sum(artifact.Content)
		if artifact.FileName == "" || len(artifact.FileName) > 255 || strings.ContainsAny(artifact.FileName, "/\\\x00\r\n") ||
			artifact.MediaType == "" || len(artifact.MediaType) > 255 || !sha256Pattern.MatchString(artifact.SHA256) ||
			artifact.SHA256 != digest || len(artifact.Content) == 0 {
			return errors.New("runner artifact is invalid")
		}
		total += len(artifact.Content)
	}
	if total > MaximumCompletionBytes {
		return errors.New("runner completion budget exceeded")
	}
	return nil
}

// SessionPVCName canonically derives the runtime-owned PVC name from a valid opaque session reference.
// SessionPVCName канонически выводит имя PVC runtime из корректной непрозрачной ссылки сессии.
func SessionPVCName(sessionRef string) (string, error) {
	if !opaqueReferencePattern.MatchString(sessionRef) {
		return "", errors.New("session reference is invalid")
	}
	digest := sha256.Sum256([]byte(sessionRef))
	return "runtime-session-" + hex.EncodeToString(digest[:8]), nil
}

// sha256Sum returns the unprefixed lowercase SHA-256 digest of the exact supplied bytes.
// sha256Sum возвращает строчный SHA-256 без префикса для точных переданных байтов.
func sha256Sum(value []byte) string {
	digest := sha256.New()
	_, _ = digest.Write(value)
	return hex.EncodeToString(digest.Sum(nil))
}
