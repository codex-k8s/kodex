package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/security"
)

const (
	// openCodeExecutable is an immutable image-owned binary, never a prompt-selected command.
	// openCodeExecutable — immutable image-owned binary, не команда из prompt.
	openCodeExecutable = "/usr/local/bin/opencode"
	// openCodeAPIKeyEnvironment confines the model key to the provider process environment.
	// openCodeAPIKeyEnvironment ограничивает ключ модели окружением provider process.
	openCodeAPIKeyEnvironment = "KODEX_MODEL_PROVIDER_API_KEY"
	// maximumOpenCodeEvents bounds one provider turn independently of the model output ceiling.
	// maximumOpenCodeEvents ограничивает turn независимо от предела ответа модели.
	maximumOpenCodeEvents = 4096
)

// errOpenCodeProvider is the safe adapter failure without provider text, paths or credentials.
// errOpenCodeProvider — безопасный отказ адаптера без текста провайдера, paths или credentials.
var errOpenCodeProvider = errors.New("OpenCode provider execution is unavailable")

// OpenCodeConfiguration builds only the managed-MCP profile; credentials remain environment references.
// It validates provider/model metadata and the exact local bridge, not execution authority or readiness.
// OpenCodeConfiguration создаёт только managed-MCP profile; credentials остаются environment references.
// Проверяет metadata и exact local bridge, не execution authority или readiness.
func OpenCodeConfiguration(input model.Input, bridgeURL string) ([]byte, error) {
	if input.Provider != "opencode" || input.ModelProvider == nil || input.ModelProvider.Validate() != nil ||
		input.ConfigOverlay != "" || input.CodexSessionID != "" || len(input.EnvironmentTools) != 0 ||
		input.EffectiveReasoningEffort != "" || input.ReasoningMode != runtimecontract.ReasoningUnsupported {
		return nil, ErrRuntimeProfile
	}
	selected, found := input.ModelProvider.Model(input.Model)
	endpoint, err := url.Parse(bridgeURL)
	if !found || err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || endpoint.RawPath != "" ||
		(endpoint.Path != "" && endpoint.Path != "/" && endpoint.Path != "/mcp") {
		return nil, ErrRuntimeProfile
	}
	port, err := strconv.Atoi(endpoint.Port())
	if err != nil || port < 1024 || port > 65535 || endpoint.Host != "127.0.0.1:"+strconv.Itoa(port) {
		return nil, ErrRuntimeProfile
	}
	provider := input.ModelProvider
	configuration := map[string]any{
		"$schema": "https://opencode.ai/config.json", "model": provider.ProviderID + "/" + input.Model,
		"small_model": provider.ProviderID + "/" + input.Model, "enabled_providers": []string{provider.ProviderID},
		"autoupdate": false, "share": "disabled", "snapshot": false, "default_agent": "build",
		"permission": map[string]string{"*": "deny", "kodex_*": "allow"},
		"agent":      map[string]any{"build": map[string]any{"steps": 8}},
		"provider": map[string]any{provider.ProviderID: map[string]any{
			"npm": "@ai-sdk/openai-compatible", "name": "Kodex managed model gateway",
			"options": map[string]any{"baseURL": provider.BaseURL, "apiKey": "{env:" + openCodeAPIKeyEnvironment + "}"},
			"models": map[string]any{input.Model: map[string]any{"name": input.Model,
				"limit": map[string]int{"context": selected.ContextWindowTokens, "output": selected.MaximumOutputTokens}}},
		}},
		"mcp": map[string]any{"kodex": map[string]any{"type": "remote", "url": bridgeURL, "enabled": true,
			"oauth": false, "headers": map[string]string{"Authorization": "Bearer {env:KODEX_MCP_PROXY_TOKEN}"}}},
	}
	raw, err := json.Marshal(configuration)
	if err != nil {
		return nil, ErrRuntimeProfile
	}
	return raw, nil
}

// executeOpenCodeTurn performs one isolated managed-MCP turn using owner-pinned canonical session history.
// Native OpenCode state is ephemeral; it never fabricates a Codex session ID or rollout archive.
// executeOpenCodeTurn выполняет isolated managed-MCP turn с owner-pinned канонической историей сессии.
// Native state OpenCode временно; Codex session ID или rollout archive не имитируются.
func executeOpenCodeTurn(ctx context.Context, input model.Input, prompt []byte, bridgeURL, mcpToken string, auth []byte, activity func(runtimecontract.RuntimeActivity) error) (Result, error) {
	if ValidateRuntimeProfile(input) != nil || activity == nil || len(prompt) == 0 || len(prompt) > 1<<20 {
		return Result{}, ErrRuntimeProfile
	}
	config, err := OpenCodeConfiguration(input, bridgeURL)
	if err != nil {
		return Result{}, err
	}
	snapshot, err := input.RequiredContextSnapshot(time.Now())
	if err != nil || verifyProviderContext(input, snapshot) != nil || runtimecontract.ValidateManagedMCPReadiness(input, time.Now()) != nil {
		return Result{}, ErrRuntimeProfile
	}
	ctx, cancel := snapshot.BoundExecutionContext(ctx)
	defer cancel()
	key, err := runtimecontract.DecodeGatewayProviderAuthentication(auth, input.ModelProvider.DefinitionKey())
	if err != nil {
		return Result{}, ErrProviderAuthentication
	}
	defer clear(key)
	if secureDirectory(input.CodexHome) != nil || security.VerifyProtectedRegular(openCodeExecutable, true) != nil {
		return Result{}, errOpenCodeProvider
	}
	home, err := os.MkdirTemp(input.CodexHome, "opencode-")
	if err != nil {
		return Result{}, errOpenCodeProvider
	}
	defer os.RemoveAll(home)
	for _, directory := range []string{"config", "cache", "data", "state"} {
		if os.Mkdir(filepath.Join(home, directory), 0o700) != nil {
			return Result{}, errOpenCodeProvider
		}
	}
	environment, err := openCodeEnvironment(home, config, string(key), mcpToken)
	if err != nil {
		return Result{}, err
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, 5*time.Second)
	probe := exec.CommandContext(probeCtx, openCodeExecutable, "--version")
	probe.Dir, probe.Env = home, []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + home}
	version, probeErr := probe.Output()
	probeCancel()
	if probeErr != nil || string(bytes.TrimSpace(version)) != runtimecontract.OpenCodeVersion {
		return Result{}, errOpenCodeProvider
	}
	command := exec.CommandContext(ctx, openCodeExecutable, "run", "--format", "json", "--title", "Kodex managed turn")
	command.Dir, command.Env = home, environment
	command.Stdin, command.Stderr = bytes.NewReader(prompt), io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGTERM) }
	command.WaitDelay = 3 * time.Second
	reader, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		return Result{}, errOpenCodeProvider
	}
	defer func() { _ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }()
	result, parseErr := readOpenCodeTurn(reader, input, activity, []string{string(key), mcpToken})
	if parseErr != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return failedProviderResult(result), ctx.Err()
	}
	if parseErr != nil || waitErr != nil {
		if errors.Is(parseErr, ErrProviderAuthentication) {
			return failedProviderResult(result), ErrProviderAuthentication
		}
		return failedProviderResult(result), errOpenCodeProvider
	}
	return result, nil
}

// openCodeEnvironment creates an explicit provider-only environment, excluding arbitrary role secrets.
// The managed MCP uses HTTP, so no credential-bearing shell/MCP child process is created.
// openCodeEnvironment создаёт explicit provider-only environment без произвольных role secrets.
// Managed MCP использует HTTP: credential-bearing shell/MCP child process не создаётся.
func openCodeEnvironment(home string, config []byte, key, mcpToken string) ([]string, error) {
	transport, err := runtimeTransportEnvironment()
	if err != nil {
		return nil, errOpenCodeProvider
	}
	values := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"), "XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
		"XDG_DATA_HOME=" + filepath.Join(home, "data"), "XDG_STATE_HOME=" + filepath.Join(home, "state"),
		"OPENCODE_CONFIG_CONTENT=" + string(config), "OPENCODE_DISABLE_PROJECT_CONFIG=true",
		"OPENCODE_DISABLE_AUTOUPDATE=true", "OPENCODE_DISABLE_MODELS_FETCH=true", "OPENCODE_DISABLE_AUTOCOMPACT=true",
		"OPENCODE_DISABLE_TERMINAL_TITLE=true", openCodeAPIKeyEnvironment + "=" + key, "KODEX_MCP_PROXY_TOKEN=" + mcpToken}
	for _, name := range runtimeTransportEnvironmentNames {
		values = append(values, name+"="+transport[name])
	}
	return values, nil
}

// openCodeEvent decodes only the pinned CLI event envelope; reasoning and raw errors are never published.
// openCodeEvent разбирает pinned CLI envelope; reasoning и raw errors не публикуются.
type openCodeEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionID"`
	Part      json.RawMessage `json:"part"`
	Error     json.RawMessage `json:"error"`
}

// openCodePart holds the bounded fields required to classify one completed text/tool/usage item.
// openCodePart хранит bounded fields для классификации completed text/tool/usage item.
type openCodePart struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Text   string `json:"text"`
	Tool   string `json:"tool"`
	Reason string `json:"reason"`
	State  struct {
		Status string `json:"status"`
	} `json:"state"`
	Tokens *struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

// readOpenCodeTurn consumes a bounded JSONL stream and emits completed messages through the existing callback.
// SDK-normalized counters remain PARTIAL: numeric fields do not prove complete upstream usage reporting.
// readOpenCodeTurn читает bounded JSONL и передаёт completed messages через прежний callback.
// Нормализованные SDK counters остаются PARTIAL: числа не доказывают полноту upstream usage.
func readOpenCodeTurn(reader io.Reader, input model.Input, activity func(runtimecontract.RuntimeActivity) error, secrets []string) (Result, error) {
	result := Result{UsageCompleteness: runtimecontract.UsageUnknown}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	sessionID, text := "", ""
	started, finished := 0, 0
	terminal := false
	seen := map[string]bool{}
	for count := 0; scanner.Scan(); count++ {
		if count >= maximumOpenCodeEvents || !utf8.Valid(scanner.Bytes()) || rejectDuplicateJSONKeys(scanner.Bytes()) != nil {
			return result, errOpenCodeProvider
		}
		var event openCodeEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			return result, errOpenCodeProvider
		}
		if event.Type == "error" {
			var failure struct {
				Data struct {
					StatusCode int `json:"statusCode"`
				} `json:"data"`
			}
			if json.Unmarshal(event.Error, &failure) == nil && (failure.Data.StatusCode == 401 || failure.Data.StatusCode == 403) {
				return result, ErrProviderAuthentication
			}
			return result, errOpenCodeProvider
		}
		if event.SessionID == "" || len(event.SessionID) > 128 {
			return result, errOpenCodeProvider
		}
		if sessionID == "" {
			sessionID = event.SessionID
		}
		if event.SessionID != sessionID || terminal {
			return result, errOpenCodeProvider
		}
		var part openCodePart
		if json.Unmarshal(event.Part, &part) != nil || part.ID == "" || seen[part.ID] {
			return result, errOpenCodeProvider
		}
		seen[part.ID] = true
		switch event.Type {
		case "step_start":
			if started != finished || part.Type != "step-start" {
				return result, errOpenCodeProvider
			}
			started++
			text = ""
			if started == 1 {
				observation := runtimecontract.BindProviderProcessObservation(input, runtimecontract.OpenCodeVersion)
				if activity(runtimecontract.RuntimeActivity{ProviderProcess: &observation}) != nil {
					return result, errOpenCodeProvider
				}
			}
		case "text":
			if started != finished+1 || part.Type != "text" || !utf8.ValidString(part.Text) {
				return result, errOpenCodeProvider
			}
			text += part.Text
			if len(text) > runtimecontract.MaximumRuntimeMessageBytes {
				return result, errOpenCodeProvider
			}
		case "tool_use":
			if started != finished+1 || part.Type != "tool" || !strings.HasPrefix(part.Tool, "kodex_") ||
				(part.State.Status != "completed" && part.State.Status != "error") {
				return result, errOpenCodeProvider
			}
		case "step_finish":
			if started != finished+1 || part.Type != "step-finish" || part.Tokens == nil {
				return result, errOpenCodeProvider
			}
			finished++
			usage := runtimecontract.TokenUsage{InputTokens: part.Tokens.Input + part.Tokens.Cache.Read + part.Tokens.Cache.Write,
				OutputTokens: part.Tokens.Output + part.Tokens.Reasoning, ReasoningOutputTokens: part.Tokens.Reasoning,
				CachedInputTokens: part.Tokens.Cache.Read, CacheWriteInputTokens: part.Tokens.Cache.Write}
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
			if part.Tokens.Input < 0 || part.Tokens.Output < 0 || part.Tokens.Reasoning < 0 || part.Tokens.Cache.Read < 0 || part.Tokens.Cache.Write < 0 || usage.Validate() != nil {
				return result, errOpenCodeProvider
			}
			result.Usage.InputTokens += usage.InputTokens
			result.Usage.OutputTokens += usage.OutputTokens
			result.Usage.TotalTokens += usage.TotalTokens
			result.Usage.CachedInputTokens += usage.CachedInputTokens
			result.Usage.CacheWriteInputTokens += usage.CacheWriteInputTokens
			result.Usage.ReasoningOutputTokens += usage.ReasoningOutputTokens
			if result.Usage.Validate() != nil {
				return Result{}, errOpenCodeProvider
			}
			if result.Usage.TotalTokens > 0 {
				result.UsageCompleteness = runtimecontract.UsagePartial
			}
			if part.Reason != "stop" && part.Reason != "tool-calls" {
				return result, errOpenCodeProvider
			}
			terminal = part.Reason == "stop"
			for _, secret := range secrets {
				if secret != "" {
					text = strings.ReplaceAll(text, secret, "[REDACTED]")
				}
			}
			if strings.TrimSpace(text) != "" {
				phase := runtimecontract.RuntimeMessageCommentary
				if terminal {
					phase = runtimecontract.RuntimeMessageFinal
				}
				message := runtimecontract.RuntimeAgentMessage{ItemID: "opencode-" + strconv.Itoa(finished), Phase: phase, Revision: 1, Text: text}
				if message.Validate() != nil || activity(runtimecontract.RuntimeActivity{Message: &message}) != nil {
					return result, errOpenCodeProvider
				}
			}
		default:
			return result, errOpenCodeProvider
		}
	}
	if scanner.Err() != nil || !terminal || started != finished || strings.TrimSpace(text) == "" {
		return result, errOpenCodeProvider
	}
	result.Outcome, result.FinalMessage = "SUCCEEDED", text
	return result, nil
}
