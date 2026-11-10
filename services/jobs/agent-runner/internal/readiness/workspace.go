package readiness

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/managedworkspace"
)

// workspaceOwnerCall routes local-tool admission and repository reads through the exact existing
// authenticated MCP path, accepting only structured observed results and never raw fallback text.
// workspaceOwnerCall направляет local-tool admission и repository reads через exact existing
// authenticated MCP path, принимая лишь structured observed result без raw fallback text.
func workspaceOwnerCall(client *http.Client, endpoint *url.URL, localToken string) managedworkspace.OwnerCall {
	return func(ctx context.Context, id, name string, arguments map[string]any) (map[string]any, error) {
		raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}})
		if err != nil {
			return nil, errors.New("workspace owner request is invalid")
		}
		response, media, code, err := postMCP(ctx, client, endpoint, localToken, raw)
		var wire struct {
			JSONRPC string `json:"jsonrpc"`
			ID      string `json:"id"`
			Result  struct {
				Content    []map[string]any `json:"content"`
				Structured map[string]any   `json:"structuredContent"`
				IsError    bool             `json:"isError,omitempty"`
			} `json:"result"`
			Error json.RawMessage `json:"error,omitempty"`
		}
		if err != nil || code != http.StatusOK || media != "application/json" || internalrpcauth.DecodeStrictJSON(response, &wire) != nil || wire.JSONRPC != "2.0" || wire.ID != id || wire.Result.IsError || len(wire.Error) != 0 || wire.Result.Structured == nil {
			return nil, errors.New("workspace owner admission is unavailable")
		}
		return wire.Result.Structured, nil
	}
}

// QuiesceWorkspace closes local admission and verifies all command descendants stopped before
// ordinary file capture or workspace publication. Failure is not a writer-stop receipt.
// QuiesceWorkspace закрывает local admission и проверяет остановку command descendants до
// ordinary file capture либо workspace publication. Отказ не является writer-stop receipt.
func (proxy *MCPProxy) QuiesceWorkspace(ctx context.Context) error {
	if proxy.workspace == nil {
		return nil
	}
	return proxy.workspace.Close(ctx)
}
