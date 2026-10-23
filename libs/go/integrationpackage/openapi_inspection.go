package integrationpackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"go.yaml.in/yaml/v3"
)

const (
	// maxOpenAPIBytes caps the untrusted source before YAML parsing or OpenAPI loading.
	// maxOpenAPIBytes ограничивает недоверенный источник до разбора YAML и загрузки OpenAPI.
	maxOpenAPIBytes = 256 << 10
	// maxOpenAPINodes bounds the total syntax tree traversed before model validation.
	// maxOpenAPINodes ограничивает общее число узлов синтаксического дерева до проверки модели.
	maxOpenAPINodes = 20000
	// maxOpenAPIDepth prevents deeply nested input from exhausting parser resources.
	// maxOpenAPIDepth не даёт глубоко вложенному вводу исчерпать ресурсы разбора.
	maxOpenAPIDepth = 64
	// maxOpenAPIOperations limits the number of candidate operations exposed for review.
	// maxOpenAPIOperations ограничивает число операций-кандидатов, предъявляемых на проверку.
	maxOpenAPIOperations = 128
)

// openAPIOperationID accepts only bounded, stable identifiers suitable for capability keys.
// openAPIOperationID допускает только ограниченные устойчивые идентификаторы для ключей возможностей.
var openAPIOperationID = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)

// OpenAPIInspection is a local inspection result, not publication or permission to invoke a remote operation.
// OpenAPIInspection — результат локальной проверки, а не публикация или право вызвать удалённую операцию.
// OpenAPIInspection описывает импорт, но сама по себе не даёт права на вызов.
// Публикация definition и сетевой admission выполняются отдельно владельцем.
type OpenAPIInspection struct {
	Digest     string
	Title      string
	Version    string
	Operations []OpenAPIOperation
}

// OpenAPIOperation records one visible operation and its closed candidate decision for later admission.
// OpenAPIOperation хранит видимую операцию и закрытое решение о передаче на последующий допуск.
type OpenAPIOperation struct {
	ID, Method, Path, Summary, ServerOrigin string
	// Candidate означает только, что operation можно передать на следующий
	// admission; это не publication, grant или право на HTTP-вызов.
	Candidate       bool
	HealthCandidate bool
	Reason          string
}

// InspectOpenAPI validates a bounded local OpenAPI document without external refs or network access;
// unsupported operations remain visible but cannot become admission candidates.
// InspectOpenAPI читает только локальный документ без внешних $ref и сети.
// Неподдерживаемая операция остаётся видимой с закрытым исходом.
func InspectOpenAPI(ctx context.Context, raw []byte) (OpenAPIInspection, error) {
	if len(raw) == 0 || len(raw) > maxOpenAPIBytes {
		return OpenAPIInspection{}, errors.New("OpenAPI document size is invalid")
	}
	if err := inspectOpenAPIYAML(raw); err != nil {
		return OpenAPIInspection{}, err
	}
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	loader.Context = ctx
	document, err := loader.LoadFromData(raw)
	if err != nil || document == nil || document.Info == nil || document.Paths == nil ||
		!(document.IsOpenAPI30() || document.IsOpenAPI31OrLater()) {
		return OpenAPIInspection{}, errors.New("OpenAPI document is invalid")
	}
	if err := document.Validate(ctx); err != nil {
		return OpenAPIInspection{}, errors.New("OpenAPI document validation failed")
	}
	paths := document.Paths.Map()
	pathKeys := make([]string, 0, len(paths))
	for path := range paths {
		pathKeys = append(pathKeys, path)
	}
	sort.Strings(pathKeys)
	operations := make([]OpenAPIOperation, 0)
	seenIDs := map[string]struct{}{}
	for _, path := range pathKeys {
		item := paths[path]
		if item == nil {
			return OpenAPIInspection{}, errors.New("OpenAPI path is invalid")
		}
		methods := make([]string, 0, len(item.Operations()))
		for method := range item.Operations() {
			methods = append(methods, method)
		}
		sort.Strings(methods)
		for _, method := range methods {
			operation := item.GetOperation(method)
			if operation == nil {
				return OpenAPIInspection{}, errors.New("OpenAPI operation is invalid")
			}
			if len(operations) >= maxOpenAPIOperations {
				return OpenAPIInspection{}, errors.New("OpenAPI operation limit exceeded")
			}
			entry := OpenAPIOperation{ID: operation.OperationID, Method: strings.ToUpper(method), Path: path, Summary: operation.Summary, Candidate: true}
			entry.ServerOrigin, entry.Reason = openAPIOperationServerOrigin(document.Servers, item.Servers, operation.Servers)
			switch {
			case entry.Reason != "":
				entry.Candidate = false
			case operation.OperationID == "":
				entry.Candidate, entry.Reason = false, "OPERATION_ID_REQUIRED"
			case !openAPIOperationID.MatchString(operation.OperationID):
				entry.Candidate, entry.Reason = false, "OPERATION_ID_UNSUPPORTED"
			case !supportedOpenAPIMethod(entry.Method):
				entry.Candidate, entry.Reason = false, "HTTP_METHOD_UNSUPPORTED"
			case !validOpenAPIPathTemplate(path):
				entry.Candidate, entry.Reason = false, "PATH_UNSUPPORTED"
			default:
				if reason := unsupportedOpenAPIOperation(item, operation, entry.Method); reason != "" {
					entry.Candidate, entry.Reason = false, reason
				}
			}
			var input map[string]any
			if entry.Candidate {
				if _, _, err := openAPIImportSecurity(document, operation); err != nil {
					entry.Candidate, entry.Reason = false, "SECURITY_SCHEME_UNSUPPORTED"
				} else if imported, err := openAPIImportInput(item, operation); err != nil {
					entry.Candidate, entry.Reason = false, "INPUT_SCHEMA_UNSUPPORTED"
				} else if _, _, err := validateOpenAPIInputSchema(imported); err != nil {
					entry.Candidate, entry.Reason = false, "INPUT_SCHEMA_UNSUPPORTED"
				} else if err := validateOpenAPIHTTPInput(&OpenAPIHTTP{Method: entry.Method, Path: path, InputSchema: imported}); err != nil {
					entry.Candidate, entry.Reason = false, "INPUT_SCHEMA_UNSUPPORTED"
				} else {
					input = imported
				}
			}
			if operation.OperationID != "" {
				if _, exists := seenIDs[operation.OperationID]; exists {
					return OpenAPIInspection{}, errors.New("OpenAPI operationId is duplicated")
				}
				seenIDs[operation.OperationID] = struct{}{}
			}
			if entry.Candidate && entry.Method == http.MethodGet {
				_, inputErr := (Capability{OpenAPI: &OpenAPIHTTP{InputSchema: input}}).ValidateInput([]byte("{}"))
				entry.HealthCandidate = inputErr == nil
			}
			operations = append(operations, entry)
		}
	}
	if len(operations) == 0 {
		return OpenAPIInspection{}, errors.New("OpenAPI document has no operations")
	}
	digest := sha256.Sum256(raw)
	return OpenAPIInspection{Digest: hex.EncodeToString(digest[:]), Title: document.Info.Title,
		Version: document.Info.Version, Operations: operations}, nil
}

// openAPIOperationServerOrigin applies OpenAPI server precedence and accepts one literal HTTPS origin.
// openAPIOperationServerOrigin применяет приоритет серверов OpenAPI и допускает один буквальный HTTPS-origin.
func openAPIOperationServerOrigin(documentServers, pathServers openapi3.Servers, operationServers *openapi3.Servers) (string, string) {
	servers := documentServers
	if len(pathServers) != 0 {
		servers = pathServers
	}
	if operationServers != nil {
		servers = *operationServers
	}
	if len(servers) != 1 || servers[0] == nil || len(servers[0].Variables) != 0 ||
		validateStringValue(Field{Type: "STRING", Format: "HTTPS_ORIGIN", MaximumLength: 2048}, servers[0].URL, false) != nil {
		return "", "SERVER_ORIGIN_UNSUPPORTED"
	}
	return servers[0].URL, ""
}

// unsupportedOpenAPIOperation returns a closed reason for callbacks, non-JSON bodies or responses,
// and parameters outside the typed scalar path/query contract.
// unsupportedOpenAPIOperation возвращает причину закрытого отказа для callback,
// не-JSON тел и ответов, а также параметров вне типизированного скалярного path/query-контракта.
func unsupportedOpenAPIOperation(path *openapi3.PathItem, operation *openapi3.Operation, method string) string {
	if len(operation.Callbacks) != 0 {
		return "CALLBACKS_UNSUPPORTED"
	}
	if operation.Responses == nil {
		return "RESPONSES_UNSUPPORTED"
	}
	successResponse := false
	for status, response := range operation.Responses.Map() {
		if len(status) != 3 || status[0] != '2' || status[1] < '0' || status[1] > '9' || status[2] < '0' || status[2] > '9' {
			continue
		}
		successResponse = true
		if response == nil || response.Value == nil || len(response.Value.Content) > 1 ||
			len(response.Value.Content) == 1 && response.Value.Content["application/json"] == nil {
			return "RESPONSE_MEDIA_UNSUPPORTED"
		}
	}
	if !successResponse {
		return "RESPONSES_UNSUPPORTED"
	}
	if operation.RequestBody != nil {
		if method == http.MethodGet || operation.RequestBody.Value == nil ||
			operation.RequestBody.Value.Content["application/json"] == nil {
			return "REQUEST_BODY_UNSUPPORTED"
		}
	}
	for _, parameter := range append(append(openapi3.Parameters(nil), path.Parameters...), operation.Parameters...) {
		if parameter == nil || parameter.Value == nil || parameter.Value.Schema == nil || parameter.Value.Schema.Value == nil ||
			len(parameter.Value.Content) != 0 || parameter.Value.AllowReserved {
			return "PARAMETER_UNSUPPORTED"
		}
		value := parameter.Value
		if value.In != openapi3.ParameterInPath && value.In != openapi3.ParameterInQuery ||
			!openAPIParameterName.MatchString(value.Name) || value.Style != "" &&
			(value.In == openapi3.ParameterInPath && value.Style != "simple" ||
				value.In == openapi3.ParameterInQuery && value.Style != "form") ||
			value.In == openapi3.ParameterInPath && !value.Required ||
			value.Schema.Value.Type == nil || !value.Schema.Value.Type.IsSingle() {
			return "PARAMETER_UNSUPPORTED"
		}
		schemaType := (*value.Schema.Value.Type)[0]
		if !approvalScopeType(schemaType) || schemaType == "object" || schemaType == "array" {
			return "PARAMETER_UNSUPPORTED"
		}
	}
	return ""
}

// supportedOpenAPIMethod admits only the closed HTTP method set implemented by the adapter.
// supportedOpenAPIMethod допускает только закрытый набор HTTP-методов, реализованный адаптером.
func supportedOpenAPIMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// validOpenAPIPathTemplate rejects ambiguous paths and permits only bounded literal segments or
// named placeholders that the managed adapter can bind without changing the destination.
// validOpenAPIPathTemplate отклоняет неоднозначные пути и допускает лишь ограниченные сегменты
// либо именованные шаблоны, которые адаптер связывает без смены назначения.
func validOpenAPIPathTemplate(path string) bool {
	if len(path) == 0 || len(path) > 512 || path[0] != '/' || strings.HasPrefix(path, "//") || strings.Contains(path, "//") {
		return false
	}
	for _, segment := range strings.Split(path[1:], "/") {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			if !openAPIOperationID.MatchString(segment[1 : len(segment)-1]) {
				return false
			}
			continue
		}
		if segment != "" && !ValidHTTPSResourcePath("/"+segment) || segment == "." || segment == ".." || strings.ContainsAny(segment, "{}") {
			return false
		}
	}
	return true
}

// inspectOpenAPIYAML rejects multiple documents, duplicate keys, aliases and excessive complexity
// before the OpenAPI loader may interpret any part of the untrusted specification.
// inspectOpenAPIYAML отклоняет несколько документов, повторные ключи, alias и чрезмерную сложность
// до того, как загрузчик OpenAPI интерпретирует недоверенную спецификацию.
func inspectOpenAPIYAML(raw []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var document yaml.Node
	if decoder.Decode(&document) != nil || document.Kind != yaml.DocumentNode {
		return errors.New("OpenAPI syntax is invalid")
	}
	var trailing yaml.Node
	if decoder.Decode(&trailing) != io.EOF {
		return errors.New("OpenAPI document must contain one document")
	}
	// nodeDepth carries one YAML node and its explicit traversal depth for bounded inspection.
	// nodeDepth хранит узел YAML и его явную глубину для ограниченного обхода.
	type nodeDepth struct {
		node  *yaml.Node
		depth int
	}
	stack := []nodeDepth{{node: &document, depth: 0}}
	count := 0
	for len(stack) != 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		count++
		if count > maxOpenAPINodes || current.depth > maxOpenAPIDepth {
			return errors.New("OpenAPI document complexity limit exceeded")
		}
		if current.node.Kind == yaml.AliasNode || current.node.Anchor != "" || current.node.Alias != nil ||
			current.node.Kind == yaml.ScalarNode && current.node.Value == "<<" {
			return errors.New("OpenAPI YAML alias or merge key is forbidden")
		}
		if current.node.Kind == yaml.MappingNode {
			keys := map[string]struct{}{}
			for index := 0; index < len(current.node.Content); index += 2 {
				key := current.node.Content[index]
				if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
					return errors.New("OpenAPI mapping key is invalid")
				}
				if _, exists := keys[key.Value]; exists {
					return errors.New("OpenAPI mapping key is duplicated")
				}
				keys[key.Value] = struct{}{}
			}
		}
		for _, child := range current.node.Content {
			stack = append(stack, nodeDepth{node: child, depth: current.depth + 1})
		}
	}
	return nil
}
