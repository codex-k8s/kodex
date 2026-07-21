package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sys/unix"
)

const (
	artifactManifestSchema = "mattercodex.artifact-manifest/v1"
	artifactMaxFiles       = 8
	artifactMaxObjectBytes = int64(8 << 20)
	artifactMaxTurnBytes   = int64(32 << 20)
)

var artifactRunIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

type artifactBridge struct {
	mu       sync.RWMutex
	active   *activeArtifactTurn
	server   *http.Server
	listener net.Listener
	runner   *runner
	openHook func()
}

type activeArtifactTurn struct {
	botServiceURL string
	sessionKey    string
	sessionToken  string
	turnID        string
	outboxDir     string
}

type publishArtifactToolInput struct {
	Path           string `json:"path" jsonschema:"относительный путь к файлу внутри MATTERCODEX_OUTPUT_DIR"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"устойчивый ключ идемпотентности публикации в пределах текущего хода"`
}

type artifactPublishResponse struct {
	ArtifactVersionID string `json:"artifact_version_id"`
	DeliveryID        string `json:"delivery_id"`
	State             string `json:"state"`
	MattermostPostID  string `json:"mattermost_post_id,omitempty"`
	Quarantined       bool   `json:"quarantined"`
}

type artifactQuarantineRequest struct {
	TurnID         string `json:"turn_id"`
	IdempotencyKey string `json:"idempotency_key"`
	OriginalName   string `json:"original_name"`
	MediaType      string `json:"media_type"`
	Size           int64  `json:"size"`
	SHA256         string `json:"sha256"`
	Reason         string `json:"reason"`
}

func (r *runner) startArtifactBridge(ctx context.Context, botServiceURL string, sessionKey string, sessionToken string) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("локальный мост артефактов не запущен: %w", err)
	}
	bridge := &artifactBridge{listener: listener, runner: r}
	server := mcp.NewServer(&mcp.Implementation{Name: "mattercodex-artifacts", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: "Публикуй только явно выбранные пользователем файлы из MATTERCODEX_OUTPUT_DIR. Путь должен быть относительным, а ключ идемпотентности — устойчивым для одного результата.",
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "publish_artifact",
		Description: "Безопасно публикует один файл из MATTERCODEX_OUTPUT_DIR в исходный Mattermost thread текущего хода. Текстовые файлы с секретами помещаются в карантин.",
	}, func(toolCtx context.Context, _ *mcp.CallToolRequest, input publishArtifactToolInput) (*mcp.CallToolResult, artifactPublishResponse, error) {
		result, publishErr := bridge.publish(toolCtx, input)
		if publishErr != nil {
			return artifactMCPError(publishErr.Error()), artifactPublishResponse{}, nil
		}
		return nil, result, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	bridge.server = &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	r.artifactBridge = bridge
	artifactURL := "http://" + listener.Addr().String() + "/mcp"
	if err := os.Setenv("MATTERCODEX_ARTIFACT_MCP_URL", artifactURL); err != nil {
		_ = listener.Close()
		r.artifactBridge = nil
		return err
	}
	go func() {
		if serveErr := bridge.server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && ctx.Err() == nil {
			r.safety.markUnsafe()
		}
	}()
	_ = botServiceURL
	_ = sessionKey
	_ = sessionToken
	return nil
}

func (r *runner) stopArtifactBridge(ctx context.Context) {
	if r.artifactBridge == nil {
		return
	}
	r.artifactBridge.deactivate()
	_ = r.artifactBridge.server.Shutdown(ctx)
	_ = r.artifactBridge.listener.Close()
	r.artifactBridge = nil
	_ = os.Unsetenv("MATTERCODEX_ARTIFACT_MCP_URL")
}

func (bridge *artifactBridge) activate(turn activeArtifactTurn) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	copy := turn
	bridge.active = &copy
}

func (bridge *artifactBridge) deactivate() {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.active = nil
}

func (bridge *artifactBridge) current() (activeArtifactTurn, error) {
	bridge.mu.RLock()
	defer bridge.mu.RUnlock()
	if bridge.active == nil {
		return activeArtifactTurn{}, fmt.Errorf("нет активного хода для публикации артефакта")
	}
	return *bridge.active, nil
}

func (r *runner) prepareTurnArtifacts(ctx context.Context, client *http.Client, botServiceURL string, sessionKey string, sessionToken string, claim sessionTurnClaimResponse) (string, error) {
	manifest := claim.ArtifactManifest
	if !artifactRunIDPattern.MatchString(claim.RunID) || manifest.SchemaVersion != artifactManifestSchema || manifest.TurnID != claim.RunID || len(manifest.Files) > artifactMaxFiles {
		return "", fmt.Errorf("манифест артефактов текущего хода отклонён")
	}
	root := filepath.Join(workspaceDir, ".matter-codex")
	inboxRoot := filepath.Join(root, "inbox")
	outboxRoot := filepath.Join(root, "outbox")
	for _, item := range []struct {
		path string
		mode os.FileMode
	}{{root, 0o700}, {inboxRoot, 0o700}, {outboxRoot, 0o700}} {
		if err := ensureArtifactDirectory(item.path, item.mode); err != nil {
			return "", err
		}
	}
	inboxDir := filepath.Join(inboxRoot, claim.RunID)
	outboxDir := filepath.Join(outboxRoot, claim.RunID)
	if err := os.Mkdir(inboxDir, 0o700); err != nil {
		return "", fmt.Errorf("inbox текущего хода не создан: %w", err)
	}
	cleanupInbox := true
	defer func() {
		if cleanupInbox {
			_ = os.RemoveAll(inboxDir)
		}
	}()
	if err := os.Mkdir(outboxDir, 0o700); err != nil {
		return "", fmt.Errorf("outbox текущего хода не создан: %w", err)
	}
	cleanupOutbox := true
	defer func() {
		if cleanupOutbox {
			_ = os.RemoveAll(outboxDir)
		}
	}()

	seenPaths := make(map[string]struct{}, len(manifest.Files))
	seenVersions := make(map[string]struct{}, len(manifest.Files))
	var total int64
	for _, entry := range manifest.Files {
		name, err := validateManifestEntry(entry, claim.RunID)
		if err != nil {
			return "", err
		}
		if _, exists := seenPaths[name]; exists {
			return "", fmt.Errorf("манифест содержит повторяющийся локальный путь")
		}
		if _, exists := seenVersions[entry.ArtifactVersionID]; exists {
			return "", fmt.Errorf("манифест содержит повторяющуюся версию")
		}
		seenPaths[name] = struct{}{}
		seenVersions[entry.ArtifactVersionID] = struct{}{}
		total += entry.Size
		if total > artifactMaxTurnBytes {
			return "", fmt.Errorf("суммарный размер вложений превышает предел")
		}
		if err := r.downloadTurnArtifact(ctx, client, botServiceURL, sessionKey, sessionToken, claim.RunID, entry, filepath.Join(inboxDir, name)); err != nil {
			return "", err
		}
	}
	manifestBody, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	manifestBody = append(manifestBody, '\n')
	manifestPath := filepath.Join(inboxDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestBody, 0o444); err != nil {
		return "", fmt.Errorf("манифест не записан в workspace: %w", err)
	}
	if err := os.Chmod(manifestPath, 0o444); err != nil {
		return "", err
	}
	if err := os.Chmod(inboxDir, 0o555); err != nil {
		return "", err
	}
	cleanupInbox = false
	cleanupOutbox = false
	return outboxDir, nil
}

func ensureArtifactDirectory(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, mode); err != nil {
			return fmt.Errorf("каталог артефактов не создан: %w", err)
		}
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("небезопасный каталог артефактов отклонён")
	}
	return os.Chmod(path, mode)
}

func validateManifestEntry(entry sessionArtifactManifestEntry, turnID string) (string, error) {
	if entry.Size < 0 || entry.Size > artifactMaxObjectBytes || !validArtifactHex(entry.SHA256, sha256.Size*2) || !validArtifactHex(entry.ArtifactVersionID, 32) {
		return "", fmt.Errorf("элемент манифеста имеет недопустимую идентичность, размер или хеш")
	}
	if entry.Source.Kind != "mattermost" || strings.TrimSpace(entry.Source.PostID) == "" || strings.TrimSpace(entry.Source.FileID) == "" {
		return "", fmt.Errorf("источник элемента манифеста отклонён")
	}
	extension, ok := artifactMediaExtensions()[entry.MediaType]
	if !ok {
		return "", fmt.Errorf("тип элемента манифеста не разрешён")
	}
	name := filepath.Base(entry.LocalPath)
	expectedNamePattern := regexp.MustCompile(`^[1-8]-` + regexp.QuoteMeta(entry.ArtifactVersionID) + regexp.QuoteMeta(extension) + `$`)
	expectedPrefix := "/workspace/.matter-codex/inbox/" + turnID + "/"
	if !strings.HasPrefix(entry.LocalPath, expectedPrefix) || strings.TrimPrefix(entry.LocalPath, expectedPrefix) != name || !expectedNamePattern.MatchString(name) {
		return "", fmt.Errorf("локальный путь элемента манифеста отклонён")
	}
	return name, nil
}

func (r *runner) downloadTurnArtifact(ctx context.Context, client *http.Client, botServiceURL string, sessionKey string, sessionToken string, turnID string, entry sessionArtifactManifestEntry, destination string) error {
	endpoint := strings.TrimRight(botServiceURL, "/") + "/internal/agent-sessions/" + url.PathEscape(sessionKey) + "/artifacts/" + url.PathEscape(entry.ArtifactVersionID) + "?turn_id=" + url.QueryEscape(turnID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+sessionToken)
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("вложение не получено из bot-service: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return sessionAPIStatusError{StatusCode: response.StatusCode, Body: "artifact download failed"}
	}
	if response.ContentLength != entry.Size || response.Header.Get("X-MatterCodex-Artifact-SHA256") != entry.SHA256 {
		return fmt.Errorf("метаданные загруженного вложения не совпадают с манифестом")
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, artifactMaxObjectBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	if closeErr != nil || written != entry.Size || written > artifactMaxObjectBytes || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
		_ = os.Remove(destination)
		return fmt.Errorf("содержимое загруженного вложения не совпадает с манифестом")
	}
	return os.Chmod(destination, 0o444)
}

func (bridge *artifactBridge) publish(ctx context.Context, input publishArtifactToolInput) (artifactPublishResponse, error) {
	active, err := bridge.current()
	if err != nil {
		return artifactPublishResponse{}, err
	}
	path := strings.TrimSpace(input.Path)
	key := strings.TrimSpace(input.IdempotencyKey)
	if !validArtifactOutputPath(path) || key == "" || len(key) > 200 {
		return artifactPublishResponse{}, fmt.Errorf("путь или ключ идемпотентности публикации отклонён")
	}
	directory, err := unix.Openat2(unix.AT_FDCWD, active.outboxDir, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return artifactPublishResponse{}, fmt.Errorf("outbox текущего хода недоступен")
	}
	defer unix.Close(directory)
	fd, err := unix.Openat2(directory, path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return artifactPublishResponse{}, fmt.Errorf("файл публикации не прошёл безопасное открытие")
	}
	file := os.NewFile(uintptr(fd), "artifact-output")
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Size < 0 || stat.Size > artifactMaxObjectBytes {
		return artifactPublishResponse{}, fmt.Errorf("файл публикации не является допустимым обычным файлом")
	}
	if bridge.openHook != nil {
		bridge.openHook()
	}
	body, err := io.ReadAll(io.LimitReader(file, artifactMaxObjectBytes+1))
	if err != nil || int64(len(body)) != stat.Size || int64(len(body)) > artifactMaxObjectBytes {
		return artifactPublishResponse{}, fmt.Errorf("файл публикации не прочитан в допустимых границах")
	}
	mediaType, err := detectArtifactMediaType(body)
	if err != nil {
		return artifactPublishResponse{}, err
	}
	hash := sha256.Sum256(body)
	hashText := hex.EncodeToString(hash[:])
	if artifactTextMediaType(mediaType) {
		protected, protectErr := bridge.runner.secrets.protect(string(body))
		if protectErr != nil || protected != string(body) {
			return bridge.quarantine(ctx, active, input, mediaType, int64(len(body)), hashText, "secret_detected")
		}
	}
	return bridge.sendArtifact(ctx, active, input, bytes.NewReader(body))
}

func (bridge *artifactBridge) sendArtifact(ctx context.Context, active activeArtifactTurn, input publishArtifactToolInput, body io.Reader) (artifactPublishResponse, error) {
	endpoint := strings.TrimRight(active.botServiceURL, "/") + "/internal/agent-sessions/" + url.PathEscape(active.sessionKey) + "/artifacts/publish"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return artifactPublishResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+active.sessionToken)
	request.Header.Set("X-MatterCodex-Turn-ID", active.turnID)
	request.Header.Set("Idempotency-Key", strings.TrimSpace(input.IdempotencyKey))
	request.Header.Set("X-MatterCodex-Artifact-Name", base64.RawURLEncoding.EncodeToString([]byte(input.Path)))
	request.Header.Set("Content-Type", "application/octet-stream")
	return bridge.doArtifactRequest(request, false)
}

func (bridge *artifactBridge) quarantine(ctx context.Context, active activeArtifactTurn, input publishArtifactToolInput, mediaType string, size int64, sha256Text string, reason string) (artifactPublishResponse, error) {
	payload, err := json.Marshal(artifactQuarantineRequest{
		TurnID: active.turnID, IdempotencyKey: strings.TrimSpace(input.IdempotencyKey), OriginalName: input.Path,
		MediaType: mediaType, Size: size, SHA256: sha256Text, Reason: reason,
	})
	if err != nil {
		return artifactPublishResponse{}, err
	}
	endpoint := strings.TrimRight(active.botServiceURL, "/") + "/internal/agent-sessions/" + url.PathEscape(active.sessionKey) + "/artifacts/quarantine"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return artifactPublishResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+active.sessionToken)
	request.Header.Set("Content-Type", "application/json")
	return bridge.doArtifactRequest(request, true)
}

func (bridge *artifactBridge) doArtifactRequest(request *http.Request, acceptQuarantine bool) (artifactPublishResponse, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return artifactPublishResponse{}, err
	}
	defer response.Body.Close()
	accepted := response.StatusCode == http.StatusOK || (acceptQuarantine && response.StatusCode == http.StatusUnprocessableEntity)
	if !accepted {
		return artifactPublishResponse{}, sessionAPIStatusError{StatusCode: response.StatusCode, Body: "artifact publish failed"}
	}
	var result artifactPublishResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&result); err != nil {
		return artifactPublishResponse{}, err
	}
	return result, nil
}

func validArtifactOutputPath(value string) bool {
	if value == "" || filepath.IsAbs(value) || filepath.Clean(value) != value || len(value) > 1024 {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		lower := strings.ToLower(part)
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") || artifactSecretFileName(lower) {
			return false
		}
	}
	for _, r := range value {
		if r == '\\' || r == '\x00' || unicode.IsControl(r) || unicode.In(r, unicode.Bidi_Control) {
			return false
		}
	}
	return true
}

func artifactSecretFileName(value string) bool {
	for _, marker := range []string{"secret", "credential", "password", "token", "kubeconfig", "auth.json", "private_key", "id_rsa", "id_ed25519"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func detectArtifactMediaType(body []byte) (string, error) {
	if len(body) == 0 {
		return "text/plain", nil
	}
	sample := body
	if len(sample) > 512 {
		sample = sample[:512]
	}
	detected := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(sample), ";")[0]))
	if bytes.HasPrefix(sample, []byte("%PDF-")) {
		detected = "application/pdf"
	} else if len(sample) >= 12 && string(sample[:4]) == "RIFF" && string(sample[8:12]) == "WEBP" {
		detected = "image/webp"
	} else if utf8.Valid(body) && !bytes.ContainsRune(body, '\x00') {
		if json.Valid(bytes.TrimSpace(body)) {
			detected = "application/json"
		} else {
			detected = "text/plain"
		}
	}
	if _, ok := artifactMediaExtensions()[detected]; !ok {
		return "", fmt.Errorf("тип файла публикации не разрешён")
	}
	return detected, nil
}

func artifactMediaExtensions() map[string]string {
	return map[string]string{
		"text/plain": ".txt", "text/markdown": ".md", "text/csv": ".csv", "application/json": ".json",
		"application/pdf": ".pdf", "image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif",
	}
}

func artifactTextMediaType(value string) bool {
	return value == "text/plain" || value == "text/markdown" || value == "text/csv" || value == "application/json"
}

func validArtifactHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func artifactMCPError(message string) *mcp.CallToolResult {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "публикация артефакта завершилась ошибкой"
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: message}}, IsError: true}
}
