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
)

const (
	RunnerInputSchemaV5       = "kodex.agent-runner-input.v5"
	RunnerModeTurn            = "TURN"
	RunnerModeWarm            = "WARM"
	MaximumRunnerInputBytes   = 2 << 20
	MaximumInputArtifactBytes = 16 << 20
	MaximumInputArtifacts     = 128
	MaximumInputArtifactTotal = 256 << 20
	MaximumCompletionBytes    = 16 << 20
	MaximumCompletionFiles    = 32
	MaximumProgressTextBytes  = 2 << 10
	ArtifactCapability        = "platform.artifact.manage"
)

var opaqueReferencePattern = regexp.MustCompile(`^[a-z][a-z0-9]{1,11}_[A-Za-z0-9_-]{8,84}$`)
var imageDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var systemRuntimeRevisionPattern = regexp.MustCompile(`^system-assistant-core-v[1-9][0-9]*$`)
var workflowStepKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,95}$`)

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
}

// RunnerInputArtifact описывает exact версию входного либо knowledge artifact.
// Байты передаются отдельно через execution-scoped callback и сверяются runner.
type RunnerInputArtifact struct {
	Ref       string `json:"ref"`
	FileName  string `json:"file_name"`
	MediaType string `json:"media_type"`
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"size_bytes"`
	Revision  int64  `json:"revision"`
	Version   int64  `json:"version"`
}

// RunnerInput — immutable contract одного turn либо always-hot system runtime.
// В нём нет actor/organization authority и secret values.
type RunnerInput struct {
	Schema                      string                   `json:"schema"`
	Mode                        string                   `json:"mode"`
	WorkloadInstance            string                   `json:"workload_instance"`
	RunRef                      string                   `json:"run_ref,omitempty"`
	ProjectRef                  string                   `json:"project_ref,omitempty"`
	NodeRef                     string                   `json:"node_ref,omitempty"`
	SessionRef                  string                   `json:"session_ref"`
	TurnRef                     string                   `json:"turn_ref,omitempty"`
	AgentRef                    string                   `json:"agent_ref"`
	Attempt                     int32                    `json:"attempt,omitempty"`
	LeaseRef                    string                   `json:"lease_ref,omitempty"`
	LeaseFence                  string                   `json:"lease_fence,omitempty"`
	LeaseGeneration             int64                    `json:"lease_generation,omitempty"`
	RuntimeRevisionRef          string                   `json:"runtime_revision_ref"`
	RuntimeRevisionVersion      int64                    `json:"runtime_revision_version"`
	RuntimeRevisionDigest       string                   `json:"runtime_revision_digest"`
	ImageReference              string                   `json:"image_reference"`
	ImageManifestDigest         string                   `json:"image_manifest_digest"`
	RoleRuntimeContractRevision uint64                   `json:"role_runtime_contract_revision"`
	RoleRuntimeContractSHA256   string                   `json:"role_runtime_contract_sha256"`
	SystemAssistant             bool                     `json:"system_assistant"`
	Instructions                string                   `json:"instructions"`
	Task                        string                   `json:"task,omitempty"`
	BoundedInput                map[string]any           `json:"bounded_input,omitempty"`
	SessionContext              []RunnerSessionMessage   `json:"session_context,omitempty"`
	DelegationTargets           []RunnerDelegationTarget `json:"delegation_targets,omitempty"`
	IntegrationGrants           []RunnerIntegrationGrant `json:"integration_grants,omitempty"`
	InputArtifacts              []RunnerInputArtifact    `json:"input_artifacts,omitempty"`
	Capabilities                []string                 `json:"capabilities,omitempty"`
	Provider                    string                   `json:"provider"`
	Model                       string                   `json:"model"`
	ProviderAccountRef          string                   `json:"provider_account_ref"`
	ProviderCredentialRef       string                   `json:"provider_credential_revision_ref"`
	ProviderCredentialRevision  int64                    `json:"provider_credential_revision"`
	ProviderCredentialSHA256    string                   `json:"provider_credential_sha256"`
	CodexSandbox                string                   `json:"codex_sandbox"`
	CodexApprovalPolicy         string                   `json:"codex_approval_policy"`
	CodexSessionID              string                   `json:"codex_session_id,omitempty"`
	CallbackURL                 string                   `json:"callback_url"`
	CallbackTLS                 RuntimeTLSBinding        `json:"callback_tls"`
	ExecutionTicketFile         string                   `json:"execution_ticket_file"`
	ProviderAuthFile            string                   `json:"provider_auth_file"`
	ProviderAuthSHA256File      string                   `json:"provider_auth_sha256_file"`
	WorkspaceRoot               string                   `json:"workspace_root"`
	OutboxRoot                  string                   `json:"outbox_root"`
	CodexHome                   string                   `json:"codex_home"`
}

func (input RunnerInput) Validate() error {
	if input.Schema != RunnerInputSchemaV5 || (input.Mode != RunnerModeTurn && input.Mode != RunnerModeWarm) ||
		input.WorkloadInstance == "" || len(input.WorkloadInstance) > 128 ||
		!opaqueReferencePattern.MatchString(input.SessionRef) || !opaqueReferencePattern.MatchString(input.AgentRef) ||
		!(opaqueReferencePattern.MatchString(input.RuntimeRevisionRef) || systemRuntimeRevisionPattern.MatchString(input.RuntimeRevisionRef)) || input.RuntimeRevisionVersion < 1 ||
		!sha256Pattern.MatchString(input.RuntimeRevisionDigest) || !validPinnedImage(input.ImageReference, input.ImageManifestDigest) ||
		input.RoleRuntimeContractRevision == 0 || !sha256Pattern.MatchString(input.RoleRuntimeContractSHA256) ||
		strings.TrimSpace(input.Instructions) == "" || len(input.Instructions) > 1<<20 ||
		input.Provider == "" || len(input.Provider) > 64 || input.Model == "" || len(input.Model) > 128 ||
		!opaqueReferencePattern.MatchString(input.ProviderAccountRef) ||
		!opaqueReferencePattern.MatchString(input.ProviderCredentialRef) ||
		input.ProviderCredentialRevision < 1 || !sha256Pattern.MatchString(input.ProviderCredentialSHA256) ||
		(input.CodexSandbox != "read-only" && input.CodexSandbox != "workspace-write") ||
		(input.CodexApprovalPolicy != "untrusted" && input.CodexApprovalPolicy != "on-request" && input.CodexApprovalPolicy != "never") ||
		input.CallbackTLS.validate() != nil || !validCallbackURL(input.CallbackURL, input.CallbackTLS.ServerName) ||
		!validSecretFile(input.ExecutionTicketFile) || !validSecretFile(input.ProviderAuthFile) ||
		!validSecretFile(input.ProviderAuthSHA256File) || input.WorkspaceRoot != "/workspace" ||
		input.OutboxRoot != "/workspace/.kodex/outbox" || input.CodexHome != "/workspace/.kodex/state/codex-home" ||
		len(input.SessionContext) > 128 || len(input.DelegationTargets) > 128 || len(input.IntegrationGrants) > 256 ||
		len(input.InputArtifacts) > MaximumInputArtifacts ||
		len(input.Capabilities) > 256 {
		return errors.New("runner input is invalid")
	}
	if len(input.InputArtifacts) > 0 && !containsString(input.Capabilities, ArtifactCapability) {
		return errors.New("runner artifact capability is missing")
	}
	if input.ProjectRef != "" && !opaqueReferencePattern.MatchString(input.ProjectRef) {
		return errors.New("runner project binding is invalid")
	}
	if input.Mode == RunnerModeTurn {
		if !opaqueReferencePattern.MatchString(input.RunRef) || !opaqueReferencePattern.MatchString(input.NodeRef) ||
			!opaqueReferencePattern.MatchString(input.TurnRef) || input.Attempt < 1 ||
			!opaqueReferencePattern.MatchString(input.LeaseRef) || input.LeaseFence == "" ||
			len(input.LeaseFence) > 128 || input.LeaseGeneration < 1 || strings.TrimSpace(input.Task) == "" || len(input.Task) > 1<<20 {
			return errors.New("runner turn binding is invalid")
		}
	} else if input.RunRef != "" || input.NodeRef != "" || input.TurnRef != "" || input.LeaseRef != "" ||
		input.LeaseFence != "" || input.LeaseGeneration != 0 || input.Attempt != 0 || input.Task != "" || !input.SystemAssistant {
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
	artifactRefs := make(map[string]struct{}, len(input.InputArtifacts))
	var artifactBytes int64
	for _, artifact := range input.InputArtifacts {
		if !opaqueReferencePattern.MatchString(artifact.Ref) || !validArtifactFileName(artifact.FileName) ||
			strings.TrimSpace(artifact.MediaType) == "" || len(artifact.MediaType) > 255 ||
			!imageDigestPattern.MatchString(artifact.Digest) || artifact.SizeBytes < 0 ||
			artifact.SizeBytes > MaximumInputArtifactBytes || artifact.Revision < 1 || artifact.Version < 1 {
			return errors.New("runner artifact catalog is invalid")
		}
		if _, exists := artifactRefs[artifact.Ref]; exists {
			return errors.New("runner artifact catalog is invalid")
		}
		artifactRefs[artifact.Ref] = struct{}{}
		artifactBytes += artifact.SizeBytes
		if artifactBytes > MaximumInputArtifactTotal {
			return errors.New("runner artifact catalog is invalid")
		}
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
// которые определяют его неизменяемое окружение. Идентификаторы Session,
// Turn и execution input остаются частью полного RuntimeRevisionDigest.
func WarmCompatibilityDigest(input RunnerInput) (string, error) {
	if !input.SystemAssistant {
		return "", errors.New("warm runtime compatibility requires system assistant")
	}
	capabilities := append([]string(nil), input.Capabilities...)
	sort.Strings(capabilities)
	payload := struct {
		ImageReference              string
		ImageManifestDigest         string
		RoleRuntimeContractRevision uint64
		RoleRuntimeContractSHA256   string
		Instructions                string
		Provider                    string
		Model                       string
		ProviderAccountRef          string
		ProviderCredentialRef       string
		ProviderCredentialRevision  int64
		ProviderCredentialSHA256    string
		CodexSandbox                string
		CodexApprovalPolicy         string
		Capabilities                []string
	}{
		ImageReference: input.ImageReference, ImageManifestDigest: input.ImageManifestDigest,
		RoleRuntimeContractRevision: input.RoleRuntimeContractRevision,
		RoleRuntimeContractSHA256:   input.RoleRuntimeContractSHA256,
		Instructions:                input.Instructions, Provider: input.Provider, Model: input.Model,
		ProviderAccountRef: input.ProviderAccountRef, ProviderCredentialRef: input.ProviderCredentialRef,
		ProviderCredentialRevision: input.ProviderCredentialRevision,
		ProviderCredentialSHA256:   input.ProviderCredentialSHA256,
		CodexSandbox:               input.CodexSandbox, CodexApprovalPolicy: input.CodexApprovalPolicy,
		Capabilities: capabilities,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", errors.New("encode warm runtime compatibility")
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (binding RuntimeTLSBinding) validate() error {
	if binding.ServerName == "" || net.ParseIP(binding.ServerName) != nil || !strings.HasSuffix(binding.ServerName, ".svc.cluster.local") {
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
	return err == nil && parsed.Scheme == "https" && (host == serverName || net.ParseIP(host) != nil) && parsed.Port() != "" &&
		parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.Path == ""
}

func validSecretFile(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path &&
		(strings.HasPrefix(path, "/var/run/secrets/") || strings.HasPrefix(path, "/run/secrets/"))
}

func validPinnedImage(reference, digest string) bool {
	return imageDigestPattern.MatchString(digest) && strings.HasSuffix(reference, "@"+digest) &&
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
	Success               bool             `json:"success"`
	ResultSummary         string           `json:"result_summary"`
	SafeErrorCode         string           `json:"safe_error_code,omitempty"`
	Usage                 TokenUsage       `json:"usage"`
	Artifacts             []RunnerArtifact `json:"artifacts,omitempty"`
}

func (request RunnerCompletionRequest) Validate() error {
	if !sha256Pattern.MatchString(request.RuntimeRevisionDigest) || len(request.ResultSummary) > 64<<10 ||
		len(request.SafeErrorCode) > 128 || len(request.Artifacts) > MaximumCompletionFiles ||
		(request.Success && strings.TrimSpace(request.ResultSummary) == "") || (!request.Success && request.SafeErrorCode == "") ||
		request.Usage.Validate() != nil {
		return errors.New("runner completion is invalid")
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

func sha256Sum(value []byte) string {
	digest := sha256.New()
	_, _ = digest.Write(value)
	return hex.EncodeToString(digest.Sum(nil))
}
