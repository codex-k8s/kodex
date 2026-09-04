package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"strings"
)

const (
	RuntimeWorkspaceRoot                = "/workspace"
	RuntimeWorkspaceWritableBytes int64 = 1 << 30
	RuntimeWorkspaceMaximumFiles  int64 = 10_000

	RuntimeWorkspaceReadOnly             = "READ_ONLY"
	RuntimeWorkspaceWritable             = "WRITABLE"
	RuntimeWorkspaceQuotaExceeded        = "QUOTA_EXCEEDED"
	RuntimeWorkspacePathOutsideWorkspace = "PATH_OUTSIDE_WORKSPACE"
	RuntimeWorkspaceIOError              = "RUNTIME_IO_ERROR"
)

var runtimeWorkspaceDenialReasons = [...]string{
	RuntimeWorkspaceReadOnly,
	RuntimeWorkspaceQuotaExceeded,
	RuntimeWorkspacePathOutsideWorkspace,
	RuntimeWorkspaceIOError,
}

type RuntimeWorkspacePathRule struct {
	Path   string `json:"path"`
	Access string `json:"access"`
}

type RuntimeWorkspacePolicy struct {
	Revision             int64                      `json:"revision"`
	Root                 string                     `json:"root"`
	Rules                []RuntimeWorkspacePathRule `json:"rules"`
	MaximumWritableBytes int64                      `json:"maximum_writable_bytes"`
	MaximumFileCount     int64                      `json:"maximum_file_count"`
	Digest               string                     `json:"digest"`
	DenialReasons        []string                   `json:"denial_reasons"`
}

func RuntimeWorkspacePolicyV1() RuntimeWorkspacePolicy {
	policy := RuntimeWorkspacePolicy{Revision: 1, Root: RuntimeWorkspaceRoot,
		Rules: []RuntimeWorkspacePathRule{
			{Path: RuntimeWorkspaceRoot + "/input", Access: RuntimeWorkspaceReadOnly},
			{Path: RuntimeWorkspaceRoot + "/knowledge", Access: RuntimeWorkspaceReadOnly},
			{Path: RuntimeWorkspaceRoot + "/.kodex/state/codex-home/auth.json", Access: RuntimeWorkspaceReadOnly},
			{Path: RuntimeWorkspaceRoot, Access: RuntimeWorkspaceWritable},
		},
		MaximumWritableBytes: RuntimeWorkspaceWritableBytes, MaximumFileCount: RuntimeWorkspaceMaximumFiles,
		DenialReasons: append([]string(nil), runtimeWorkspaceDenialReasons[:]...)}
	raw, _ := json.Marshal(policy)
	digest := sha256.Sum256(raw)
	policy.Digest = hex.EncodeToString(digest[:])
	return policy
}

func (policy RuntimeWorkspacePolicy) Validate() error {
	if policy.Revision != 1 || policy.Root != RuntimeWorkspaceRoot ||
		policy.MaximumWritableBytes != RuntimeWorkspaceWritableBytes ||
		policy.MaximumFileCount != RuntimeWorkspaceMaximumFiles || len(policy.Rules) != 4 {
		return errors.New("runtime workspace policy is invalid")
	}
	expectedRules := map[string]string{
		RuntimeWorkspaceRoot + "/input":                             RuntimeWorkspaceReadOnly,
		RuntimeWorkspaceRoot + "/knowledge":                         RuntimeWorkspaceReadOnly,
		RuntimeWorkspaceRoot + "/.kodex/state/codex-home/auth.json": RuntimeWorkspaceReadOnly,
		RuntimeWorkspaceRoot:                                        RuntimeWorkspaceWritable,
	}
	seen := make(map[string]struct{}, len(policy.Rules))
	for _, rule := range policy.Rules {
		if rule.Path == "" || !path.IsAbs(rule.Path) || path.Clean(rule.Path) != rule.Path || !strings.HasPrefix(rule.Path, policy.Root+"/") && rule.Path != policy.Root || (rule.Access != "READ_ONLY" && rule.Access != "WRITABLE") {
			return errors.New("runtime workspace path rule is invalid")
		}
		if expectedRules[rule.Path] != rule.Access {
			return errors.New("runtime workspace path rule is unsupported")
		}
		if _, ok := seen[rule.Path]; ok {
			return errors.New("runtime workspace path rule is duplicated")
		}
		seen[rule.Path] = struct{}{}
	}
	if len(policy.DenialReasons) != len(runtimeWorkspaceDenialReasons) {
		return errors.New("runtime workspace denial reasons are invalid")
	}
	for index, reason := range runtimeWorkspaceDenialReasons {
		if policy.DenialReasons[index] != reason {
			return errors.New("runtime workspace denial reasons are invalid")
		}
	}
	if policy.Digest == "" {
		return errors.New("runtime workspace policy digest is missing")
	}
	canonical := policy
	canonical.Digest = ""
	raw, err := json.Marshal(canonical)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	if policy.Digest != hex.EncodeToString(digest[:]) {
		return errors.New("runtime workspace policy digest mismatch")
	}
	return nil
}

// NormalizePath принимает абсолютный либо workspace-relative path, но не
// разрешает traversal даже тогда, когда filepath.Clean вернул бы путь обратно
// внутрь workspace.
func (policy RuntimeWorkspacePolicy) NormalizePath(candidate string) (string, string) {
	if candidate == "" || strings.ContainsRune(candidate, '\x00') || strings.Contains(candidate, "\\") {
		return "", RuntimeWorkspacePathOutsideWorkspace
	}
	parts := strings.Split(candidate, "/")
	for _, part := range parts {
		if part == ".." {
			return "", RuntimeWorkspacePathOutsideWorkspace
		}
	}
	resolved := candidate
	if !path.IsAbs(resolved) {
		resolved = path.Join(policy.Root, resolved)
	}
	resolved = path.Clean(resolved)
	if resolved != policy.Root && !strings.HasPrefix(resolved, policy.Root+"/") {
		return "", RuntimeWorkspacePathOutsideWorkspace
	}
	return resolved, ""
}

// AccessForPath применяет longest-prefix rule к уже нормализованному path.
func (policy RuntimeWorkspacePolicy) AccessForPath(candidate string) (string, string) {
	resolved, denial := policy.NormalizePath(candidate)
	if denial != "" {
		return "", denial
	}
	matchedPath, access := "", ""
	for _, rule := range policy.Rules {
		if (resolved == rule.Path || strings.HasPrefix(resolved, rule.Path+"/")) && len(rule.Path) > len(matchedPath) {
			matchedPath, access = rule.Path, rule.Access
		}
	}
	if access == "" {
		return "", RuntimeWorkspacePathOutsideWorkspace
	}
	return access, ""
}
