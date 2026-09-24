package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// executeOpenAPI принимает только проверенный package binding. Вход модели
// заполняет закреплённые path/query/body, но не выбирает host, method,
// authentication header или idempotency contract.
func (adapter *Adapter) executeOpenAPI(ctx context.Context, request Request, capability integrationpackage.Capability,
	configuration map[string]string, canonicalInput []byte) (Result, error) {
	binding := capability.OpenAPI
	if binding == nil || binding.ServerOrigin == "" || configuration["base_url"] != binding.ServerOrigin {
		return Result{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	decoder := json.NewDecoder(bytes.NewReader(canonicalInput))
	decoder.UseNumber()
	var input map[string]any
	if decoder.Decode(&input) != nil || input == nil || decoder.Decode(&struct{}{}) != io.EOF {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	path, err := openAPIRequestPath(binding.Path, input["path"])
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	query, err := openAPIRequestQuery(input["query"])
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	response, err := adapter.callProviderResponse(ctx, providerCall{
		BaseURL: configuration["base_url"], Method: binding.Method, Path: path,
		Query: query, Body: input["body"], AuthScheme: binding.AuthScheme, AuthHeader: binding.AuthHeader,
		Credential: request.Credential, Capability: capability, EffectKey: request.EffectKey,
		IdempotencyHeader: binding.IdempotencyHeader,
		Client:            adapter.openAPIHTTPClient,
	})
	if err != nil {
		return Result{}, err
	}
	if len(response.Body) > maximumHTTPSJSONBodyBytes {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	var document any
	if len(response.Body) != 0 {
		decoder = json.NewDecoder(bytes.NewReader(response.Body))
		decoder.UseNumber()
		if decoder.Decode(&document) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	}
	canonicalResponse, err := json.Marshal(document)
	if err != nil || len(canonicalResponse) > maximumHTTPSJSONBodyBytes {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	digest := sha256.Sum256(response.Body)
	return providerResult(request, "openapi:"+binding.OperationID+":"+hex.EncodeToString(digest[:]),
		map[string]any{"body_json": string(canonicalResponse)})
}

func openAPIRequestPath(template string, raw any) (string, error) {
	parameters := map[string]any{}
	if raw != nil {
		var ok bool
		parameters, ok = raw.(map[string]any)
		if !ok {
			return "", errors.New("OpenAPI path parameters are invalid")
		}
	}
	segments := strings.Split(template, "/")
	for index, segment := range segments {
		if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
			continue
		}
		value, ok := parameters[segment[1:len(segment)-1]]
		if !ok {
			return "", errors.New("OpenAPI path parameter is missing")
		}
		text, err := openAPIPrimitiveText(value)
		if err != nil || len(text) > 256 || text == "." || text == ".." || strings.ContainsAny(text, "/\\\x00\r\n") {
			return "", errors.New("OpenAPI path parameter is invalid")
		}
		segments[index] = url.PathEscape(text)
	}
	path := strings.Join(segments, "/")
	if len(path) > 2048 || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", errors.New("OpenAPI path is invalid")
	}
	return path, nil
}

func openAPIRequestQuery(raw any) (url.Values, error) {
	query := url.Values{}
	if raw == nil {
		return query, nil
	}
	parameters, ok := raw.(map[string]any)
	if !ok || len(parameters) > 24 {
		return nil, errors.New("OpenAPI query parameters are invalid")
	}
	for key, value := range parameters {
		text, err := openAPIPrimitiveText(value)
		if err != nil || len(text) > 2048 {
			return nil, errors.New("OpenAPI query parameter is invalid")
		}
		query.Set(key, text)
	}
	if len(query.Encode()) > 8192 {
		return nil, errors.New("OpenAPI query is too large")
	}
	return query, nil
}

func openAPIPrimitiveText(value any) (string, error) {
	switch item := value.(type) {
	case string:
		if item == "" {
			return "", errors.New("OpenAPI parameter is empty")
		}
		return item, nil
	case json.Number:
		return item.String(), nil
	case bool:
		return strconv.FormatBool(item), nil
	default:
		return "", errors.New("OpenAPI parameter type is unsupported")
	}
}
