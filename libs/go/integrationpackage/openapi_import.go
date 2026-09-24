package integrationpackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// OpenAPIImportChoice является выбором владельца, а не входом MCP-вызова.
// Сервер обязан проверить полномочия владельца перед сохранением результата.
type OpenAPIImportChoice struct {
	OperationID       string `json:"operationId"`
	Risk              string `json:"risk"`
	ApprovalPolicy    string `json:"approvalPolicy"`
	IdempotencyHeader string `json:"idempotencyHeader,omitempty"`
}

type OpenAPIImportOptions struct {
	Version           string                `json:"version"`
	Name              string                `json:"name"`
	Description       string                `json:"description"`
	HealthOperationID string                `json:"healthOperationId"`
	Choices           []OpenAPIImportChoice `json:"choices"`
}

type OpenAPIImportPayload struct {
	Source  string               `json:"source"`
	Options OpenAPIImportOptions `json:"options"`
}

// DraftOpenAPIPackageFromJSON принимает строго ограниченный импортный
// envelope из защищённой формы. Исходный документ не сохраняется в package.
func DraftOpenAPIPackageFromJSON(ctx context.Context, raw []byte) (Package, error) {
	if len(raw) == 0 || len(raw) > 256<<10 {
		return Package{}, errors.New("OpenAPI import payload size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload OpenAPIImportPayload
	if decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF || len(payload.Source) == 0 || len(payload.Source) > 128<<10 {
		return Package{}, errors.New("OpenAPI import payload is invalid")
	}
	return DraftOpenAPIPackage(ctx, []byte(payload.Source), payload.Options)
}

// DraftOpenAPIPackage создаёт только проверенный черновик. Пока shipped
// OPENAPI_MCP имеет readiness NOT_READY, публикация и исполнение закрыты.
func DraftOpenAPIPackage(ctx context.Context, raw []byte, options OpenAPIImportOptions) (Package, error) {
	inspection, err := InspectOpenAPI(ctx, raw)
	if err != nil {
		return Package{}, err
	}
	if len(options.Choices) == 0 || len(options.Choices) > 48 || options.HealthOperationID == "" {
		return Package{}, errors.New("OpenAPI import operation selection is invalid")
	}
	loader := openapi3.NewLoader()
	loader.Context = ctx
	loader.IsExternalRefsAllowed = false
	document, err := loader.LoadFromData(raw)
	if err != nil || document == nil {
		return Package{}, errors.New("OpenAPI import document is invalid")
	}
	shipped, err := LoadShipped()
	if err != nil {
		return Package{}, err
	}
	definition, exists := shipped["openapi-mcp"]
	if !exists {
		return Package{}, errors.New("OpenAPI adapter template is unavailable")
	}
	definition.Metadata.Origin = OriginUI
	definition.Metadata.Version = options.Version
	definition.Spec.Name = strings.TrimSpace(options.Name)
	if definition.Spec.Name == "" {
		definition.Spec.Name = inspection.Title
	}
	definition.Spec.Description = strings.TrimSpace(options.Description)
	if definition.Spec.Description == "" {
		definition.Spec.Description = "Интеграция из проверенного OpenAPI-контракта."
	}
	definition.Spec.Capabilities = nil
	byID := make(map[string]OpenAPIOperation, len(inspection.Operations))
	for _, operation := range inspection.Operations {
		byID[operation.ID] = operation
	}
	chosen := make(map[string]struct{}, len(options.Choices))
	authScheme, authHeader, origin := "", "", ""
	healthFound := false
	for _, choice := range options.Choices {
		entry, ok := byID[choice.OperationID]
		if !ok || !entry.Candidate || choice.OperationID == "" {
			return Package{}, errors.New("OpenAPI import operation is unsupported")
		}
		if _, duplicate := chosen[choice.OperationID]; duplicate {
			return Package{}, errors.New("OpenAPI import operation is duplicated")
		}
		chosen[choice.OperationID] = struct{}{}
		path := document.Paths.Value(entry.Path)
		if path == nil || path.GetOperation(entry.Method) == nil {
			return Package{}, errors.New("OpenAPI import operation is unavailable")
		}
		operation := path.GetOperation(entry.Method)
		currentAuth, currentHeader, err := openAPIImportSecurity(document, operation)
		if err != nil || authScheme != "" && (currentAuth != authScheme || currentHeader != authHeader) {
			return Package{}, errors.New("OpenAPI import security scheme is unsupported")
		}
		authScheme, authHeader = currentAuth, currentHeader
		if origin != "" && origin != entry.ServerOrigin {
			return Package{}, errors.New("OpenAPI import mixes server origins")
		}
		origin = entry.ServerOrigin
		inputSchema, err := openAPIImportInput(path, operation)
		if err != nil {
			return Package{}, err
		}
		if _, _, err := validateOpenAPIInputSchema(inputSchema); err != nil {
			return Package{}, err
		}
		methodRead := entry.Method == http.MethodGet
		if methodRead && (choice.Risk != string(RiskRead) || choice.ApprovalPolicy != string(ApprovalNone) || choice.IdempotencyHeader != "") ||
			!methodRead && (choice.Risk != string(RiskWrite) && choice.Risk != string(RiskSensitive) && choice.Risk != string(RiskDestructive) ||
				choice.ApprovalPolicy != string(ApprovalHumanEachEffect) && choice.ApprovalPolicy != string(ApprovalHumanScoped)) {
			return Package{}, errors.New("OpenAPI import risk or approval policy is invalid")
		}
		operationHash := sha256.Sum256([]byte(choice.OperationID))
		key := "op." + hex.EncodeToString(operationHash[:8])
		capability := Capability{
			Key: key, Name: strings.TrimSpace(entry.Summary), Description: strings.TrimSpace(operation.Description),
			Operation: "openapi." + key, Risk: choice.Risk, ApprovalPolicy: choice.ApprovalPolicy,
			ResourceScope: ResourceScope{Kind: "HTTPS_RESOURCE", ConnectionFields: []string{"base_url"}},
			OutputFields:  []Field{{Key: "body_json", Type: "STRING", Format: "PLAIN", Required: true, MaximumLength: 32768}},
			Execution:     Execution{TimeoutSeconds: 20, MaxAttempts: 1, RetryBackoffMilliseconds: 250},
			OpenAPI: &OpenAPIHTTP{OperationID: choice.OperationID, SourceDigest: inspection.Digest,
				ServerOrigin: entry.ServerOrigin, Method: entry.Method, Path: entry.Path,
				AuthScheme: currentAuth, AuthHeader: currentHeader, IdempotencyHeader: choice.IdempotencyHeader,
				InputSchema: inputSchema},
		}
		if capability.Name == "" {
			capability.Name = choice.OperationID
		}
		if capability.Description == "" {
			capability.Description = "Операция OpenAPI " + choice.OperationID
		}
		if methodRead {
			capability.Execution.Idempotency = string(IdempotencyReadOnly)
			capability.Execution.MaxAttempts = 2
		} else if choice.IdempotencyHeader != "" {
			capability.Execution.Idempotency = string(IdempotencyEffectKey)
		} else {
			capability.Execution.Idempotency = string(IdempotencyOneAttempt)
		}
		if choice.OperationID == options.HealthOperationID {
			if !methodRead {
				return Package{}, errors.New("OpenAPI health operation must be a read")
			}
			if _, err := capability.ValidateInput([]byte("{}")); err != nil {
				return Package{}, errors.New("OpenAPI health operation requires input")
			}
			definition.Spec.HealthCheck.Operation = capability.Operation
			healthFound = true
		}
		definition.Spec.Capabilities = append(definition.Spec.Capabilities, capability)
	}
	if !healthFound || origin == "" {
		return Package{}, errors.New("OpenAPI health operation or server origin is missing")
	}
	if authScheme == "NONE" {
		definition.Spec.Credential = nil
	} else {
		definition.Spec.Credential = &Credential{SecretKey: "token", Kind: "TOKEN"}
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return Package{}, errors.New("OpenAPI import serialization failed")
	}
	result, err := Parse(encoded)
	if err != nil {
		return Package{}, err
	}
	if err := ValidateExecutableRevision(result, shipped["openapi-mcp"]); err != nil {
		return Package{}, err
	}
	return result, nil
}

func openAPIImportSecurity(document *openapi3.T, operation *openapi3.Operation) (string, string, error) {
	requirements := document.Security
	if operation.Security != nil {
		requirements = *operation.Security
	}
	if len(requirements) == 0 || len(requirements) == 1 && len(requirements[0]) == 0 {
		return "NONE", "", nil
	}
	if len(requirements) != 1 || len(requirements[0]) != 1 || document.Components == nil {
		return "", "", errors.New("OpenAPI security requirements are unsupported")
	}
	for name, scopes := range requirements[0] {
		ref := document.Components.SecuritySchemes[name]
		if len(scopes) != 0 || ref == nil || ref.Value == nil {
			return "", "", errors.New("OpenAPI security scheme is unavailable")
		}
		scheme := ref.Value
		if scheme.Type == "http" && strings.EqualFold(scheme.Scheme, "bearer") {
			return "BEARER", "", nil
		}
		if scheme.Type == "apiKey" && scheme.In == "header" && ValidOpenAPIOutboundHeader(scheme.Name) {
			return "API_KEY_HEADER", scheme.Name, nil
		}
	}
	return "", "", errors.New("OpenAPI security scheme is unsupported")
}

func openAPIImportInput(path *openapi3.PathItem, operation *openapi3.Operation) (map[string]any, error) {
	properties := map[string]any{}
	requiredRoot := []any{}
	fields := map[string]map[string]any{"path": {}, "query": {}}
	required := map[string][]any{"path": {}, "query": {}}
	seen := map[string]struct{}{}
	for _, ref := range append(append(openapi3.Parameters(nil), path.Parameters...), operation.Parameters...) {
		if ref == nil || ref.Value == nil || ref.Value.Schema == nil || ref.Value.Schema.Value == nil {
			return nil, errors.New("OpenAPI import parameter is invalid")
		}
		parameter := ref.Value
		if parameter.In != "path" && parameter.In != "query" || !openAPIParameterName.MatchString(parameter.Name) ||
			parameter.AllowReserved || parameter.AllowEmptyValue || parameter.Explode != nil ||
			parameter.Style != "" && (parameter.In == "path" && parameter.Style != "simple" || parameter.In == "query" && parameter.Style != "form") {
			return nil, errors.New("OpenAPI import parameter serialization is unsupported")
		}
		identity := parameter.In + "\x00" + parameter.Name
		if _, duplicate := seen[identity]; duplicate {
			return nil, errors.New("OpenAPI import parameter is duplicated")
		}
		seen[identity] = struct{}{}
		schema, err := openAPIImportSchema(ref.Value.Schema, 0, map[*openapi3.Schema]bool{})
		if err != nil || !openAPIPrimitiveParameter(schema) {
			return nil, errors.New("OpenAPI import parameter schema is unsupported")
		}
		fields[parameter.In][parameter.Name] = schema
		if parameter.Required {
			required[parameter.In] = append(required[parameter.In], parameter.Name)
		}
	}
	for _, location := range []string{"path", "query"} {
		if len(fields[location]) == 0 {
			continue
		}
		schema := map[string]any{"type": "object", "additionalProperties": false, "properties": fields[location]}
		if len(required[location]) != 0 {
			schema["required"] = required[location]
			// Если есть обязательный параметр, сам container обязателен.
			requiredRoot = append(requiredRoot, location)
		}
		properties[location] = schema
	}
	if operation.RequestBody != nil {
		body := operation.RequestBody.Value
		if body == nil || len(body.Content) != 1 || body.Content["application/json"] == nil ||
			body.Content["application/json"].Schema == nil {
			return nil, errors.New("OpenAPI import request body is unsupported")
		}
		schema, err := openAPIImportSchema(body.Content["application/json"].Schema, 0, map[*openapi3.Schema]bool{})
		if err != nil || schema["type"] != "object" {
			return nil, errors.New("OpenAPI import request body schema is unsupported")
		}
		properties["body"] = schema
		if body.Required {
			requiredRoot = append(requiredRoot, "body")
		}
	}
	result := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if len(requiredRoot) != 0 {
		result["required"] = requiredRoot
	}
	return result, nil
}

func openAPIImportSchema(ref *openapi3.SchemaRef, depth int, active map[*openapi3.Schema]bool) (map[string]any, error) {
	if depth > 12 || ref == nil || ref.Value == nil || active[ref.Value] {
		return nil, errors.New("OpenAPI import schema is recursive or incomplete")
	}
	schema := ref.Value
	active[schema] = true
	defer delete(active, schema)
	if schema.Always != nil || len(schema.Extensions) != 0 || schema.Type == nil || !schema.Type.IsSingle() ||
		schema.Nullable || schema.Format != "" || len(schema.OneOf) != 0 || len(schema.AnyOf) != 0 ||
		len(schema.AllOf) != 0 || schema.Not != nil || schema.Discriminator != nil ||
		len(schema.PatternProperties) != 0 || len(schema.DependentSchemas) != 0 || schema.PropertyNames != nil ||
		schema.If != nil || schema.Then != nil || schema.Else != nil || len(schema.PrefixItems) != 0 ||
		schema.Contains != nil || schema.DynamicRef != "" || schema.SchemaDialect != "" ||
		schema.SchemaID != "" || schema.Anchor != "" || schema.DynamicAnchor != "" ||
		len(schema.Defs) != 0 || schema.ContentMediaType != "" || schema.ContentEncoding != "" || schema.ContentSchema != nil {
		return nil, errors.New("OpenAPI import schema feature is unsupported")
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, errors.New("OpenAPI import schema serialization failed")
	}
	var result map[string]any
	if json.Unmarshal(encoded, &result) != nil {
		return nil, errors.New("OpenAPI import schema serialization failed")
	}
	for key := range result {
		switch key {
		case "type", "properties", "required", "additionalProperties", "items", "minLength", "maxLength", "pattern",
			"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf", "enum", "const",
			"minItems", "maxItems", "uniqueItems", "minProperties", "maxProperties":
		case "title", "description", "example", "examples", "default", "deprecated":
			delete(result, key)
		default:
			return nil, errors.New("OpenAPI import schema keyword is unsupported")
		}
	}
	switch (*schema.Type)[0] {
	case "object":
		if schema.AdditionalProperties.Has == nil || *schema.AdditionalProperties.Has || schema.AdditionalProperties.Schema != nil || len(schema.Properties) > 64 {
			return nil, errors.New("OpenAPI import object schema is not closed")
		}
		properties := make(map[string]any, len(schema.Properties))
		for name, child := range schema.Properties {
			if !openAPIParameterName.MatchString(name) {
				return nil, errors.New("OpenAPI import object property is invalid")
			}
			value, err := openAPIImportSchema(child, depth+1, active)
			if err != nil {
				return nil, err
			}
			properties[name] = value
		}
		result["properties"] = properties
		result["additionalProperties"] = false
	case "array":
		if schema.Items == nil || schema.MaxItems == nil || *schema.MaxItems > 128 {
			return nil, errors.New("OpenAPI import array schema is unbounded")
		}
		item, err := openAPIImportSchema(schema.Items, depth+1, active)
		if err != nil {
			return nil, err
		}
		result["items"] = item
	case "string", "integer", "number", "boolean":
	default:
		return nil, errors.New("OpenAPI import schema type is unsupported")
	}
	return result, nil
}
