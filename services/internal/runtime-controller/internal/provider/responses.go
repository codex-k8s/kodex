// Package provider реализует изолированный adapter модели. Модель не получает
// файловую систему, runtime-grants или secret material.
package provider

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	responseapi "github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

const (
	openAIBaseURL        = "https://api.openai.com/v1"
	maximumSecretBytes   = 16 << 10
	maximumToolRounds    = 4
	maximumInputBytes    = 256 << 10
	maximumToolOutput    = 4000
	maximumResultBytes   = 100_000
	maximumResponseToken = 4096
)

//go:embed prompts/*.txt
var promptFiles embed.FS

type Message struct{ Role, Content string }
type DelegationTarget struct{ Ref, Name, Purpose, RoleDescription string }

type Request struct {
	IdempotencyKey, Model, Instructions, Task string
	InputJSON                                 json.RawMessage
	SessionContext                            []Message
	DelegationTargets                         []DelegationTarget
	AllowDelegation                           bool
}

type ToolCall struct {
	Name, CallID, TargetAgentRef, Task string
}

type ToolHandler func(context.Context, ToolCall) (string, error)

type Result struct {
	Text                      string
	ResponseRef               string
	InputTokens, OutputTokens int64
}

type SafeError struct{ Code string }

func (err *SafeError) Error() string { return err.Code }

type Responses struct {
	client         *http.Client
	credentialFile string
	baseURL        string
}

func NewResponses(proxyAddress, credentialFile string) (*Responses, error) {
	proxyURL, err := url.Parse(proxyAddress)
	if err != nil || proxyURL.Scheme != "http" || proxyURL.Host != "egress-gateway.mattercodex-system.svc.cluster.local:8080" || proxyURL.Path != "" || proxyURL.RawQuery != "" || proxyURL.User != nil || !path.IsAbs(credentialFile) {
		return nil, errors.New("provider configuration is invalid")
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyURL(proxyURL),
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "api.openai.com"},
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &Responses{client: &http.Client{Transport: transport}, credentialFile: credentialFile, baseURL: openAIBaseURL}, nil
}

func (adapter *Responses) CloseIdleConnections() {
	if transport, ok := adapter.client.Transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}

func (adapter *Responses) Check(ctx context.Context, providerName, model string) error {
	if providerName != "openai-codex" || !validIdentifier(model) {
		return &SafeError{Code: "RUNTIME_PROFILE_UNSUPPORTED"}
	}
	client, err := adapter.authorizedClient()
	if err != nil {
		return err
	}
	if _, err := client.Models.Get(ctx, model); err != nil {
		return providerError(err)
	}
	return nil
}

func (adapter *Responses) Execute(ctx context.Context, input Request, handler ToolHandler) (Result, error) {
	if !validIdentifier(input.Model) || input.IdempotencyKey == "" || strings.TrimSpace(input.Instructions) == "" || strings.TrimSpace(input.Task) == "" || handler == nil {
		return Result{}, &SafeError{Code: "RUNTIME_INPUT_INVALID"}
	}
	prompt, err := buildPrompt(input)
	if err != nil {
		return Result{}, err
	}
	client, err := adapter.authorizedClient()
	if err != nil {
		return Result{}, err
	}
	requestInput := responseapi.ResponseNewParamsInputUnion{OfString: openai.String(prompt)}
	var result Result
	for round := 0; round < maximumToolRounds; round++ {
		parameters := responseapi.ResponseNewParams{
			Model:             shared.ResponsesModel(input.Model),
			Instructions:      openai.String(input.Instructions),
			Input:             requestInput,
			Store:             openai.Bool(false),
			ParallelToolCalls: openai.Bool(false),
			MaxOutputTokens:   openai.Int(maximumResponseToken),
			Include:           []responseapi.ResponseIncludable{responseapi.ResponseIncludableReasoningEncryptedContent},
		}
		if input.AllowDelegation && len(input.DelegationTargets) > 0 {
			parameters.Tools = []responseapi.ToolUnionParam{{OfFunction: delegationTool(input.DelegationTargets)}}
		}
		response, createErr := client.Responses.New(ctx, parameters, option.WithHeader("Idempotency-Key", input.IdempotencyKey+"-"+roundKey(round)))
		if createErr != nil {
			return Result{}, providerError(createErr)
		}
		if response == nil || response.ID == "" || response.Status != "completed" || response.Error.Code != "" {
			return Result{}, &SafeError{Code: "PROVIDER_RESPONSE_INVALID"}
		}
		result.ResponseRef = response.ID
		result.InputTokens += response.Usage.InputTokens
		result.OutputTokens += response.Usage.OutputTokens
		nextInput, calls, decodeErr := continuationInput(response.Output)
		if decodeErr != nil {
			return Result{}, decodeErr
		}
		if len(calls) == 0 {
			result.Text = boundedText(response.OutputText(), maximumResultBytes)
			if result.Text == "" {
				return Result{}, &SafeError{Code: "PROVIDER_EMPTY_RESULT"}
			}
			return result, nil
		}
		for _, call := range calls {
			output, toolErr := handler(ctx, call)
			if toolErr != nil {
				output = `{"ok":false,"error":"operation_rejected"}`
			}
			nextInput = append(nextInput, responseapi.ResponseInputItemParamOfFunctionCallOutput(call.CallID, boundedText(output, maximumToolOutput)))
		}
		requestInput = responseapi.ResponseNewParamsInputUnion{OfInputItemList: nextInput}
	}
	return Result{}, &SafeError{Code: "PROVIDER_TOOL_LIMIT"}
}

func continuationInput(items []responseapi.ResponseOutputItemUnion) ([]responseapi.ResponseInputItemUnionParam, []ToolCall, error) {
	result := make([]responseapi.ResponseInputItemUnionParam, 0, len(items))
	calls := make([]ToolCall, 0, len(items))
	for _, item := range items {
		switch item.Type {
		case "reasoning":
			value := item.AsReasoning().ToParam()
			result = append(result, responseapi.ResponseInputItemUnionParam{OfReasoning: &value})
		case "function_call":
			value := item.AsFunctionCall()
			call, err := decodeToolCall(value)
			if err != nil {
				return nil, nil, err
			}
			parameter := value.ToParam()
			result = append(result, responseapi.ResponseInputItemUnionParam{OfFunctionCall: &parameter})
			calls = append(calls, call)
		case "message":
			value := item.AsMessage().ToParam()
			result = append(result, responseapi.ResponseInputItemUnionParam{OfOutputMessage: &value})
		default:
			return nil, nil, &SafeError{Code: "PROVIDER_RESPONSE_INVALID"}
		}
	}
	return result, calls, nil
}

func buildPrompt(input Request) (string, error) {
	var boundedInput json.RawMessage
	if len(input.InputJSON) == 0 {
		boundedInput = json.RawMessage(`{}`)
	} else if !json.Valid(input.InputJSON) || len(input.InputJSON) > maximumInputBytes {
		return "", &SafeError{Code: "RUNTIME_INPUT_TOO_LARGE"}
	} else {
		boundedInput = append(json.RawMessage(nil), input.InputJSON...)
	}
	payload := struct {
		Task             string             `json:"task"`
		Input            json.RawMessage    `json:"input"`
		PreviousMessages []Message          `json:"previous_messages,omitempty"`
		AvailableAgents  []DelegationTarget `json:"available_agents,omitempty"`
	}{Task: strings.TrimSpace(input.Task), Input: boundedInput, PreviousMessages: input.SessionContext}
	if input.AllowDelegation {
		payload.AvailableAgents = input.DelegationTargets
	}
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > maximumInputBytes {
		return "", &SafeError{Code: "RUNTIME_INPUT_TOO_LARGE"}
	}
	prefix, err := promptFiles.ReadFile("prompts/execution.txt")
	if err != nil {
		return "", errors.New("read execution prompt")
	}
	return strings.TrimSpace(string(prefix)) + "\n\n" + string(raw), nil
}

func delegationTool(targets []DelegationTarget) *responseapi.FunctionToolParam {
	refs := make([]string, 0, len(targets))
	for _, target := range targets {
		refs = append(refs, target.Ref)
	}
	description, _ := promptFiles.ReadFile("prompts/delegate_description.txt")
	return &responseapi.FunctionToolParam{
		Name:        "delegate_agent",
		Description: openai.String(strings.TrimSpace(string(description))),
		Strict:      openai.Bool(true),
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"target_agent_ref": map[string]any{"type": "string", "enum": refs},
				"task":             map[string]any{"type": "string", "minLength": 1, "maxLength": 4000},
			},
			"required": []string{"target_agent_ref", "task"},
		},
	}
}

func decodeToolCall(item responseapi.ResponseFunctionToolCall) (ToolCall, error) {
	if item.Name != "delegate_agent" || item.CallID == "" || len(item.Arguments) > 16<<10 {
		return ToolCall{}, &SafeError{Code: "PROVIDER_TOOL_INVALID"}
	}
	var arguments struct {
		TargetAgentRef string `json:"target_agent_ref"`
		Task           string `json:"task"`
	}
	decoder := json.NewDecoder(strings.NewReader(item.Arguments))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&arguments) != nil || decoder.Decode(&struct{}{}) != io.EOF || strings.TrimSpace(arguments.TargetAgentRef) == "" || strings.TrimSpace(arguments.Task) == "" || len(arguments.Task) > 4000 {
		return ToolCall{}, &SafeError{Code: "PROVIDER_TOOL_INVALID"}
	}
	return ToolCall{Name: item.Name, CallID: item.CallID, TargetAgentRef: arguments.TargetAgentRef, Task: strings.TrimSpace(arguments.Task)}, nil
}

func (adapter *Responses) authorizedClient() (openai.Client, error) {
	key, err := readCredential(adapter.credentialFile)
	if err != nil {
		return openai.Client{}, &SafeError{Code: "PROVIDER_AUTH_UNAVAILABLE"}
	}
	return openai.NewClient(
		option.WithAPIKey(key),
		option.WithBaseURL(adapter.baseURL),
		option.WithHTTPClient(adapter.client),
		option.WithMaxRetries(0),
	), nil
}

func providerError(err error) error {
	var apiError *openai.Error
	if !errors.As(err, &apiError) {
		return &SafeError{Code: "PROVIDER_UNAVAILABLE"}
	}
	switch apiError.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &SafeError{Code: "PROVIDER_AUTH_REJECTED"}
	case http.StatusTooManyRequests:
		return &SafeError{Code: "PROVIDER_RATE_LIMITED"}
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
		return &SafeError{Code: "PROVIDER_REQUEST_REJECTED"}
	default:
		return &SafeError{Code: "PROVIDER_UNAVAILABLE"}
	}
}

func readCredential(fileName string) (string, error) {
	info, err := os.Lstat(fileName)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 || info.Size() > maximumSecretBytes || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("provider credential file is invalid")
	}
	raw, err := os.ReadFile(fileName)
	if err != nil || len(raw) == 0 || len(raw) > maximumSecretBytes {
		return "", errors.New("read provider credential")
	}
	value := strings.TrimSpace(string(raw))
	for index := range raw {
		raw[index] = 0
	}
	if value == "" || strings.ContainsAny(value, "\r\n\t ") {
		return "", errors.New("provider credential is invalid")
	}
	return value, nil
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func boundedText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit])
}

func roundKey(round int) string { return string(rune('1' + round)) }
