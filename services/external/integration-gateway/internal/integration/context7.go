package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (adapter *Adapter) executeContext7(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	tool := context7Tool(request.Operation)
	if tool == "" || capability.Risk != "READ" || request.ApprovalPolicy != "NONE" || configuration["base_url"] != context7Origin {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	ctx, cancel := context.WithTimeout(ctx, min(adapter.timeout, context7Deadline))
	defer cancel()
	credential, err := adapter.readCredential(ctx, request.Credential)
	if err != nil {
		return Result{}, err
	}
	defer clear(credential)
	session, err := adapter.openContext7(ctx, credential)
	if err != nil {
		return Result{}, err
	}
	defer session.Close()
	if err := context7Probe(ctx, session); err != nil {
		return Result{}, err
	}
	var arguments map[string]any
	if json.Unmarshal(canonicalInput, &arguments) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	field, upstreamField := "library_name", "libraryName"
	if tool == "query-docs" {
		field, upstreamField = "library_id", "libraryId"
	}
	library, libraryOK := arguments[field].(string)
	query, queryOK := arguments["query"].(string)
	if len(arguments) != 2 || !libraryOK || !queryOK {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	arguments = map[string]any{upstreamField: library, "query": query}
	response, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		return Result{}, context7SafeError(err)
	}
	text, err := context7Text(response, credential)
	if err != nil {
		return Result{}, err
	}
	digest := sha256.Sum256([]byte(text))
	return providerResult(request, "context7:"+tool+":"+hex.EncodeToString(digest[:]), map[string]any{"text": text})
}

func (adapter *Adapter) testContext7(ctx context.Context, request Request) (string, error) {
	configuration, err := normalizeStringMap(request.Configuration)
	if err != nil || len(configuration) != 1 || configuration["base_url"] != context7Origin {
		return "", &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	ctx, cancel := context.WithTimeout(ctx, min(adapter.timeout, 15*time.Second))
	defer cancel()
	credential, err := adapter.readCredential(ctx, request.Credential)
	if err != nil {
		return "", err
	}
	defer clear(credential)
	session, err := adapter.openContext7(ctx, credential)
	if err != nil {
		return "", err
	}
	defer session.Close()
	if err := context7Probe(ctx, session); err != nil {
		return "", err
	}
	return "i18n:INTEGRATION_TEST_SUCCEEDED", nil
}

func (adapter *Adapter) openContext7(ctx context.Context, credential []byte) (*mcp.ClientSession, error) {
	if len(credential) < 1 || len(credential) > 4096 || adapter.context7HTTPClient == nil || adapter.context7HTTPClient.Transport == nil {
		return nil, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}
	}
	for _, value := range credential {
		if value < 33 || value > 126 {
			return nil, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}
		}
	}
	client := *adapter.context7HTTPClient
	client.Jar = nil
	client.Transport = context7Transport{base: client.Transport, credential: string(credential)}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"} }
	protocolClient := mcp.NewClient(&mcp.Implementation{Name: "kodex-context7", Version: "1.0.0"}, &mcp.ClientOptions{
		Capabilities: &mcp.ClientCapabilities{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	session, err := protocolClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: context7Endpoint, HTTPClient: &client, DisableStandaloneSSE: true, MaxRetries: -1, MaxEventSize: context7ResponseBytes,
	}, nil)
	if err != nil {
		return nil, context7SafeError(err)
	}
	return session, nil
}

func context7Probe(ctx context.Context, session *mcp.ClientSession) error {
	response, err := session.ListTools(ctx, nil)
	if err != nil {
		return context7SafeError(err)
	}
	if response == nil || response.NextCursor != "" || len(response.Tools) > 32 {
		return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	found := map[string]bool{}
	for _, tool := range response.Tools {
		if tool == nil || found[tool.Name] {
			return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		if tool.Name != "resolve-library-id" && tool.Name != "query-docs" {
			continue
		}
		field := "libraryName"
		if tool.Name == "query-docs" {
			field = "libraryId"
		}
		if !context7Schema(tool.InputSchema, field) {
			return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		found[tool.Name] = true
	}
	if !found["resolve-library-id"] || !found["query-docs"] {
		return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return nil
}

func context7Schema(raw any, field string) bool {
	schema, ok := raw.(map[string]any)
	if !ok || schema["type"] != "object" {
		return false
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{field, "query"} {
		property, ok := properties[key].(map[string]any)
		if !ok || property["type"] != "string" {
			return false
		}
	}
	required, ok := schema["required"].([]any)
	if !ok || len(required) != 2 {
		return false
	}
	first, firstOK := required[0].(string)
	second, secondOK := required[1].(string)
	return firstOK && secondOK && first != second && (first == field && second == "query" || second == field && first == "query")
}

func context7Text(response *mcp.CallToolResult, credential []byte) (string, error) {
	if response == nil || response.IsError || len(response.Content) == 0 || len(response.Content) > 64 {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	var output strings.Builder
	for index, content := range response.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok || !utf8.ValidString(text.Text) || strings.Contains(text.Text, string(credential)) {
			return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		for _, character := range text.Text {
			if character < 32 && character != '\n' && character != '\r' && character != '\t' {
				return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
		}
		if index > 0 {
			output.WriteByte('\n')
		}
		if output.Len()+len(text.Text) > context7TextBytes {
			return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		output.WriteString(text.Text)
	}
	if strings.TrimSpace(output.String()) == "" {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return output.String(), nil
}

func context7Tool(operation string) string {
	switch operation {
	case "context7.library.resolve":
		return "resolve-library-id"
	case "context7.docs.query":
		return "query-docs"
	default:
		return ""
	}
}

func context7SafeError(err error) error {
	var safe *SafeError
	if errors.As(err, &safe) {
		return safe
	}
	return &SafeError{Code: "INTEGRATION_UNAVAILABLE", Transient: true}
}
