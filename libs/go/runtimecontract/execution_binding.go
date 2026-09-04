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
	Provider                    string                    `json:"provider"`
	Model                       string                    `json:"model"`
	Instructions                string                    `json:"instructions"`
	InstructionRef              string                    `json:"instruction_ref"`
	InstructionDigest           string                    `json:"instruction_digest"`
	PromptTemplateRef           string                    `json:"prompt_template_ref"`
	PromptTemplateDigest        string                    `json:"prompt_template_digest"`
	PromptMaterializationDigest string                    `json:"prompt_materialization_digest"`
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
	Capabilities                []string                  `json:"capabilities"`
	DelegationTargets           []RunnerDelegationTarget  `json:"delegation_targets"`
	IntegrationGrants           []RunnerIntegrationGrant  `json:"integration_grants"`
	AttachmentSets              []RunnerAttachmentSet     `json:"attachment_sets"`
	InputArtifacts              []RunnerInputArtifact     `json:"input_artifacts"`
	SessionContext              []RunnerSessionMessage    `json:"session_context"`
	BoundedInput                map[string]any            `json:"bounded_input"`
	WorkspacePolicy             RuntimeWorkspacePolicy    `json:"workspace_policy"`
	CodexSandbox                string                    `json:"codex_sandbox"`
	CodexApprovalPolicy         string                    `json:"codex_approval_policy"`
	CodexSessionID              string                    `json:"codex_session_id"`
}

func RuntimeExecutionBindingDigests(input RunnerInput) (string, string, error) {
	identity := runtimeExecutionIdentity{OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, RunRef: input.RunRef, NodeRef: input.NodeRef,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt, LeaseRef: input.LeaseRef,
		LeaseFence:      input.LeaseFence,
		LeaseGeneration: input.LeaseGeneration, RuntimeRevisionRef: input.RuntimeRevisionRef,
		RuntimeRevisionVersion: input.RuntimeRevisionVersion, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		InputDigest: input.InputDigest}
	if identity.OrganizationRef == "" || identity.ProjectRef == "" || identity.RunRef == "" || identity.NodeRef == "" || identity.SessionRef == "" ||
		identity.TurnRef == "" || identity.Attempt < 1 || identity.LeaseRef == "" || identity.LeaseFence == "" || identity.LeaseGeneration < 1 ||
		identity.RuntimeRevisionRef == "" || identity.RuntimeRevisionVersion < 1 || identity.RuntimeRevisionDigest == "" || identity.InputDigest == "" {
		return "", "", errors.New("runtime execution identity is incomplete")
	}
	materialization := runtimeMaterializationBinding{
		Identity: identity, Provider: input.Provider, Model: input.Model, Instructions: input.Instructions,
		InstructionRef: input.InstructionRef, InstructionDigest: input.InstructionDigest,
		PromptTemplateRef: input.PromptTemplateRef, PromptTemplateDigest: input.PromptTemplateDigest,
		PromptMaterializationDigest: input.PromptMaterializationDigest,
		RuntimeConfigRef:            input.RuntimeConfigRef, RuntimeConfigVersion: input.RuntimeConfigVersion, RuntimeConfigDigest: input.RuntimeConfigDigest,
		ProviderPolicyRef: input.ProviderPolicyRef, ProviderPolicyVersion: input.ProviderPolicyVersion, ProviderPolicyDigest: input.ProviderPolicyDigest,
		ConfigOverlayRef: input.ConfigOverlayRef, ConfigOverlayVersion: input.ConfigOverlayVersion,
		ConfigOverlayDigest: input.ConfigOverlayDigest, ConfigOverlay: input.ConfigOverlay,
		RuntimeEnvironmentRef: input.RuntimeEnvironmentRef, RuntimeEnvironmentVersion: input.RuntimeEnvironmentVersion,
		RuntimeEnvironmentDigest: input.RuntimeEnvironmentDigest, EnvironmentBindingRef: input.EnvironmentBindingRef,
		EnvironmentBindingVersion: input.EnvironmentBindingVersion, EnvironmentBindingDigest: input.EnvironmentBindingDigest,
		ImageReference: input.ImageReference, ImageManifestDigest: input.ImageManifestDigest, EnvironmentImage: input.EnvironmentImage,
		EnvironmentTools: input.EnvironmentTools, EnvironmentValues: input.EnvironmentValues, SecretProjections: input.SecretProjections,
		Capabilities: input.Capabilities, DelegationTargets: input.DelegationTargets, IntegrationGrants: input.IntegrationGrants,
		AttachmentSets: input.AttachmentSets, InputArtifacts: input.InputArtifacts, SessionContext: input.SessionContext,
		BoundedInput: input.BoundedInput, WorkspacePolicy: input.WorkspacePolicy, CodexSandbox: input.CodexSandbox,
		CodexApprovalPolicy: input.CodexApprovalPolicy, CodexSessionID: input.CodexSessionID,
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
		Identity          runtimeExecutionIdentity `json:"identity"`
		Capabilities      []string                 `json:"capabilities"`
		DelegationTargets []RunnerDelegationTarget `json:"delegation_targets"`
		Grants            []RunnerIntegrationGrant `json:"grants"`
	}{Identity: identity, Capabilities: input.Capabilities, DelegationTargets: input.DelegationTargets, Grants: input.IntegrationGrants})
	if err != nil {
		return "", "", errors.New("encode runtime MCP binding")
	}
	mcpSum := sha256.Sum256(mcpRaw)
	return hex.EncodeToString(executionSum[:]), hex.EncodeToString(mcpSum[:]), nil
}
