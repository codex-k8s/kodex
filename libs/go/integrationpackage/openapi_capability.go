package integrationpackage

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
)

var openAPIHeaderName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)
var openAPIParameterName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

func validateOpenAPICapability(definition *Package, capability Capability) error {
	binding := capability.OpenAPI
	if binding == nil || len(capability.InputFields) != 0 || capability.ResourceScope.Kind != "HTTPS_RESOURCE" ||
		len(capability.ResourceScope.ConnectionFields) != 1 || capability.ResourceScope.ConnectionFields[0] != "base_url" ||
		!openAPIOperationID.MatchString(binding.OperationID) || !supportedOpenAPIMethod(binding.Method) ||
		!validOpenAPIPathTemplate(binding.Path) || !strings.HasPrefix(capability.Operation, "openapi.") ||
		len(binding.InputSchema) == 0 {
		return errors.New("OpenAPI capability binding is invalid")
	}
	if _, _, err := validateOpenAPIInputSchema(binding.InputSchema); err != nil {
		return err
	}
	if err := validateOpenAPIHTTPInput(binding); err != nil {
		return err
	}
	if len(capability.OutputFields) != 1 || capability.OutputFields[0].Key != "body_json" ||
		capability.OutputFields[0].Type != "STRING" || capability.OutputFields[0].Format != "PLAIN" ||
		!capability.OutputFields[0].Required || capability.OutputFields[0].MaximumLength < 16384 {
		return errors.New("OpenAPI output projection is invalid")
	}
	if binding.Method == http.MethodGet {
		if capability.Risk != string(RiskRead) || capability.Execution.Idempotency != string(IdempotencyReadOnly) ||
			capability.ApprovalPolicy != string(ApprovalNone) || binding.IdempotencyHeader != "" {
			return errors.New("OpenAPI read capability policy is invalid")
		}
	} else if capability.Risk == string(RiskRead) ||
		capability.ApprovalPolicy != string(ApprovalHumanEachEffect) && capability.ApprovalPolicy != string(ApprovalHumanScoped) ||
		capability.Execution.MaxAttempts != 1 || binding.IdempotencyHeader == "" && capability.Execution.Idempotency != string(IdempotencyOneAttempt) ||
		binding.IdempotencyHeader != "" && capability.Execution.Idempotency != string(IdempotencyEffectKey) {
		return errors.New("OpenAPI write capability policy is invalid")
	}
	if binding.IdempotencyHeader != "" && (!ValidOpenAPIOutboundHeader(binding.IdempotencyHeader) &&
		!strings.EqualFold(binding.IdempotencyHeader, "Idempotency-Key") ||
		strings.EqualFold(binding.IdempotencyHeader, binding.AuthHeader)) {
		return errors.New("OpenAPI idempotency header is invalid")
	}
	switch binding.AuthScheme {
	case "NONE":
		if binding.AuthHeader != "" {
			return errors.New("OpenAPI authentication header is invalid")
		}
	case "BEARER":
		if binding.AuthHeader != "" || definition.Spec.Credential == nil {
			return errors.New("OpenAPI bearer credential is invalid")
		}
	case "API_KEY_HEADER":
		if !ValidOpenAPIOutboundHeader(binding.AuthHeader) || definition.Spec.Credential == nil {
			return errors.New("OpenAPI API key header is invalid")
		}
	default:
		return errors.New("OpenAPI authentication scheme is unsupported")
	}
	var baseURLFound, destinationFound bool
	for _, field := range definition.Spec.ConfigurationFields {
		if field.Key == "base_url" {
			baseURLFound = field.Type == "STRING" && field.Format == "HTTPS_ORIGIN" && field.Required
		}
	}
	for _, destination := range definition.Spec.NetworkDestinations {
		if destination.Source == "CONFIGURATION" && destination.ConfigurationField == "base_url" && destination.Port == 443 && destination.TLS == "REQUIRED" {
			destinationFound = true
		}
	}
	if !baseURLFound || !destinationFound {
		return errors.New("OpenAPI network binding is invalid")
	}
	return nil
}

// ValidOpenAPIOutboundHeader исключает заголовки, влияющие на routing,
// transport, авторизацию прокси и формат тела. Остальные имена закрепляет
// опубликованное определение, а не input инструмента.
func ValidOpenAPIOutboundHeader(name string) bool {
	if !openAPIHeaderName.MatchString(name) {
		return false
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "proxy-") || strings.HasPrefix(lower, "sec-") || strings.HasPrefix(lower, "x-forwarded-") {
		return false
	}
	switch lower {
	case "authorization", "host", "cookie", "accept", "content-type", "content-length", "connection",
		"transfer-encoding", "te", "upgrade", "forwarded", "idempotency-key":
		return false
	default:
		return true
	}
}

func validateOpenAPIHTTPInput(binding *OpenAPIHTTP) error {
	properties, ok := binding.InputSchema["properties"].(map[string]any)
	if !ok {
		return errors.New("OpenAPI input parameter schema is invalid")
	}
	for key := range properties {
		if key != "path" && key != "query" && key != "body" {
			return errors.New("OpenAPI input location is unsupported")
		}
	}
	if binding.Method == http.MethodGet {
		if _, hasBody := properties["body"]; hasBody {
			return errors.New("OpenAPI GET body is unsupported")
		}
	}
	pathFields := map[string]any{}
	if rawPath, present := properties["path"]; present {
		rootRequired, valid := openAPIRequiredKeys(binding.InputSchema["required"])
		if !valid || !rootRequired["path"] {
			return errors.New("OpenAPI path parameter container must be required")
		}
		path, ok := rawPath.(map[string]any)
		if !ok || path["type"] != "object" || path["additionalProperties"] != false {
			return errors.New("OpenAPI path parameter schema is invalid")
		}
		pathFields, ok = path["properties"].(map[string]any)
		if !ok {
			return errors.New("OpenAPI path parameter schema is invalid")
		}
		required, valid := openAPIRequiredKeys(path["required"])
		if !valid || len(pathFields) != len(required) {
			return errors.New("OpenAPI path parameters must be required")
		}
		for key := range pathFields {
			if !required[key] {
				return errors.New("OpenAPI path parameter is optional")
			}
		}
	}
	placeholders := map[string]struct{}{}
	for _, segment := range strings.Split(binding.Path, "/") {
		if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
			continue
		}
		key := segment[1 : len(segment)-1]
		if _, duplicate := placeholders[key]; duplicate {
			return errors.New("OpenAPI path parameter is duplicated")
		}
		placeholders[key] = struct{}{}
	}
	if len(placeholders) != len(pathFields) {
		return errors.New("OpenAPI path parameters do not match template")
	}
	for key, raw := range pathFields {
		if _, present := placeholders[key]; !present || !openAPIPrimitiveParameter(raw) {
			return errors.New("OpenAPI path parameter is unsupported")
		}
	}
	if rawQuery, present := properties["query"]; present {
		query, ok := rawQuery.(map[string]any)
		if !ok || query["type"] != "object" || query["additionalProperties"] != false {
			return errors.New("OpenAPI query parameter schema is invalid")
		}
		fields, ok := query["properties"].(map[string]any)
		if !ok || len(fields) > 24 {
			return errors.New("OpenAPI query parameter schema is invalid")
		}
		for key, raw := range fields {
			if !openAPIParameterName.MatchString(key) || !openAPIPrimitiveParameter(raw) {
				return errors.New("OpenAPI query parameter is unsupported")
			}
		}
	}
	return nil
}

func openAPIPrimitiveParameter(raw any) bool {
	field, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	switch field["type"] {
	case "string", "integer", "number", "boolean":
		return true
	default:
		return false
	}
}

func openAPIRequiredKeys(raw any) (map[string]bool, bool) {
	items, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	result := make(map[string]bool, len(items))
	for _, item := range items {
		key, ok := item.(string)
		if !ok || result[key] {
			return nil, false
		}
		result[key] = true
	}
	return result, true
}
