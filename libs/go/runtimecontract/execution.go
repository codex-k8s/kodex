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
)

const (
	RunnerInputSchemaV6       = "kodex.agent-runner-input.v6"
	RunnerInputSchemaV7       = "kodex.agent-runner-input.v7"
	RunnerModeTurn            = "TURN"
	RunnerModeWarm            = "WARM"
	MaximumRunnerInputBytes   = 2 << 20
	MaximumInputArtifactBytes = 512 << 20
	MaximumInputArtifactTotal = 512 << 20
	MaximumCompletionBytes    = 16 << 20
	MaximumCompletionFiles    = 32
	MaximumSessionSourceBytes = 64 << 20
	MaximumProgressTextBytes  = 2 << 10
	MaximumProviderAuthBytes  = 1 << 20
	ArtifactCapability        = "platform.artifact.manage"
)

var opaqueReferencePattern = regexp.MustCompile(`^[a-z][a-z0-9]{1,11}_[A-Za-z0-9_-]{8,84}$`)
var imageDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var systemRuntimeRevisionPattern = regexp.MustCompile(`^system-assistant-(?:core-v[1-9][0-9]*|runtime-[a-f0-9]{64})$`)
var workflowStepKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,95}$`)
var integrationDefinitionVersionPattern = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+\.[0-9]+$`)

// RuntimeTLSBinding описывает точную mTLS-границу callback runtime-controller.
type RuntimeTLSBinding struct {
	ServerName      string `json:"server_name"`
	CAFile          string `json:"ca_file"`
	CertificateFile string `json:"certificate_file"`
	PrivateKeyFile  string `json:"private_key_file"`
}

// RunnerSessionMessage — bounded часть авторитетной истории Session.
type RunnerSessionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RunnerDelegationTarget — закрытый server-owned каталог доступных дочерних агентов.
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

// RunnerIntegrationGrant — безопасная проекция одной типизированной capability.
type RunnerIntegrationGrant struct {
	Ref                   string `json:"ref"`
	ConnectionRef         string `json:"connection_ref"`
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

// RunnerAssistantContext — безопасный route/resource snapshot, который
// определяет доступный набор plan operations для одного assistant turn.
type RunnerAssistantContext struct {
	Route             string   `json:"route"`
	EntityKind        string   `json:"entity_kind"`
	EntityRef         string   `json:"entity_ref"`
	EntityName        string   `json:"entity_name"`
	EntityVersion     *int64   `json:"entity_version,omitempty"`
	AllowedOperations []string `json:"allowed_operations"`
}

// RunnerAttachmentSet связывает один finalized set с его точным местом в
// RuntimeRevision. Scope и provenance назначаются control-plane.
type RunnerAttachmentSet struct {
	Ref            string `json:"ref"`
	ManifestDigest string `json:"manifest_digest"`
	Purpose        string `json:"purpose"`
	Scope          string `json:"scope"`
	Provenance     string `json:"provenance"`
	TurnRef        string `json:"turn_ref,omitempty"`
}

// RunnerInputArtifact описывает exact версию входного либо knowledge artifact.
// Байты передаются отдельно через execution-scoped callback и сверяются runner.
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

// RunnerInput — immutable contract одного turn либо always-hot system runtime.
// В нём нет actor/organization authority и secret values.
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
	SystemSTTConfigurationRef         string                    `json:"system_stt_configuration_ref,omitempty"`
	SystemSTTConfigurationRevisionRef string                    `json:"system_stt_configuration_revision_ref,omitempty"`
	SystemSTTConfigurationVersion     int64                     `json:"system_stt_configuration_version,omitempty"`
	SystemSTTConfigurationDigest      string                    `json:"system_stt_configuration_digest,omitempty"`
	ExecutionBindingDigest            string                    `json:"execution_binding_digest,omitempty"`
	MCPBindingDigest                  string                    `json:"mcp_binding_digest,omitempty"`
	SystemAssistant                   bool                      `json:"system_assistant"`
	Instructions                      string                    `json:"instructions"`
	Task                              string                    `json:"task,omitempty"`
	BoundedInput                      map[string]any            `json:"bounded_input,omitempty"`
	SessionContext                    []RunnerSessionMessage    `json:"session_context,omitempty"`
	DelegationTargets                 []RunnerDelegationTarget  `json:"delegation_targets,omitempty"`
	IntegrationGrants                 []RunnerIntegrationGrant  `json:"integration_grants,omitempty"`
	AssistantContext                  *RunnerAssistantContext   `json:"assistant_context,omitempty"`
	AttachmentSetRef                  string                    `json:"attachment_set_ref,omitempty"`
	AttachmentSetManifestDigest       string                    `json:"attachment_set_manifest_digest,omitempty"`
	AttachmentContext                 string                    `json:"attachment_context,omitempty"`
	AttachmentSets                    []RunnerAttachmentSet     `json:"attachment_sets,omitempty"`
	InputArtifacts                    []RunnerInputArtifact     `json:"input_artifacts,omitempty"`
	ContextSnapshot                   *RuntimeContextSnapshot   `json:"context_snapshot,omitempty"`
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

// RunnerProviderCredentialRefreshRequest передает обновленный managed OAuth
// snapshot от provider к изолированному credential relay по UDS, а затем по
// execution-scoped mTLS callback. Control-plane получает исключительно
// метаданные созданной runtime-controller immutable Secret.
type RunnerProviderCredentialRefreshRequest struct {
	RuntimeRevisionDigest         string `json:"runtime_revision_digest"`
	PreviousCredentialRevisionRef string `json:"previous_credential_revision_ref"`
	PreviousContentSHA256         string `json:"previous_content_sha256"`
	Authentication                []byte `json:"authentication"`
}

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

func (input RunnerInput) Validate() error {
	if input.ContextSnapshot != nil && input.ContextSnapshot.ValidateFor(input, time.Now()) != nil {
		return ErrRuntimeContext
	}
	if input.Schema != RunnerInputSchemaV7 || (input.Mode != RunnerModeTurn && input.Mode != RunnerModeWarm) ||
		input.WorkloadInstance == "" || len(input.WorkloadInstance) > 128 || !opaqueReferencePattern.MatchString(input.OrganizationRef) ||
		!opaqueReferencePattern.MatchString(input.SessionRef) || !opaqueReferencePattern.MatchString(input.AgentRef) ||
		!(opaqueReferencePattern.MatchString(input.RuntimeRevisionRef) || systemRuntimeRevisionPattern.MatchString(input.RuntimeRevisionRef)) || input.RuntimeRevisionVersion < 1 ||
		!sha256Pattern.MatchString(input.RuntimeRevisionDigest) || !validPinnedImage(input.ImageReference, input.ImageManifestDigest) ||
		input.RoleRuntimeContractRevision == 0 || !sha256Pattern.MatchString(input.RoleRuntimeContractSHA256) ||
		!opaqueReferencePattern.MatchString(input.RoleDefinitionRef) || !opaqueReferencePattern.MatchString(input.RuntimeProfileRef) ||
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
		input.CallbackTLS.validate() != nil || !validCallbackURL(input.CallbackURL, input.CallbackTLS.ServerName) ||
		!validSecretFile(input.ExecutionTicketFile) || !validSecretFile(input.ProviderAuthFile) ||
		!validSecretFile(input.ProviderAuthSHA256File) || input.WorkspaceRoot != "/workspace" ||
		input.OutboxRoot != "/workspace/.kodex/outbox" || input.CodexHome != "/workspace/.kodex/state/codex-home" ||
		len(input.SessionContext) > 128 || len(input.DelegationTargets) > 128 || len(input.IntegrationGrants) > 256 || len(input.AttachmentSets) > 128 ||
		len(input.BoundedInput) > 256 || len(input.CodexSessionID) > 255 ||
		!validSessionContext(input.SessionContext) || !validIntegrationGrants(input.IntegrationGrants) ||
		!validCapabilities(input.Capabilities) || ValidateRuntimeEnvironment(input.EnvironmentValues, input.SecretProjections) != nil ||
		input.EnvironmentImage.Reference != input.ImageReference || input.EnvironmentImage.Digest != input.ImageManifestDigest ||
		(!input.SystemAssistant && (input.EnvironmentImage.ArtifactRef == "" || input.EnvironmentImage.RecipeRef == "" || input.EnvironmentImage.RecipeGeneration < 1)) {
		return errors.New("runner input is invalid")
	}
	usesSTT := containsString(input.Capabilities, "platform.stt.use")
	if usesSTT != (opaqueReferencePattern.MatchString(input.SystemSTTConfigurationRef) && opaqueReferencePattern.MatchString(input.SystemSTTConfigurationRevisionRef) && input.SystemSTTConfigurationVersion > 0 && sha256Pattern.MatchString(input.SystemSTTConfigurationDigest)) ||
		!usesSTT && (input.SystemSTTConfigurationRef != "" || input.SystemSTTConfigurationRevisionRef != "" || input.SystemSTTConfigurationVersion != 0 || input.SystemSTTConfigurationDigest != "") {
		return errors.New("runner STT configuration binding is invalid")
	}
	canonicalOverlay, overlayDigest, err := CanonicalConfigOverlay(input.ConfigOverlay)
	if ValidateEffectiveReasoningEffort(input.ConfigOverlay, input.EffectiveReasoningEffort) != nil {
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
	if input.WorkspacePolicy.Validate() != nil {
		return errors.New("runner workspace policy binding is invalid")
	}
	environmentDigest, err := RuntimeEnvironmentDigest(input.EnvironmentValues, input.SecretProjections, input.EnvironmentImage, input.EnvironmentTools, normalizedPolicy)
	if err != nil || environmentDigest != input.RuntimeEnvironmentDigest {
		return errors.New("runner environment binding is invalid")
	}
	if len(input.InputArtifacts) > 0 && !containsString(input.Capabilities, ArtifactCapability) {
		return errors.New("runner artifact capability is missing")
	}
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
		input.LeaseFence != "" || input.LeaseGeneration != 0 || input.Attempt != 0 || input.Task != "" || !input.SystemAssistant ||
		input.ExecutionBindingDigest != "" || input.MCPBindingDigest != "" {
		return errors.New("warm runner binding is invalid")
	}
	for _, target := range input.DelegationTargets {
		if !opaqueReferencePattern.MatchString(target.Ref) || strings.TrimSpace(target.Name) == "" || len(target.Name) > 160 ||
			len(target.Purpose) > 2000 || len(target.RoleDescription) > 2000 || len(target.WorkflowStepName) > 160 ||
			len(target.Instructions) > 1000 || len(target.ExpectedResult) > 1000 ||
			(target.WorkflowStepKey != "" && !workflowStepKeyPattern.MatchString(target.WorkflowStepKey)) {
			return errors.New("runner delegation catalog is invalid")
		}
	}
	if input.AssistantContext != nil {
		context := input.AssistantContext
		if !input.SystemAssistant || len(context.Route) > 500 || len(context.EntityKind) > 80 ||
			len(context.EntityRef) > 96 || len(context.EntityName) > 300 || len(context.AllowedOperations) > 32 ||
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

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func validSessionContext(messages []RunnerSessionMessage) bool {
	for _, message := range messages {
		if !containsString([]string{"USER", "ASSISTANT", "SYSTEM"}, message.Role) || len(message.Content) > 64<<10 {
			return false
		}
	}
	return true
}

func validIntegrationGrants(grants []RunnerIntegrationGrant) bool {
	refs := make(map[string]struct{}, len(grants))
	bindings := make(map[string]struct{}, len(grants))
	for _, grant := range grants {
		if !opaqueReferencePattern.MatchString(grant.Ref) || !opaqueReferencePattern.MatchString(grant.ConnectionRef) ||
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

func validArtifactFileName(value string) bool {
	return value != "" && len(value) <= 255 && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00\r\n")
}

func EncodeRunnerInput(input RunnerInput) ([]byte, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(input)
}

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

// WarmCompatibilityDigest связывает always-hot runtime только с полями,
// которые определяют его неизменяемое окружение и session boundary. Turn и
// execution input остаются частью полного RuntimeRevisionDigest.
func WarmCompatibilityDigest(input RunnerInput) (string, error) {
	if !input.SystemAssistant {
		return "", errors.New("warm runtime compatibility requires system assistant")
	}
	capabilities := append([]string(nil), input.Capabilities...)
	sort.Strings(capabilities)
	payload := struct {
		OrganizationRef             string
		SessionRef                  string
		AgentRef                    string
		ImageReference              string
		ImageManifestDigest         string
		EnvironmentImage            RuntimeEnvironmentImage
		EnvironmentTools            []RuntimeEnvironmentTool
		RoleRuntimeContractRevision uint64
		RoleRuntimeContractSHA256   string
		RoleDefinitionRef           string
		RuntimeProfileRef           string
		RuntimeProfileRevision      string
		Instructions                string
		InstructionRef              string
		InstructionDigest           string
		PromptTemplateRef           string
		PromptTemplateDigest        string
		PromptMaterializationDigest string
		SystemSTTConfigurationRef   string
		SystemSTTRevisionRef        string
		SystemSTTVersion            int64
		SystemSTTDigest             string
		Provider                    string
		Model                       string
		ProviderAccountRef          string
		ProviderCredentialRef       string
		ProviderCredentialRevision  int64
		ProviderCredentialSHA256    string
		CodexSandbox                string
		CodexApprovalPolicy         string
		Capabilities                []string
		RuntimeConfigRef            string
		RuntimeConfigVersion        int64
		RuntimeConfigDigest         string
		ProviderPolicyRef           string
		ProviderPolicyVersion       int64
		ProviderPolicyDigest        string
		ConfigOverlayRef            string
		ConfigOverlayVersion        int64
		ConfigOverlayDigest         string
		ConfigOverlay               string
		EffectiveReasoningEffort    string
		RuntimeEnvironmentRef       string
		RuntimeEnvironmentVersion   int64
		RuntimeEnvironmentDigest    string
		EnvironmentBindingRef       string
		EnvironmentBindingVersion   int64
		EnvironmentBindingDigest    string
		EnvironmentValues           []RuntimeEnvironmentValue
		SecretProjections           []RuntimeSecretProjection
		EnvironmentPolicy           RuntimeEnvironmentPolicy
		WorkspacePolicy             RuntimeWorkspacePolicy
		ContextSnapshot             *RuntimeContextSnapshot
		KubernetesAccessProfile     RuntimeKubernetesAccessProfile
	}{
		OrganizationRef: input.OrganizationRef, SessionRef: input.SessionRef, AgentRef: input.AgentRef,
		ImageReference: input.ImageReference, ImageManifestDigest: input.ImageManifestDigest,
		EnvironmentImage: input.EnvironmentImage, EnvironmentTools: input.EnvironmentTools,
		RoleRuntimeContractRevision: input.RoleRuntimeContractRevision,
		RoleRuntimeContractSHA256:   input.RoleRuntimeContractSHA256,
		RoleDefinitionRef:           input.RoleDefinitionRef, RuntimeProfileRef: input.RuntimeProfileRef,
		RuntimeProfileRevision: input.RuntimeProfileRevision, Instructions: input.Instructions,
		InstructionRef: input.InstructionRef, InstructionDigest: input.InstructionDigest,
		PromptTemplateRef: input.PromptTemplateRef, PromptTemplateDigest: input.PromptTemplateDigest,
		PromptMaterializationDigest: input.PromptMaterializationDigest,
		SystemSTTConfigurationRef:   input.SystemSTTConfigurationRef, SystemSTTRevisionRef: input.SystemSTTConfigurationRevisionRef,
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

func validCallbackURL(raw, serverName string) bool {
	parsed, err := url.Parse(raw)
	host := parsed.Hostname()
	return len(raw) <= 512 && err == nil && parsed.Scheme == "https" && (host == serverName || net.ParseIP(host) != nil) && parsed.Port() != "" &&
		parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.Path == ""
}

func validSecretFile(path string) bool {
	return len(path) <= 512 && !strings.ContainsAny(path, "\x00\r\n") && filepath.IsAbs(path) && filepath.Clean(path) == path &&
		(strings.HasPrefix(path, "/var/run/secrets/") || strings.HasPrefix(path, "/run/secrets/"))
}

func validPinnedImage(reference, digest string) bool {
	return len(reference) >= 73 && len(reference) <= 512 && !strings.ContainsAny(reference, " \t\r\n") &&
		imageDigestPattern.MatchString(digest) && strings.HasSuffix(reference, "@"+digest) &&
		!strings.Contains(reference, "$") && !strings.Contains(reference, "{") && !strings.Contains(reference, "}")
}

type RunnerProgressRequest struct {
	RuntimeRevisionDigest string `json:"runtime_revision_digest"`
	Progress              string `json:"progress"`
}

type RunnerArtifact struct {
	FileName  string `json:"file_name"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
	Content   []byte `json:"content"`
}

// TokenUsage — измеренный provider runtime расход одного turn.
// Cached/cache-write/reasoning входят соответственно в input/output и не
// прибавляются к total повторно.
type TokenUsage struct {
	TotalTokens           int64 `json:"total_tokens"`
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	ModelContextWindow    int64 `json:"model_context_window"`
}

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

type RunnerCompletionRequest struct {
	RuntimeRevisionDigest string           `json:"runtime_revision_digest"`
	Attempt               int32            `json:"attempt"`
	Success               bool             `json:"success"`
	ResultSummary         string           `json:"result_summary"`
	SafeErrorCode         string           `json:"safe_error_code,omitempty"`
	Usage                 TokenUsage       `json:"usage"`
	Artifacts             []RunnerArtifact `json:"artifacts,omitempty"`
	CodexSessionID        string           `json:"codex_session_id,omitempty"`
	ArchiveRelativePath   string           `json:"archive_relative_path,omitempty"`
	ArchiveSHA256         string           `json:"archive_sha256,omitempty"`
	ArchiveSizeBytes      int64            `json:"archive_size_bytes,omitempty"`
}

func (request RunnerCompletionRequest) Validate() error {
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

// SessionPVCName является единственным каноническим преобразованием opaque
// Session ref в имя принадлежащего runtime PVC.
func SessionPVCName(sessionRef string) (string, error) {
	if !opaqueReferencePattern.MatchString(sessionRef) {
		return "", errors.New("session reference is invalid")
	}
	digest := sha256.Sum256([]byte(sessionRef))
	return "runtime-session-" + hex.EncodeToString(digest[:8]), nil
}

func sha256Sum(value []byte) string {
	digest := sha256.New()
	_, _ = digest.Write(value)
	return hex.EncodeToString(digest.Sum(nil))
}
