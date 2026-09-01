// Package codex управляет одним non-interactive Codex turn.
package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

type runtimeConfig struct {
	Model                  string                       `toml:"model"`
	ModelReasoningEffort   string                       `toml:"model_reasoning_effort,omitempty"`
	Personality            string                       `toml:"personality,omitempty"`
	AllowLoginShell        *bool                        `toml:"allow_login_shell,omitempty"`
	ApprovalPolicy         string                       `toml:"approval_policy"`
	DefaultPermissions     string                       `toml:"default_permissions"`
	CLIAuthCredentialStore string                       `toml:"cli_auth_credentials_store"`
	History                historyConfig                `toml:"history"`
	ShellEnvironmentPolicy shellEnvironmentPolicy       `toml:"shell_environment_policy"`
	Features               runtimeFeatures              `toml:"features"`
	MCPServers             map[string]mcpServerConfig   `toml:"mcp_servers"`
	Permissions            map[string]permissionProfile `toml:"permissions"`
}

type runtimeFeatures struct {
	CodeMode runtimeCodeModeConfig `toml:"code_mode"`
}

type runtimeCodeModeConfig struct {
	DirectOnlyToolNamespaces []string `toml:"direct_only_tool_namespaces"`
}

type permissionProfile struct {
	Extends    string            `toml:"extends"`
	Filesystem map[string]string `toml:"filesystem"`
}

type historyConfig struct {
	Persistence string `toml:"persistence"`
}

type shellEnvironmentPolicy struct {
	Inherit               string            `toml:"inherit"`
	IgnoreDefaultExcludes bool              `toml:"ignore_default_excludes"`
	IncludeOnly           []string          `toml:"include_only"`
	Set                   map[string]string `toml:"set"`
}

type mcpServerConfig struct {
	URL                   string `toml:"url"`
	Required              bool   `toml:"required"`
	BearerTokenEnvVar     string `toml:"bearer_token_env_var"`
	StartupTimeoutSeconds int    `toml:"startup_timeout_sec"`
	ToolTimeoutSeconds    int    `toml:"tool_timeout_sec"`
}

func PrepareHomeWithAuth(input model.Input, mcpURL string, auth []byte) error {
	if filepath.Clean(input.CodexHome) != input.CodexHome ||
		!strings.HasPrefix(input.CodexHome, input.WorkspaceRoot+string(os.PathSeparator)) {
		return errors.New("CODEX_HOME path is invalid")
	}
	if err := secureDirectory(input.CodexHome); err != nil {
		return err
	}
	if len(auth) == 0 || len(auth) > 1<<20 || !bytes.HasPrefix(bytes.TrimSpace(auth), []byte("{")) {
		return errors.New("Codex authentication snapshot is invalid; use codex login --device-auth outside the runtime")
	}
	authDigest := sha256.Sum256(auth)
	expectedDigest, err := pinnedProviderDigest(input)
	if err != nil || hex.EncodeToString(authDigest[:]) != expectedDigest {
		return errors.New("Codex authentication snapshot does not match the pinned provider account")
	}
	if err := replacePrivateFile(filepath.Join(input.CodexHome, "auth.json"), auth); err != nil {
		return err
	}
	const permissionProfileName = "kodex-runtime"
	permissionBase, err := codexPermissionBase(input.CodexSandbox)
	if err != nil {
		return err
	}
	overlay, err := runtimecontract.ParseConfigOverlay(input.ConfigOverlay)
	if err != nil {
		return errors.New("validate published Codex configuration overlay")
	}
	allowLoginShell := false
	if overlay.AllowLoginShell != nil {
		allowLoginShell = *overlay.AllowLoginShell
	}
	historyPersistence := overlay.History.Persistence
	if historyPersistence == "" {
		historyPersistence = "save-all"
	}
	environmentSet := map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": "/tmp"}
	environmentNames := map[string]struct{}{"PATH": {}, "HOME": {}}
	for _, item := range input.EnvironmentValues {
		environmentSet[item.Name] = item.Value
		environmentNames[item.Name] = struct{}{}
	}
	for _, item := range input.SecretProjections {
		environmentNames[item.Name] = struct{}{}
	}
	includeOnly := make([]string, 0, len(environmentNames))
	for name := range environmentNames {
		includeOnly = append(includeOnly, name)
	}
	slices.Sort(includeOnly)
	config := runtimeConfig{Model: input.Model, ModelReasoningEffort: overlay.ModelReasoningEffort,
		Personality: overlay.Personality, AllowLoginShell: &allowLoginShell, ApprovalPolicy: input.CodexApprovalPolicy,
		DefaultPermissions: permissionProfileName, CLIAuthCredentialStore: "file",
		History: historyConfig{Persistence: historyPersistence},
		Features: runtimeFeatures{CodeMode: runtimeCodeModeConfig{
			DirectOnlyToolNamespaces: []string{"mcp__kodex"},
		}},
		Permissions: map[string]permissionProfile{permissionProfileName: {Extends: permissionBase,
			Filesystem: map[string]string{
				filepath.Join(input.CodexHome, "auth.json"): "deny",
				"/run/secrets": "deny",
				"/proc":        "deny",
			}}},
		ShellEnvironmentPolicy: shellEnvironmentPolicy{Inherit: "all", IgnoreDefaultExcludes: true,
			IncludeOnly: includeOnly, Set: environmentSet},
		MCPServers: map[string]mcpServerConfig{"kodex": {URL: mcpURL,
			BearerTokenEnvVar: "KODEX_MCP_PROXY_TOKEN",
			Required:          true, StartupTimeoutSeconds: 15, ToolTimeoutSeconds: 60}}}
	var raw bytes.Buffer
	if err := toml.NewEncoder(&raw).Encode(config); err != nil {
		return errors.New("encode Codex configuration")
	}
	var decoded runtimeConfig
	metadata, err := toml.Decode(raw.String(), &decoded)
	if err != nil || len(metadata.Undecoded()) != 0 || decoded.Model != input.Model ||
		!decoded.MCPServers["kodex"].Required ||
		decoded.MCPServers["kodex"].BearerTokenEnvVar != "KODEX_MCP_PROXY_TOKEN" ||
		!slices.Equal(decoded.Features.CodeMode.DirectOnlyToolNamespaces, []string{"mcp__kodex"}) ||
		decoded.DefaultPermissions != permissionProfileName || decoded.Permissions[permissionProfileName].Extends != permissionBase ||
		decoded.ShellEnvironmentPolicy.Inherit != "all" || !slices.Equal(decoded.ShellEnvironmentPolicy.IncludeOnly, includeOnly) ||
		decoded.Permissions[permissionProfileName].Filesystem[filepath.Join(input.CodexHome, "auth.json")] != "deny" {
		return errors.New("validate Codex configuration")
	}
	return replacePrivateFile(filepath.Join(input.CodexHome, "config.toml"), raw.Bytes())
}

func readProviderDigest(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) < 64 || len(raw) > 66 {
		return "", errors.New("read provider authentication digest")
	}
	value := strings.TrimSpace(string(raw))
	if len(value) != 64 || strings.Trim(value, "0123456789abcdef") != "" {
		return "", errors.New("provider authentication digest is invalid")
	}
	return value, nil
}

func pinnedProviderDigest(input model.Input) (string, error) {
	value, err := readProviderDigest(input.ProviderAuthSHA256File)
	if err != nil || value != input.ProviderCredentialSHA256 {
		return "", errors.New("provider authentication revision does not match RuntimeRevision")
	}
	return value, nil
}

func codexPermissionBase(sandbox string) (string, error) {
	switch sandbox {
	case "read-only":
		return ":read-only", nil
	case "workspace-write":
		return ":workspace", nil
	default:
		return "", errors.New("Codex sandbox policy is invalid")
	}
}

func secureDirectory(path string) error {
	if err := os.MkdirAll(path, 0o770); err != nil {
		return errors.New("create Codex state directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Codex state directory is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("Codex state directory metadata is unavailable")
	}
	if os.Geteuid() == 10002 {
		if stat.Uid != 10001 || stat.Gid != 29000 || info.Mode().Perm() != 0o770 || info.Mode()&os.ModeSetgid == 0 {
			return errors.New("Codex state directory is outside the trusted shared boundary")
		}
		return nil
	}
	if stat.Uid != uint32(os.Geteuid()) || os.Chmod(path, 0o2770) != nil {
		return errors.New("protect Codex state directory")
	}
	return nil
}

func replacePrivateFile(path string, payload []byte) error {
	temporary := path + ".next"
	if err := os.Remove(temporary); err != nil && !os.IsNotExist(err) {
		return errors.New("remove stale Codex snapshot")
	}
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("create Codex snapshot")
	}
	if _, err := file.Write(payload); err != nil {
		file.Close()
		return errors.New("write Codex snapshot")
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return errors.New("sync Codex snapshot")
	}
	if err := file.Close(); err != nil {
		return errors.New("close Codex snapshot")
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("commit Codex snapshot: %w", err)
	}
	return nil
}
