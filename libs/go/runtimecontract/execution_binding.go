package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

var runtimeCallbackMethods = [...]string{
	"GET artifact",
	"POST complete",
	"POST mcp",
	"POST native-tool-call",
	"POST progress",
	"POST provider-credential-refresh",
}

type runtimeExecutionIdentity struct {
	Mode                   string `json:"mode"`
	WorkloadInstance       string `json:"workload_instance"`
	OrganizationRef        string `json:"organization_ref"`
	ProjectRef             string `json:"project_ref"`
	RunRef                 string `json:"run_ref"`
	NodeRef                string `json:"node_ref"`
	SessionRef             string `json:"session_ref"`
	TurnRef                string `json:"turn_ref"`
	Attempt                int32  `json:"attempt"`
	LeaseRef               string `json:"lease_ref"`
	LeaseFence             string `json:"lease_fence"`
	LeaseGeneration        int64  `json:"lease_generation"`
	RuntimeRevisionRef     string `json:"runtime_revision_ref"`
	RuntimeRevisionVersion int64  `json:"runtime_revision_version"`
	RuntimeRevisionDigest  string `json:"runtime_revision_digest"`
	InputDigest            string `json:"input_digest"`
}

type runtimeMaterializationBinding struct {
	Identity                    runtimeExecutionIdentity  `json:"identity"`
	AgentRef                    string                    `json:"agent_ref"`
	SystemAssistant             bool                      `json:"system_assistant"`
	RoleRuntimeContractRevision uint64                    `json:"role_runtime_contract_revision"`
	RoleRuntimeContractSHA256   string                    `json:"role_runtime_contract_sha256"`
	RoleDefinitionRef           string                    `json:"role_definition_ref"`
	RuntimeProfileRef           string                    `json:"runtime_profile_ref"`
	RuntimeProfileRevision      string                    `json:"runtime_profile_revision"`
	Provider                    string                    `json:"provider"`
	Model                       string                    `json:"model"`
	ProviderAccountRef          string                    `json:"provider_account_ref"`
	ProviderCredentialRef       string                    `json:"provider_credential_ref"`
	ProviderCredentialRevision  int64                     `json:"provider_credential_revision"`
	ProviderCredentialSHA256    string                    `json:"provider_credential_sha256"`
	Instructions                string                    `json:"instructions"`
	Task                        string                    `json:"task"`
	InstructionRef              string                    `json:"instruction_ref"`
	InstructionDigest           string                    `json:"instruction_digest"`
	PromptTemplateRef           string                    `json:"prompt_template_ref"`
	PromptTemplateDigest        string                    `json:"prompt_template_digest"`
	PromptMaterializationDigest string                    `json:"prompt_materialization_digest"`
	SystemSTTConfigurationRef   string                    `json:"system_stt_configuration_ref"`
	SystemSTTRevisionRef        string                    `json:"system_stt_configuration_revision_ref"`
	SystemSTTVersion            int64                     `json:"system_stt_configuration_version"`
	SystemSTTDigest             string                    `json:"system_stt_configuration_digest"`
	RuntimeConfigRef            string                    `json:"runtime_config_ref"`
	RuntimeConfigVersion        int64                     `json:"runtime_config_version"`
	RuntimeConfigDigest         string                    `json:"runtime_config_digest"`
	ProviderPolicyRef           string                    `json:"provider_policy_ref"`
	ProviderPolicyVersion       int64                     `json:"provider_policy_version"`
	ProviderPolicyDigest        string                    `json:"provider_policy_digest"`
	ConfigOverlayRef            string                    `json:"config_overlay_ref"`
	ConfigOverlayVersion        int64                     `json:"config_overlay_version"`
	ConfigOverlayDigest         string                    `json:"config_overlay_digest"`
	ConfigOverlay               string                    `json:"config_overlay"`
	RuntimeEnvironmentRef       string                    `json:"runtime_environment_ref"`
	RuntimeEnvironmentVersion   int64                     `json:"runtime_environment_version"`
	RuntimeEnvironmentDigest    string                    `json:"runtime_environment_digest"`
	EnvironmentBindingRef       string                    `json:"environment_binding_ref"`
	EnvironmentBindingVersion   int64                     `json:"environment_binding_version"`
	EnvironmentBindingDigest    string                    `json:"environment_binding_digest"`
	ImageReference              string                    `json:"image_reference"`
	ImageManifestDigest         string                    `json:"image_manifest_digest"`
	EnvironmentImage            RuntimeEnvironmentImage   `json:"environment_image"`
	EnvironmentTools            []RuntimeEnvironmentTool  `json:"environment_tools"`
	EnvironmentValues           []RuntimeEnvironmentValue `json:"environment_values"`
	SecretProjections           []RuntimeSecretProjection `json:"secret_projections"`
	EnvironmentPolicy           RuntimeEnvironmentPolicy  `json:"environment_policy"`
	EffectiveKubernetesAccess   RuntimeKubernetesAccess   `json:"effective_kubernetes_access"`
	Capabilities                []string                  `json:"capabilities"`
	DelegationTargets           []RunnerDelegationTarget  `json:"delegation_targets"`
	IntegrationGrants           []RunnerIntegrationGrant  `json:"integration_grants"`
	AttachmentSets              []RunnerAttachmentSet     `json:"attachment_sets"`
	InputArtifacts              []RunnerInputArtifact     `json:"input_artifacts"`
	SessionContext              []RunnerSessionMessage    `json:"session_context"`
	BoundedInput                map[string]any            `json:"bounded_input"`
	AssistantContext            *RunnerAssistantContext   `json:"assistant_context"`
	AttachmentSetRef            string                    `json:"attachment_set_ref"`
	AttachmentSetManifestDigest string                    `json:"attachment_set_manifest_digest"`
	AttachmentContext           string                    `json:"attachment_context"`
	WorkspacePolicy             RuntimeWorkspacePolicy    `json:"workspace_policy"`
	CodexSandbox                string                    `json:"codex_sandbox"`
	CodexApprovalPolicy         string                    `json:"codex_approval_policy"`
	CodexSessionID              string                    `json:"codex_session_id"`
	CallbackURL                 string                    `json:"callback_url"`
	CallbackTLS                 RuntimeTLSBinding         `json:"callback_tls"`
	ExecutionTicketFile         string                    `json:"execution_ticket_file"`
	ProviderAuthFile            string                    `json:"provider_auth_file"`
	ProviderAuthSHA256File      string                    `json:"provider_auth_sha256_file"`
	WorkspaceRoot               string                    `json:"workspace_root"`
	OutboxRoot                  string                    `json:"outbox_root"`
	CodexHome                   string                    `json:"codex_home"`
}

func RuntimeExecutionBindingDigests(input RunnerInput) (string, string, error) {
	identity := runtimeExecutionIdentity{Mode: input.Mode, WorkloadInstance: input.WorkloadInstance,
		OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, RunRef: input.RunRef, NodeRef: input.NodeRef,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt, LeaseRef: input.LeaseRef,
		LeaseFence:      input.LeaseFence,
		LeaseGeneration: input.LeaseGeneration, RuntimeRevisionRef: input.RuntimeRevisionRef,
		RuntimeRevisionVersion: input.RuntimeRevisionVersion, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		InputDigest: input.InputDigest}
	if identity.Mode != RunnerModeTurn || identity.WorkloadInstance == "" || identity.OrganizationRef == "" || identity.ProjectRef == "" ||
		identity.RunRef == "" || identity.NodeRef == "" || identity.SessionRef == "" ||
		identity.TurnRef == "" || identity.Attempt < 1 || identity.LeaseRef == "" || identity.LeaseFence == "" || identity.LeaseGeneration < 1 ||
		identity.RuntimeRevisionRef == "" || identity.RuntimeRevisionVersion < 1 || identity.RuntimeRevisionDigest == "" || identity.InputDigest == "" {
		return "", "", errors.New("runtime execution identity is incomplete")
	}
	materialization := runtimeMaterializationBinding{
		Identity: identity, AgentRef: input.AgentRef, SystemAssistant: input.SystemAssistant,
		RoleRuntimeContractRevision: input.RoleRuntimeContractRevision, RoleRuntimeContractSHA256: input.RoleRuntimeContractSHA256,
		RoleDefinitionRef: input.RoleDefinitionRef, RuntimeProfileRef: input.RuntimeProfileRef,
		RuntimeProfileRevision: input.RuntimeProfileRevision, Provider: input.Provider, Model: input.Model,
		ProviderAccountRef: input.ProviderAccountRef, ProviderCredentialRef: input.ProviderCredentialRef,
		ProviderCredentialRevision: input.ProviderCredentialRevision, ProviderCredentialSHA256: input.ProviderCredentialSHA256,
		Instructions: input.Instructions, Task: input.Task,
		InstructionRef: input.InstructionRef, InstructionDigest: input.InstructionDigest,
		PromptTemplateRef: input.PromptTemplateRef, PromptTemplateDigest: input.PromptTemplateDigest,
		PromptMaterializationDigest: input.PromptMaterializationDigest, SystemSTTConfigurationRef: input.SystemSTTConfigurationRef,
		SystemSTTRevisionRef: input.SystemSTTConfigurationRevisionRef, SystemSTTVersion: input.SystemSTTConfigurationVersion,
		SystemSTTDigest:  input.SystemSTTConfigurationDigest,
		RuntimeConfigRef: input.RuntimeConfigRef, RuntimeConfigVersion: input.RuntimeConfigVersion, RuntimeConfigDigest: input.RuntimeConfigDigest,
		ProviderPolicyRef: input.ProviderPolicyRef, ProviderPolicyVersion: input.ProviderPolicyVersion, ProviderPolicyDigest: input.ProviderPolicyDigest,
		ConfigOverlayRef: input.ConfigOverlayRef, ConfigOverlayVersion: input.ConfigOverlayVersion,
		ConfigOverlayDigest: input.ConfigOverlayDigest, ConfigOverlay: input.ConfigOverlay,
		RuntimeEnvironmentRef: input.RuntimeEnvironmentRef, RuntimeEnvironmentVersion: input.RuntimeEnvironmentVersion,
		RuntimeEnvironmentDigest: input.RuntimeEnvironmentDigest, EnvironmentBindingRef: input.EnvironmentBindingRef,
		EnvironmentBindingVersion: input.EnvironmentBindingVersion, EnvironmentBindingDigest: input.EnvironmentBindingDigest,
		ImageReference: input.ImageReference, ImageManifestDigest: input.ImageManifestDigest, EnvironmentImage: input.EnvironmentImage,
		EnvironmentTools: input.EnvironmentTools, EnvironmentValues: input.EnvironmentValues, SecretProjections: input.SecretProjections,
		EnvironmentPolicy: input.EnvironmentPolicy, EffectiveKubernetesAccess: input.EffectiveKubernetesAccess,
		Capabilities: input.Capabilities, DelegationTargets: input.DelegationTargets, IntegrationGrants: input.IntegrationGrants,
		AttachmentSets: input.AttachmentSets, InputArtifacts: input.InputArtifacts, SessionContext: input.SessionContext,
		BoundedInput: input.BoundedInput, AssistantContext: input.AssistantContext, AttachmentSetRef: input.AttachmentSetRef,
		AttachmentSetManifestDigest: input.AttachmentSetManifestDigest, AttachmentContext: input.AttachmentContext,
		WorkspacePolicy: input.WorkspacePolicy, CodexSandbox: input.CodexSandbox,
		CodexApprovalPolicy: input.CodexApprovalPolicy, CodexSessionID: input.CodexSessionID,
		CallbackURL: input.CallbackURL, CallbackTLS: input.CallbackTLS, ExecutionTicketFile: input.ExecutionTicketFile,
		ProviderAuthFile: input.ProviderAuthFile, ProviderAuthSHA256File: input.ProviderAuthSHA256File,
		WorkspaceRoot: input.WorkspaceRoot, OutboxRoot: input.OutboxRoot, CodexHome: input.CodexHome,
	}
	executionRaw, err := json.Marshal(struct {
		Materialization runtimeMaterializationBinding `json:"materialization"`
		Methods         []string                      `json:"methods"`
	}{Materialization: materialization, Methods: runtimeCallbackMethods[:]})
	if err != nil {
		return "", "", errors.New("encode runtime execution binding")
	}
	executionSum := sha256.Sum256(executionRaw)
	mcpRaw, err := json.Marshal(struct {
		Materialization runtimeMaterializationBinding `json:"materialization"`
		Method          string                        `json:"method"`
	}{Materialization: materialization, Method: "POST mcp"})
	if err != nil {
		return "", "", errors.New("encode runtime MCP binding")
	}
	mcpSum := sha256.Sum256(mcpRaw)
	return hex.EncodeToString(executionSum[:]), hex.EncodeToString(mcpSum[:]), nil
}
