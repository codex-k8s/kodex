package opencode

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// managedToolPlugin is pinned application code, never a plugin from a repository or prompt.
// managedToolPlugin — закреплённый application code, не plugin из repository либо prompt.
//
//go:embed tool_identity.mjs
var managedToolPlugin []byte

// nativeCallPattern admits bounded upstream call identifiers without granting authority from them.
// nativeCallPattern принимает ограниченные upstream call IDs без выдачи ими полномочий.
var nativeCallPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)

// InstallManagedToolPlugin writes the exact embedded hook into an already private provider HOME.
// No dependency install, secret file or repository plugin is involved.
// InstallManagedToolPlugin записывает exact embedded hook в private provider HOME.
// Dependency install, secret file и repository plugin не используются.
func InstallManagedToolPlugin(home string) (string, error) {
	info, err := os.Lstat(home)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || filepath.Clean(home) != home || !filepath.IsAbs(home) {
		return "", ErrProtocol
	}
	path := filepath.Join(home, "managed-tool-identity.mjs")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", ErrProtocol
	}
	_, writeErr := file.Write(managedToolPlugin)
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return "", ErrProtocol
	}
	// Upstream skips its automatic npm bootstrap for a read-only config directory.
	// This dependency-free local hook must not trigger package downloads during a turn.
	// Upstream пропускает automatic npm bootstrap для read-only config directory.
	// Local hook без dependencies не должен загружать packages во время turn.
	directory := filepath.Join(home, "config", "opencode")
	if os.MkdirAll(directory, 0o700) != nil || os.WriteFile(filepath.Join(directory, ".gitignore"), []byte("*\n"), 0o400) != nil || os.Chmod(directory, 0o500) != nil {
		return "", ErrProtocol
	}
	return (&url.URL{Scheme: "file", Path: path}).String(), nil
}

// RemoveManagedHome removes a joined native process's private HOME, including its read-only config.
// Callers must separately prove process termination; this function grants no cleanup authority.
// RemoveManagedHome удаляет private HOME завершённого native process, включая read-only config.
// Caller отдельно доказывает process termination; функция не выдаёт cleanup authority.
func RemoveManagedHome(home string) error {
	root, err := os.OpenRoot(home)
	if err != nil {
		return ErrProtocol
	}
	defer root.Close()
	if err := root.Chmod("config/opencode", 0o700); err != nil && !os.IsNotExist(err) {
		return ErrProtocol
	}
	return os.RemoveAll(home)
}

// VerifyManagedToolIdentity removes the hook-only envelope after checking its exact execution and arguments.
// The opaque identity is suitable for receipt deduplication, never for authorization or a grant.
// VerifyManagedToolIdentity удаляет hook-only envelope после проверки exact execution и arguments.
// Opaque identity пригодна для receipt deduplication, но не authorization либо grant.
func VerifyManagedToolIdentity(raw json.RawMessage, server, revision, key string) (json.RawMessage, string, error) {
	var params struct {
		Name      string                     `json:"name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
		Metadata  json.RawMessage            `json:"_meta,omitempty"`
	}
	if len(raw) > 2<<20 || (server != "kodex" && server != "workspace") || len(key) != 64 || len(revision) != 64 || internalrpcauth.DecodeStrictJSON(raw, &params) != nil {
		return nil, "", ErrProtocol
	}
	if len(params.Metadata) > 0 {
		var metadata struct {
			ProgressToken json.RawMessage `json:"progressToken"`
		}
		if internalrpcauth.DecodeStrictJSON(params.Metadata, &metadata) != nil || len(metadata.ProgressToken) == 0 || len(metadata.ProgressToken) > 256 || bytes.Equal(metadata.ProgressToken, []byte("null")) {
			return nil, "", ErrProtocol
		}
		var token any
		if json.Unmarshal(metadata.ProgressToken, &token) != nil {
			return nil, "", ErrProtocol
		}
		switch token.(type) {
		case string, float64:
		default:
			return nil, "", ErrProtocol
		}
	}
	var proof struct {
		Payload string `json:"payload"`
		MAC     string `json:"mac"`
	}
	if internalrpcauth.DecodeStrictJSON(params.Arguments["__kodex_native"], &proof) != nil || len(proof.Payload) > 1<<20 {
		return nil, "", ErrProtocol
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(proof.Payload))
	provided, err := hex.DecodeString(proof.MAC)
	if err != nil || !hmac.Equal(provided, mac.Sum(nil)) {
		return nil, "", ErrProtocol
	}
	var payload struct {
		SessionID string                     `json:"session_id"`
		CallID    string                     `json:"call_id"`
		Tool      string                     `json:"tool"`
		Revision  string                     `json:"revision"`
		Input     map[string]json.RawMessage `json:"input"`
	}
	if internalrpcauth.DecodeStrictJSON([]byte(proof.Payload), &payload) != nil || !nativeSessionPattern.MatchString(payload.SessionID) || !nativeCallPattern.MatchString(payload.CallID) || payload.Tool != server+"_"+params.Name || payload.Revision != revision || payload.Input == nil {
		return nil, "", ErrProtocol
	}
	delete(params.Arguments, "__kodex_native")
	// Both objects are encoded by the same codec; map order and string escapes carry no authority.
	// Оба object кодируются одинаково: порядок map и string escapes не несут authority.
	actual, _ := json.Marshal(params.Arguments)
	expected, _ := json.Marshal(payload.Input)
	if !bytes.Equal(actual, expected) {
		return nil, "", ErrProtocol
	}
	clean, err := json.Marshal(params)
	if err != nil || len(clean) > 1<<20 {
		return nil, "", ErrProtocol
	}
	id := sha256.Sum256([]byte(payload.SessionID + "\x00" + payload.CallID))
	return clean, "ncall_" + hex.EncodeToString(id[:]), nil
}
