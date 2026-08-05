// Package model содержит проверяемые immutable DTO agent-runner.
package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
)

const (
	InputSchemaV2            = "mattercodex.agent-runner-input.v2"
	ScheduledResultSchemaV1  = "mattercodex.scheduled-result.v1"
	ScheduledResultPathV1    = ".matter-codex/outbox/scheduled-result.v1.json"
	ScheduledResultFormatV1  = "application/json"
	ScheduledResultSHA256V1  = "13eadb9bc557312d0968b7507cb5cb33b30d6b12c8e4c2a5a504060c354da13a"
	ScheduledResultMaxBytes  = 8192
	MaximumInputBytes        = 2 << 20
	MaximumMaterializations  = 4096
	MaximumMaterializedBytes = int64(2 << 30)
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type TLSBinding struct {
	ServerName      string `json:"server_name"`
	CAFile          string `json:"ca_file"`
	CertificateFile string `json:"certificate_file"`
	PrivateKeyFile  string `json:"private_key_file"`
	BindingSHA256   string `json:"binding_sha256"`
}

type GRPCBinding struct {
	Target string     `json:"target"`
	TLS    TLSBinding `json:"tls"`
}

type HTTPBinding struct {
	URL string     `json:"url"`
	TLS TLSBinding `json:"tls"`
}

type CredentialFiles struct {
	ControlPlaneGrant    string `json:"control_plane_grant"`
	MCPToken             string `json:"mcp_token"`
	MaterializationToken string `json:"materialization_token"`
	CodexAuth            string `json:"codex_auth"`
	CodexAuthSHA256      string `json:"codex_auth_sha256"`
	HandoffPrivateKey    string `json:"handoff_private_key"`
	HandoffKeyID         string `json:"handoff_key_id"`
}

type Materialization struct {
	Kind            string `json:"kind"`
	ArtifactID      string `json:"artifact_id"`
	ArtifactVersion uint64 `json:"artifact_version"`
	SHA256          string `json:"sha256"`
	SizeBytes       int64  `json:"size_bytes"`
	RelativePath    string `json:"relative_path"`
	MediaType       string `json:"media_type"`
}

// ScheduledResultContract — immutable server-owned указатель на закрытый
// результат scheduled execution. Route/policy/tenant в него не входят.
type ScheduledResultContract struct {
	Schema       string `json:"schema"`
	Path         string `json:"path"`
	Format       string `json:"format"`
	SchemaSHA256 string `json:"schema_sha256"`
	MaximumBytes int    `json:"maximum_bytes"`
}

func (contract ScheduledResultContract) Validate() error {
	if contract.Schema != ScheduledResultSchemaV1 || contract.Path != ScheduledResultPathV1 ||
		contract.Format != ScheduledResultFormatV1 || contract.SchemaSHA256 != ScheduledResultSHA256V1 ||
		contract.MaximumBytes != ScheduledResultMaxBytes {
		return errors.New("scheduled result contract is invalid")
	}
	return nil
}

type Input struct {
	Schema                                 string                   `json:"schema"`
	ExecutionID                            string                   `json:"execution_id"`
	ExecutionVersion                       uint64                   `json:"execution_version"`
	Fence                                  uint64                   `json:"fence"`
	GrantGeneration                        uint64                   `json:"grant_generation"`
	RuntimeRevisionID                      string                   `json:"runtime_revision_id"`
	RuntimeRevisionVersion                 uint64                   `json:"runtime_revision_version"`
	RuntimeRevisionSHA256                  string                   `json:"runtime_revision_sha256"`
	EffectiveRuntimeSHA256                 string                   `json:"effective_runtime_sha256"`
	ImmutableInputSHA256                   string                   `json:"immutable_input_sha256"`
	SessionID                              string                   `json:"session_id"`
	TurnID                                 string                   `json:"turn_id"`
	ScheduleOccurrenceID                   string                   `json:"schedule_occurrence_id,omitempty"`
	ScheduledResultContract                *ScheduledResultContract `json:"scheduled_result_contract,omitempty"`
	Attempt                                uint32                   `json:"attempt"`
	SessionKey                             string                   `json:"session_key"`
	ProviderBindingID                      string                   `json:"provider_binding_id"`
	ProviderAccountName                    string                   `json:"provider_account_name"`
	MCPBindingVersion                      uint64                   `json:"mcp_binding_version"`
	ProviderBindingVersion                 uint64                   `json:"provider_binding_version"`
	ProviderBindingSHA256                  string                   `json:"provider_binding_sha256"`
	CredentialSnapshotSHA256               string                   `json:"credential_snapshot_sha256"`
	WorkloadTicketSHA256                   string                   `json:"workload_ticket_sha256"`
	AgentProfile                           string                   `json:"agent_profile"`
	CodexModel                             string                   `json:"codex_model"`
	CodexSandbox                           string                   `json:"codex_sandbox"`
	CodexApprovalPolicy                    string                   `json:"codex_approval_policy"`
	CodexSessionID                         string                   `json:"codex_session_id"`
	CodexArchiveRelativePath               string                   `json:"codex_archive_relative_path"`
	CodexArchiveSHA256                     string                   `json:"codex_archive_sha256"`
	CodexArchiveProvenance                 string                   `json:"codex_archive_provenance"`
	CodexDeliveryRecoverySourceExecutionID string                   `json:"codex_delivery_recovery_source_execution_id"`
	ControlPlane                           GRPCBinding              `json:"control_plane"`
	MCP                                    HTTPBinding              `json:"mcp"`
	InteractionGateway                     HTTPBinding              `json:"interaction_gateway"`
	CredentialFiles                        CredentialFiles          `json:"credential_files"`
	Materializations                       []Materialization        `json:"materializations"`
	PromptPath                             string                   `json:"prompt_path"`
	InstructionsPath                       string                   `json:"instructions_path"`
	WorkspaceRoot                          string                   `json:"workspace_root"`
	OutboxRoot                             string                   `json:"outbox_root"`
	CodexHome                              string                   `json:"codex_home"`
	MattermostPostMaximumRunes             int                      `json:"mattermost_post_max_runes"`
	HandoffConfigMap                       string                   `json:"handoff_config_map"`
	PodNamespace                           string                   `json:"pod_namespace"`
}

func DecodeInput(path string) (Input, error) {
	if !filepath.IsAbs(path) {
		return Input{}, errors.New("runtime input path is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return Input{}, errors.New("open runtime input")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, MaximumInputBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > MaximumInputBytes {
		return Input{}, errors.New("read runtime input")
	}
	var input Input
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || input.Validate() != nil {
		return Input{}, errors.New("runtime input is invalid")
	}
	input.Materializations = slices.Clone(input.Materializations)
	return input, nil
}

func (input Input) Validate() error {
	for _, identifier := range []string{input.ExecutionID, input.RuntimeRevisionID, input.SessionID,
		input.TurnID, input.ProviderBindingID} {
		if uuid.Validate(identifier) != nil {
			return errors.New("runtime identity is invalid")
		}
	}
	if input.ScheduleOccurrenceID != "" && uuid.Validate(input.ScheduleOccurrenceID) != nil {
		return errors.New("runtime schedule occurrence identity is invalid")
	}
	if (input.ScheduleOccurrenceID == "") != (input.ScheduledResultContract == nil) ||
		input.ScheduledResultContract != nil && input.ScheduledResultContract.Validate() != nil {
		return errors.New("runtime scheduled result binding is invalid")
	}
	for _, digest := range []string{input.RuntimeRevisionSHA256, input.EffectiveRuntimeSHA256,
		input.ImmutableInputSHA256, input.ProviderBindingSHA256,
		input.CredentialSnapshotSHA256, input.WorkloadTicketSHA256} {
		if !sha256Pattern.MatchString(digest) {
			return errors.New("runtime digest is invalid")
		}
	}
	if input.Schema != InputSchemaV2 || input.ExecutionVersion == 0 || input.Fence == 0 ||
		input.GrantGeneration == 0 || input.RuntimeRevisionVersion == 0 || input.Attempt == 0 ||
		input.ProviderBindingVersion == 0 || input.MCPBindingVersion == 0 || input.SessionKey == "" || len(input.SessionKey) > 256 ||
		!regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,47}[a-z0-9])?$`).MatchString(input.ProviderAccountName) ||
		!regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`).MatchString(input.AgentProfile) ||
		!regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`).MatchString(input.CodexModel) ||
		(input.CodexSandbox != "read-only" && input.CodexSandbox != "workspace-write") ||
		(input.CodexApprovalPolicy != "untrusted" && input.CodexApprovalPolicy != "on-request" &&
			input.CodexApprovalPolicy != "never") ||
		(input.CodexSessionID != "" && uuid.Validate(input.CodexSessionID) != nil) ||
		input.PromptPath != ".matter-codex/inbox/prompt.md" || input.InstructionsPath != "AGENTS.md" ||
		input.WorkspaceRoot != "/workspace" || input.OutboxRoot != "/workspace/.matter-codex/outbox" ||
		input.CodexHome != "/workspace/.matter-codex/state/codex-home" ||
		input.MattermostPostMaximumRunes != 16383 || input.ControlPlane.validate() != nil ||
		!regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`).MatchString(input.HandoffConfigMap) ||
		!regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`).MatchString(input.PodNamespace) ||
		input.MCP.validate() != nil || input.InteractionGateway.validate() != nil || input.CredentialFiles.validate() != nil {
		return errors.New("runtime binding is invalid")
	}
	archiveEmpty := input.CodexArchiveRelativePath == "" && input.CodexArchiveSHA256 == "" && input.CodexArchiveProvenance == ""
	archivePresent := regexp.MustCompile(`^\.matter-codex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[A-Za-z0-9._-]+\.jsonl$`).MatchString(input.CodexArchiveRelativePath) &&
		!strings.Contains(input.CodexArchiveRelativePath, "..") && sha256Pattern.MatchString(input.CodexArchiveSHA256) &&
		validCodexArchiveProvenance(input.CodexArchiveProvenance, input.CodexArchiveRelativePath, input.CodexArchiveSHA256)
	if (!archiveEmpty && !archivePresent) || (archivePresent && input.CodexSessionID == "") {
		return errors.New("runtime Codex archive binding is invalid")
	}
	if input.CodexDeliveryRecoverySourceExecutionID != "" &&
		(uuid.Validate(input.CodexDeliveryRecoverySourceExecutionID) != nil || input.Attempt < 2 || !archivePresent) {
		return errors.New("runtime Codex delivery recovery binding is invalid")
	}
	if len(input.Materializations) < 2 || len(input.Materializations) > MaximumMaterializations {
		return errors.New("runtime materialization set is invalid")
	}
	seen := make(map[string]struct{}, len(input.Materializations))
	var total int64
	prompt, instructions := 0, 0
	for _, item := range input.Materializations {
		if item.validate() != nil {
			return errors.New("runtime materialization is invalid")
		}
		if _, duplicate := seen[item.RelativePath]; duplicate {
			return errors.New("runtime materialization path is duplicated")
		}
		seen[item.RelativePath] = struct{}{}
		total += item.SizeBytes
		if item.Kind == "PROMPT" && item.RelativePath == input.PromptPath {
			prompt++
		}
		if item.Kind == "INSTRUCTION" && item.RelativePath == input.InstructionsPath {
			instructions++
		}
	}
	if total > MaximumMaterializedBytes || prompt != 1 || instructions != 1 {
		return errors.New("runtime materialization set is incomplete")
	}
	return nil
}

func validCodexArchiveProvenance(value, path, digest string) bool {
	const prefix = "codex-app-server-rollout-v1:"
	suffix := ":" + path + ":" + digest
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return false
	}
	sourceExecutionID := strings.TrimSuffix(strings.TrimPrefix(value, prefix), suffix)
	return uuid.Validate(sourceExecutionID) == nil
}

func (binding GRPCBinding) validate() error {
	host, port, err := net.SplitHostPort(binding.Target)
	if err != nil || host == "" || port == "" || !strings.HasSuffix(host, ".svc") {
		return errors.New("gRPC binding is invalid")
	}
	return binding.TLS.validate()
}

func (binding HTTPBinding) validate() error {
	endpoint, err := url.Parse(binding.URL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		!strings.HasSuffix(endpoint.Hostname(), ".svc.cluster.local") || endpoint.Hostname() != binding.TLS.ServerName {
		return errors.New("HTTP binding is invalid")
	}
	return binding.TLS.validate()
}

func (binding TLSBinding) validate() error {
	if binding.ServerName == "" || net.ParseIP(binding.ServerName) != nil ||
		!strings.HasSuffix(binding.ServerName, ".svc.cluster.local") || !sha256Pattern.MatchString(binding.BindingSHA256) {
		return errors.New("TLS binding is invalid")
	}
	for _, path := range []string{binding.CAFile, binding.CertificateFile, binding.PrivateKeyFile} {
		if !validSecretPath(path) {
			return errors.New("TLS credential path is invalid")
		}
	}
	return nil
}

func (credentials CredentialFiles) validate() error {
	for _, path := range []string{credentials.ControlPlaneGrant, credentials.MCPToken,
		credentials.MaterializationToken, credentials.CodexAuth, credentials.HandoffPrivateKey} {
		if !validSecretPath(path) {
			return errors.New("runtime credential path is invalid")
		}
	}
	if credentials.HandoffKeyID == "" || len(credentials.HandoffKeyID) > 128 ||
		strings.ContainsAny(credentials.HandoffKeyID, "\x00\r\n") || !sha256Pattern.MatchString(credentials.CodexAuthSHA256) {
		return errors.New("handoff key identity is invalid")
	}
	return nil
}

func (item Materialization) validate() error {
	if item.Kind != "PROMPT" && item.Kind != "INSTRUCTION" && item.Kind != "REPOSITORY" &&
		item.Kind != "WORKSPACE" && item.Kind != "ATTACHMENT" && item.Kind != "SESSION_ARCHIVE" {
		return errors.New("materialization kind is invalid")
	}
	clean := filepath.Clean(item.RelativePath)
	if clean != item.RelativePath || filepath.IsAbs(clean) || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || strings.ContainsRune(clean, '\x00') ||
		uuid.Validate(item.ArtifactID) != nil || item.ArtifactVersion == 0 ||
		!sha256Pattern.MatchString(item.SHA256) || item.SizeBytes < 1 || item.SizeBytes > 64<<20 ||
		item.MediaType == "" || len(item.MediaType) > 255 {
		return errors.New("materialization entry is invalid")
	}
	return nil
}

func validSecretPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && strings.HasPrefix(path, "/var/run/secrets/")
}
