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
	// RuntimeWorkspaceRoot is the fixed root of the agent-visible workspace.
	// RuntimeWorkspaceRoot — неизменный корень рабочего пространства, видимого агенту.
	RuntimeWorkspaceRoot = "/workspace"
	// RuntimeWorkspaceWorkingRoot is the conventional writable work directory.
	// RuntimeWorkspaceWorkingRoot — стандартная доступная для записи рабочая папка.
	RuntimeWorkspaceWorkingRoot = RuntimeWorkspaceRoot + "/work"
	// RuntimeWorkspaceWritableBytes caps owner-configurable writable storage.
	// RuntimeWorkspaceWritableBytes ограничивает настраиваемый владельцем объём записи.
	RuntimeWorkspaceWritableBytes int64 = 1 << 30
	// RuntimeWorkspaceMaximumFiles caps the number of writable workspace files.
	// RuntimeWorkspaceMaximumFiles ограничивает количество записываемых файлов.
	RuntimeWorkspaceMaximumFiles int64 = 10_000

	// RuntimeWorkspaceReadOnly denotes a path protected from agent writes.
	// RuntimeWorkspaceReadOnly обозначает путь, защищённый от записи агентом.
	RuntimeWorkspaceReadOnly = "READ_ONLY"
	// RuntimeWorkspaceWritable denotes a path permitted for agent writes.
	// RuntimeWorkspaceWritable обозначает путь, доступный агенту для записи.
	RuntimeWorkspaceWritable = "WRITABLE"
	// RuntimeWorkspaceQuotaExceeded identifies a rejected resource-limit operation.
	// RuntimeWorkspaceQuotaExceeded обозначает отказ при превышении ресурсной квоты.
	RuntimeWorkspaceQuotaExceeded = "QUOTA_EXCEEDED"
	// RuntimeWorkspacePathOutsideWorkspace identifies a rejected path escape.
	// RuntimeWorkspacePathOutsideWorkspace обозначает отказ при выходе пути за границу рабочего пространства.
	RuntimeWorkspacePathOutsideWorkspace = "PATH_OUTSIDE_WORKSPACE"
	// RuntimeWorkspaceIOError identifies a runtime filesystem failure.
	// RuntimeWorkspaceIOError обозначает сбой файловой системы runtime.
	RuntimeWorkspaceIOError = "RUNTIME_IO_ERROR"
)

// runtimeWorkspaceDenialReasons fixes the ordered set exposed by the workspace contract.
// runtimeWorkspaceDenialReasons закрепляет упорядоченный набор причин отказа в контракте рабочего пространства.
var runtimeWorkspaceDenialReasons = [...]string{
	RuntimeWorkspaceReadOnly,
	RuntimeWorkspaceQuotaExceeded,
	RuntimeWorkspacePathOutsideWorkspace,
	RuntimeWorkspaceIOError,
}

// RuntimeWorkspacePathRule assigns access to one canonical path prefix.
// RuntimeWorkspacePathRule назначает режим доступа одному каноническому префиксу пути.
type RuntimeWorkspacePathRule struct {
	Path   string `json:"path"`
	Access string `json:"access"`
}

// RuntimeWorkspaceLimits can only narrow resource quotas in the platform policy;
// owners cannot change the root, protected paths, or volume sources.
// RuntimeWorkspaceLimits может лишь сузить ресурсные квоты платформенной политики;
// владелец не меняет корень, защищённые пути и источники томов.
type RuntimeWorkspaceLimits struct {
	MaxBytes int64 `json:"max_bytes"`
	MaxFiles int64 `json:"max_files"`
}

// Validate requires positive quota limits no larger than the platform ceilings.
// Validate требует положительные квоты не выше платформенных пределов.
func (limits RuntimeWorkspaceLimits) Validate() error {
	if limits.MaxBytes < 1 || limits.MaxBytes > RuntimeWorkspaceWritableBytes ||
		limits.MaxFiles < 1 || limits.MaxFiles > RuntimeWorkspaceMaximumFiles {
		return errors.New("runtime workspace limits are invalid")
	}
	return nil
}

// RuntimeWorkspacePolicyWithLimits derives a digest-pinned policy with optional narrower quotas.
// RuntimeWorkspacePolicyWithLimits создаёт закреплённую дайджестом политику с необязательными меньшими квотами.
func RuntimeWorkspacePolicyWithLimits(limits *RuntimeWorkspaceLimits) (RuntimeWorkspacePolicy, error) {
	policy := RuntimeWorkspacePolicyV1()
	if limits == nil {
		return policy, nil
	}
	if err := limits.Validate(); err != nil {
		return RuntimeWorkspacePolicy{}, err
	}
	policy.MaximumWritableBytes, policy.MaximumFileCount = limits.MaxBytes, limits.MaxFiles
	policy.Digest = ""
	raw, _ := json.Marshal(policy)
	digest := sha256.Sum256(raw)
	policy.Digest = hex.EncodeToString(digest[:])
	return policy, nil
}

// RuntimeWorkspacePolicy records fixed path rules, bounded quotas, and a canonical digest.
// RuntimeWorkspacePolicy хранит неизменные правила путей, ограниченные квоты и канонический дайджест.
type RuntimeWorkspacePolicy struct {
	Revision             int64                      `json:"revision"`
	Root                 string                     `json:"root"`
	Rules                []RuntimeWorkspacePathRule `json:"rules"`
	MaximumWritableBytes int64                      `json:"maximum_writable_bytes"`
	MaximumFileCount     int64                      `json:"maximum_file_count"`
	Digest               string                     `json:"digest"`
	DenialReasons        []string                   `json:"denial_reasons"`
}

// RuntimeWorkspacePolicyV1 constructs the platform-owned default path and quota rules.
// RuntimeWorkspacePolicyV1 создаёт платформенные правила путей и квот по умолчанию.
func RuntimeWorkspacePolicyV1() RuntimeWorkspacePolicy {
	policy := RuntimeWorkspacePolicy{Revision: 1, Root: RuntimeWorkspaceRoot,
		Rules: []RuntimeWorkspacePathRule{
			{Path: RuntimeWorkspaceRoot + "/input", Access: RuntimeWorkspaceReadOnly},
			{Path: RuntimeWorkspaceRoot + "/knowledge", Access: RuntimeWorkspaceReadOnly},
			{Path: RuntimeContextRoot, Access: RuntimeWorkspaceReadOnly},
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

// Validate rejects changed path authority, quota ceilings, denial reasons, or policy digest.
// Validate отклоняет изменённые полномочия путей, пределы квот, причины отказа или дайджест политики.
func (policy RuntimeWorkspacePolicy) Validate() error {
	if policy.Revision != 1 || policy.Root != RuntimeWorkspaceRoot ||
		(RuntimeWorkspaceLimits{MaxBytes: policy.MaximumWritableBytes, MaxFiles: policy.MaximumFileCount}).Validate() != nil || len(policy.Rules) != 5 {
		return errors.New("runtime workspace policy is invalid")
	}
	expectedRules := map[string]string{
		RuntimeWorkspaceRoot + "/input":                             RuntimeWorkspaceReadOnly,
		RuntimeWorkspaceRoot + "/knowledge":                         RuntimeWorkspaceReadOnly,
		RuntimeContextRoot:                                          RuntimeWorkspaceReadOnly,
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

// NormalizePath accepts an absolute or workspace-relative path but rejects traversal
// even when cleaning would place the resulting path back inside the workspace.
// NormalizePath принимает абсолютный путь или путь относительно рабочего пространства, но
// отклоняет traversal, даже если очистка вернула бы результат внутрь рабочего пространства.
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

// AccessForPath normalizes the candidate and applies the longest matching path rule.
// AccessForPath нормализует путь и применяет наиболее длинное подходящее правило.
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
