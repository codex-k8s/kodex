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
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
)

type runtimeConfig struct {
	Model                  string                     `toml:"model"`
	ApprovalPolicy         string                     `toml:"approval_policy"`
	SandboxMode            string                     `toml:"sandbox_mode"`
	CLIAuthCredentialStore string                     `toml:"cli_auth_credentials_store"`
	History                historyConfig              `toml:"history"`
	ShellEnvironmentPolicy shellEnvironmentPolicy     `toml:"shell_environment_policy"`
	MCPServers             map[string]mcpServerConfig `toml:"mcp_servers"`
}

type historyConfig struct {
	Persistence string `toml:"persistence"`
}

type shellEnvironmentPolicy struct {
	Inherit string            `toml:"inherit"`
	Set     map[string]string `toml:"set"`
}

type mcpServerConfig struct {
	URL                   string `toml:"url"`
	Required              bool   `toml:"required"`
	StartupTimeoutSeconds int    `toml:"startup_timeout_sec"`
	ToolTimeoutSeconds    int    `toml:"tool_timeout_sec"`
}

func PrepareHome(input model.Input, mcpURL string) error {
	if filepath.Clean(input.CodexHome) != input.CodexHome ||
		!strings.HasPrefix(input.CodexHome, input.WorkspaceRoot+string(os.PathSeparator)) {
		return errors.New("CODEX_HOME path is invalid")
	}
	if err := secureDirectory(input.CodexHome); err != nil {
		return err
	}
	auth, err := os.ReadFile(input.CredentialFiles.CodexAuth)
	if err != nil || len(auth) == 0 || len(auth) > 1<<20 || !bytes.HasPrefix(bytes.TrimSpace(auth), []byte("{")) {
		return errors.New("Codex authentication snapshot is invalid; use codex login --device-auth outside the runtime")
	}
	authDigest := sha256.Sum256(auth)
	if hex.EncodeToString(authDigest[:]) != input.CredentialFiles.CodexAuthSHA256 {
		return errors.New("Codex authentication snapshot does not match the pinned provider account")
	}
	if err := replacePrivateFile(filepath.Join(input.CodexHome, "auth.json"), auth); err != nil {
		return err
	}
	config := runtimeConfig{Model: input.CodexModel, ApprovalPolicy: input.CodexApprovalPolicy,
		SandboxMode: input.CodexSandbox, CLIAuthCredentialStore: "file",
		History: historyConfig{Persistence: "save-all"},
		ShellEnvironmentPolicy: shellEnvironmentPolicy{Inherit: "none", Set: map[string]string{
			"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": input.CodexHome, "CODEX_HOME": input.CodexHome,
		}}, MCPServers: map[string]mcpServerConfig{"mattercodex": {URL: mcpURL,
			Required: true, StartupTimeoutSeconds: 15, ToolTimeoutSeconds: 60}}}
	var raw bytes.Buffer
	if err := toml.NewEncoder(&raw).Encode(config); err != nil {
		return errors.New("encode Codex configuration")
	}
	var decoded runtimeConfig
	metadata, err := toml.Decode(raw.String(), &decoded)
	if err != nil || len(metadata.Undecoded()) != 0 || decoded.Model != input.CodexModel ||
		!decoded.MCPServers["mattercodex"].Required {
		return errors.New("validate Codex configuration")
	}
	return replacePrivateFile(filepath.Join(input.CodexHome, "config.toml"), raw.Bytes())
}

func secureDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return errors.New("create Codex state directory")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return errors.New("protect Codex state directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return errors.New("Codex state directory is unsafe")
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
