package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"strings"
)

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

func (policy RuntimeWorkspacePolicy) Validate() error {
	if policy.Revision < 1 || policy.Root != "/workspace" || policy.MaximumWritableBytes <= 0 || policy.MaximumFileCount <= 0 || len(policy.Rules) == 0 {
		return errors.New("runtime workspace policy is invalid")
	}
	seen := make(map[string]struct{}, len(policy.Rules))
	for _, rule := range policy.Rules {
		if rule.Path == "" || !path.IsAbs(rule.Path) || path.Clean(rule.Path) != rule.Path || !strings.HasPrefix(rule.Path, policy.Root+"/") && rule.Path != policy.Root || (rule.Access != "READ_ONLY" && rule.Access != "WRITABLE") {
			return errors.New("runtime workspace path rule is invalid")
		}
		if _, ok := seen[rule.Path]; ok {
			return errors.New("runtime workspace path rule is duplicated")
		}
		seen[rule.Path] = struct{}{}
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
