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
	"strings"
)

const (
	RunnerInputSchemaV3      = "mattercodex.agent-runner-input.v3"
	RunnerModeTurn           = "TURN"
	RunnerModeWarm           = "WARM"
	MaximumRunnerInputBytes  = 2 << 20
	MaximumCompletionBytes   = 16 << 20
	MaximumCompletionFiles   = 32
	MaximumProgressTextBytes = 2 << 10
)

var opaqueReferencePattern = regexp.MustCompile(`^[a-z][a-z0-9]{1,11}_[A-Za-z0-9_-]{8,84}$`)
var imageDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var systemRuntimeRevisionPattern = regexp.MustCompile(`^system-assistant-core-v[1-9][0-9]*$`)

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
	Ref             string `json:"ref"`
	Name            string `json:"name"`
	Purpose         string `json:"purpose"`
	RoleDescription string `json:"role_description"`
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

// RunnerInput — immutable contract одного turn либо always-hot system runtime.
// В нём нет actor/organization authority и secret values.
type RunnerInput struct {
	Schema                      string                   `json:"schema"`
	Mode                        string                   `json:"mode"`
	WorkloadInstance            string                   `json:"workload_instance"`
	RunRef                      string                   `json:"run_ref,omitempty"`
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
	Capabilities                []string                 `json:"capabilities,omitempty"`
	Provider                    string                   `json:"provider"`
	Model                       string                   `json:"model"`
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
	if input.Schema != RunnerInputSchemaV3 || (input.Mode != RunnerModeTurn && input.Mode != RunnerModeWarm) ||
		input.WorkloadInstance == "" || len(input.WorkloadInstance) > 128 ||
		!opaqueReferencePattern.MatchString(input.SessionRef) || !opaqueReferencePattern.MatchString(input.AgentRef) ||
		!(opaqueReferencePattern.MatchString(input.RuntimeRevisionRef) || systemRuntimeRevisionPattern.MatchString(input.RuntimeRevisionRef)) || input.RuntimeRevisionVersion < 1 ||
		!sha256Pattern.MatchString(input.RuntimeRevisionDigest) || !validPinnedImage(input.ImageReference, input.ImageManifestDigest) ||
		input.RoleRuntimeContractRevision == 0 || !sha256Pattern.MatchString(input.RoleRuntimeContractSHA256) ||
		strings.TrimSpace(input.Instructions) == "" || len(input.Instructions) > 1<<20 ||
		input.Provider == "" || len(input.Provider) > 64 || input.Model == "" || len(input.Model) > 128 ||
		(input.CodexSandbox != "read-only" && input.CodexSandbox != "workspace-write") ||
		(input.CodexApprovalPolicy != "untrusted" && input.CodexApprovalPolicy != "on-request" && input.CodexApprovalPolicy != "never") ||
		input.CallbackTLS.validate() != nil || !validCallbackURL(input.CallbackURL, input.CallbackTLS.ServerName) ||
		!validSecretFile(input.ExecutionTicketFile) || !validSecretFile(input.ProviderAuthFile) ||
		!validSecretFile(input.ProviderAuthSHA256File) || input.WorkspaceRoot != "/workspace" ||
		input.OutboxRoot != "/workspace/.matter-codex/outbox" || input.CodexHome != "/tmp/codex-home" ||
		len(input.SessionContext) > 128 || len(input.DelegationTargets) > 128 || len(input.IntegrationGrants) > 256 ||
		len(input.Capabilities) > 256 {
		return errors.New("runner input is invalid")
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
	return nil
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
	return filepath.IsAbs(path) && filepath.Clean(path) == path && strings.HasPrefix(path, "/var/run/secrets/")
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

type RunnerCompletionRequest struct {
	RuntimeRevisionDigest string           `json:"runtime_revision_digest"`
	Success               bool             `json:"success"`
	ResultSummary         string           `json:"result_summary"`
	SafeErrorCode         string           `json:"safe_error_code,omitempty"`
	Artifacts             []RunnerArtifact `json:"artifacts,omitempty"`
}

func (request RunnerCompletionRequest) Validate() error {
	if !sha256Pattern.MatchString(request.RuntimeRevisionDigest) || len(request.ResultSummary) > 64<<10 ||
		len(request.SafeErrorCode) > 128 || len(request.Artifacts) > MaximumCompletionFiles ||
		(request.Success && strings.TrimSpace(request.ResultSummary) == "") || (!request.Success && request.SafeErrorCode == "") {
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
