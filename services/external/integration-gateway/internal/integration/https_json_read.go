package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

const maximumHTTPSJSONBodyBytes = 16 << 10

// executeHTTPSJSONRead использует только закреплённые поля соединения; input
// capability пуст, поэтому модель не выбирает URL, путь или HTTP method.
func (adapter *Adapter) executeHTTPSJSONRead(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (Result, error) {
	if request.Operation != "https_json.resource.read" || !integrationpackage.ValidHTTPSResourcePath(configuration["resource_path"]) {
		return Result{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	body, err := adapter.callProvider(ctx, providerCall{
		BaseURL: configuration["base_url"], Method: http.MethodGet,
		Path: configuration["resource_path"], AuthScheme: "BEARER",
		Credential: request.Credential, Capability: capability,
	})
	if err != nil {
		return Result{}, err
	}
	if len(body) == 0 || len(body) > maximumHTTPSJSONBodyBytes {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	var document any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&document) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	switch document.(type) {
	case map[string]any, []any:
	default:
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	canonical, err := json.Marshal(document)
	if err != nil || len(canonical) > maximumHTTPSJSONBodyBytes {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	endpointDigest := sha256.Sum256([]byte(configuration["base_url"] + configuration["resource_path"]))
	return providerResult(request, "https-json:"+hex.EncodeToString(endpointDigest[:]), map[string]any{"body_json": string(canonical)})
}
