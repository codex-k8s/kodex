package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	native "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/opencode"
)

// nativeMCPResponseKey is private request-local metadata, never an inbound header.
// nativeMCPResponseKey — private request-local metadata, не inbound header.
type nativeMCPResponseKey struct{}

// nativeMCPResponse restores the SDK transport ID after the owner consumes the stable native identity.
// nativeMCPResponse восстанавливает SDK transport ID после передачи stable native identity владельцу.
type nativeMCPResponse struct {
	Original  json.RawMessage
	Forwarded json.RawMessage
}

// bindNativeMCPRequest verifies the image hook before replacing an SDK-generated transport ID.
// Non-tool protocol messages remain unchanged; raw model arguments cannot choose a receipt identity.
// bindNativeMCPRequest проверяет image hook перед заменой SDK-generated transport ID.
// Другие protocol messages сохраняются; raw model arguments не выбирают receipt identity.
func bindNativeMCPRequest(request *http.Request, input model.Input, token string) error {
	if input.Provider != "opencode" || request.Method != http.MethodPost {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, 2<<20+1))
	closeErr := request.Body.Close()
	var rpc struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}
	if err != nil || closeErr != nil || len(raw) > 2<<20 || internalrpcauth.DecodeStrictJSON(raw, &rpc) != nil || rpc.JSONRPC != "2.0" {
		return native.ErrProtocol
	}
	if rpc.Method == "tools/call" {
		server := "kodex"
		if request.URL.Path == "/workspace/mcp" {
			server = "workspace"
		}
		clean, id, err := native.VerifyManagedToolIdentity(rpc.Params, server, input.RuntimeRevisionDigest, token)
		if err != nil || len(rpc.ID) == 0 {
			return native.ErrProtocol
		}
		forwarded, _ := json.Marshal(id)
		*request = *request.WithContext(context.WithValue(request.Context(), nativeMCPResponseKey{}, nativeMCPResponse{Original: rpc.ID, Forwarded: forwarded}))
		rpc.Params, rpc.ID = clean, forwarded
		raw, err = json.Marshal(rpc)
		if err != nil {
			return native.ErrProtocol
		}
	}
	if len(raw) > 1<<20 {
		return native.ErrProtocol
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	request.ContentLength = int64(len(raw))
	request.Header.Set("Content-Length", strconv.Itoa(len(raw)))
	return nil
}

// restoreNativeMCPResponse keeps native receipt IDs off the SDK's JSON-RPC correlation boundary.
// It checks bounded exact JSON rather than trusting an arbitrary response or retrying a tool.
// restoreNativeMCPResponse отделяет native receipt IDs от SDK JSON-RPC correlation.
// Проверяет bounded exact JSON без доверия произвольному response или повтора tool.
func restoreNativeMCPResponse(response *http.Response) error {
	binding, ok := response.Request.Context().Value(nativeMCPResponseKey{}).(nativeMCPResponse)
	if !ok {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20+1))
	closeErr := response.Body.Close()
	var rpc struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   json.RawMessage `json:"error,omitempty"`
	}
	if err != nil || closeErr != nil || len(raw) > 2<<20 || internalrpcauth.DecodeStrictJSON(raw, &rpc) != nil || rpc.JSONRPC != "2.0" || !bytes.Equal(rpc.ID, binding.Forwarded) {
		return native.ErrProtocol
	}
	rpc.ID = binding.Original
	raw, err = json.Marshal(rpc)
	if err != nil {
		return native.ErrProtocol
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	response.ContentLength = int64(len(raw))
	response.Header.Set("Content-Length", strconv.Itoa(len(raw)))
	return nil
}
