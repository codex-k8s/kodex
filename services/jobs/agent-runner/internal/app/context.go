package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const maxContextBytes = 64 * 1024

type AgentRunContext struct {
	AgentRunID           string               `json:"agent_run_id"`
	AgentSessionID       string               `json:"agent_session_id"`
	FlowVersionID        string               `json:"flow_version_id,omitempty"`
	StageID              string               `json:"stage_id,omitempty"`
	RoleProfileID        string               `json:"role_profile_id"`
	RoleProfileVersion   int64                `json:"role_profile_version"`
	RoleProfileDigest    string               `json:"role_profile_digest"`
	PromptVersionID      string               `json:"prompt_template_version_id"`
	PromptTemplateDigest string               `json:"prompt_template_digest"`
	WorkspaceFingerprint string               `json:"workspace_fingerprint"`
	RuntimeProfile       string               `json:"runtime_profile"`
	ProviderTarget       ProviderTargetRef    `json:"provider_target"`
	GuidancePackages     []GuidancePackageRef `json:"guidance_packages,omitempty"`
	SessionSnapshotRef   string               `json:"session_snapshot_ref,omitempty"`
	AllowedMCPTools      []string             `json:"allowed_mcp_tools,omitempty"`
}

type ProviderTargetRef struct {
	WorkItemRef     string `json:"work_item_ref,omitempty"`
	PullRequestRef  string `json:"pull_request_ref,omitempty"`
	CommentRef      string `json:"comment_ref,omitempty"`
	ReviewSignalRef string `json:"review_signal_ref,omitempty"`
}

type GuidancePackageRef struct {
	LocalPath              string `json:"local_path"`
	PackageInstallationRef string `json:"package_installation_ref"`
	PackageVersionRef      string `json:"package_version_ref"`
	PackageRef             string `json:"package_ref,omitempty"`
	PackageSlug            string `json:"package_slug,omitempty"`
	ManifestDigest         string `json:"manifest_digest"`
	CapabilityRef          string `json:"capability_ref,omitempty"`
}

func LoadContext(cfg Config) (AgentRunContext, Diagnostic) {
	info, err := os.Stat(cfg.ContextPath)
	if err != nil {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_unavailable", "agent_run context file is unavailable", ExitFailure)
	}
	if info.Size() > maxContextBytes {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_too_large", "agent_run context file is too large", ExitFailure)
	}
	raw, err := os.ReadFile(cfg.ContextPath)
	if err != nil {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_unavailable", "agent_run context file is unavailable", ExitFailure)
	}
	return DecodeContext(raw, cfg)
}

func DecodeContext(raw []byte, cfg Config) (AgentRunContext, Diagnostic) {
	if len(raw) > maxContextBytes {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_too_large", "agent_run context file is too large", ExitFailure)
	}
	if !digestMatches(raw, cfg.ContextDigest) {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_digest_mismatch", "agent_run context digest does not match expected digest", ExitFailure)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var context AgentRunContext
	if err := decoder.Decode(&context); err != nil {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_invalid", "agent_run context JSON is invalid", ExitFailure)
	}
	var extra json.RawMessage
	err := decoder.Decode(&extra)
	if err == nil {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_invalid", "agent_run context JSON contains multiple values", ExitFailure)
	}
	if !errors.Is(err, io.EOF) {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_invalid", "agent_run context JSON is invalid", ExitFailure)
	}
	return validateContext(context, cfg)
}

func validateContext(context AgentRunContext, cfg Config) (AgentRunContext, Diagnostic) {
	context.AgentRunID = strings.TrimSpace(context.AgentRunID)
	context.AgentSessionID = strings.TrimSpace(context.AgentSessionID)
	context.FlowVersionID = strings.TrimSpace(context.FlowVersionID)
	context.StageID = strings.TrimSpace(context.StageID)
	context.RoleProfileID = strings.TrimSpace(context.RoleProfileID)
	context.RoleProfileDigest = strings.TrimSpace(context.RoleProfileDigest)
	context.PromptVersionID = strings.TrimSpace(context.PromptVersionID)
	context.PromptTemplateDigest = strings.TrimSpace(context.PromptTemplateDigest)
	context.WorkspaceFingerprint = strings.TrimSpace(context.WorkspaceFingerprint)
	context.RuntimeProfile = strings.TrimSpace(context.RuntimeProfile)
	context.SessionSnapshotRef = strings.TrimSpace(context.SessionSnapshotRef)
	if context.AgentRunID != cfg.AgentRunID {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_mismatch", "agent_run context does not match runtime refs", ExitFailure)
	}
	if _, err := uuid.Parse(context.AgentSessionID); err != nil {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_invalid", "agent_run context session ref is invalid", ExitFailure)
	}
	for _, optionalID := range []string{context.FlowVersionID, context.StageID} {
		if optionalID != "" {
			if _, err := uuid.Parse(optionalID); err != nil {
				return AgentRunContext{}, NewDiagnostic("agent_run_context_invalid", "agent_run context optional refs are invalid", ExitFailure)
			}
		}
	}
	if _, err := uuid.Parse(context.RoleProfileID); err != nil || context.RoleProfileVersion < 1 {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_invalid", "agent_run context role refs are invalid", ExitFailure)
	}
	if _, err := uuid.Parse(context.PromptVersionID); err != nil {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_invalid", "agent_run context prompt refs are invalid", ExitFailure)
	}
	if context.WorkspaceFingerprint != cfg.ExpectedMaterializationFingerprint {
		return AgentRunContext{}, NewDiagnostic("agent_run_context_fingerprint_mismatch", "agent_run context fingerprint does not match runtime materialization", ExitFailure)
	}
	for _, value := range []string{
		context.RoleProfileDigest,
		context.PromptTemplateDigest,
		context.WorkspaceFingerprint,
		context.RuntimeProfile,
		context.SessionSnapshotRef,
		context.ProviderTarget.WorkItemRef,
		context.ProviderTarget.PullRequestRef,
		context.ProviderTarget.CommentRef,
		context.ProviderTarget.ReviewSignalRef,
	} {
		if !safeRef(value, false) {
			return AgentRunContext{}, NewDiagnostic("agent_run_context_unsafe", "agent_run context contains unsafe refs", ExitFailure)
		}
	}
	for _, tool := range context.AllowedMCPTools {
		if !safeRef(strings.TrimSpace(tool), true) {
			return AgentRunContext{}, NewDiagnostic("agent_run_context_unsafe", "agent_run context contains unsafe tool refs", ExitFailure)
		}
	}
	for _, guidance := range context.GuidancePackages {
		if diagnostic := validateGuidancePackage(guidance); !diagnostic.OK() {
			return AgentRunContext{}, diagnostic
		}
	}
	return context, OKDiagnostic()
}

func validateGuidancePackage(guidance GuidancePackageRef) Diagnostic {
	localPath := filepath.Clean(strings.TrimSpace(guidance.LocalPath))
	if !strings.HasPrefix(localPath, filepath.Join(".kodex", "guidance")+string(filepath.Separator)) {
		return NewDiagnostic("agent_run_context_unsafe", "agent_run context guidance path is unsafe", ExitFailure)
	}
	for _, value := range []string{
		guidance.PackageInstallationRef,
		guidance.PackageVersionRef,
		guidance.PackageRef,
		guidance.PackageSlug,
		guidance.ManifestDigest,
		guidance.CapabilityRef,
	} {
		if !safeRef(strings.TrimSpace(value), value == guidance.PackageInstallationRef || value == guidance.PackageVersionRef || value == guidance.ManifestDigest) {
			return NewDiagnostic("agent_run_context_unsafe", "agent_run context contains unsafe guidance refs", ExitFailure)
		}
	}
	return OKDiagnostic()
}

func digestMatches(raw []byte, expected string) bool {
	expectedHex, ok := parseSHA256Digest(expected)
	if !ok {
		return false
	}
	actual := sha256.Sum256(raw)
	return hex.EncodeToString(actual[:]) == expectedHex
}

func parseSHA256Digest(raw string) (string, bool) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if !strings.HasPrefix(value, "sha256:") {
		return "", false
	}
	hexValue := strings.TrimPrefix(value, "sha256:")
	if len(hexValue) != sha256.Size*2 {
		return "", false
	}
	decoded, err := hex.DecodeString(hexValue)
	return hexValue, err == nil && len(decoded) == sha256.Size
}

func SHA256Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
