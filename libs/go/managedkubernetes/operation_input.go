package managedkubernetes

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"k8s.io/apimachinery/pkg/util/validation"
)

// MaximumOperationInputBytes bounds the immutable input before decoding or schema validation.
// MaximumOperationInputBytes ограничивает immutable input до decode либо schema validation.
const MaximumOperationInputBytes = 512 * 1024

// ErrInvalidOperationInput deliberately excludes input, object bodies and upstream parser errors.
// ErrInvalidOperationInput намеренно не содержит input, object bodies и upstream parser errors.
var ErrInvalidOperationInput = errors.New("invalid managed Kubernetes operation input")

// OperationInput contains only command data; target, identity, actor and binding are owner-resolved.
// Body preserves the registered full-object, partial-patch, JSON Patch or DeleteOptions variant.
// OperationInput содержит только command data; target, identity, actor и binding разрешает owner.
// Body сохраняет registered full-object, partial-patch, JSON Patch либо DeleteOptions variant.
type OperationInput struct {
	Group           string          `json:"group,omitempty"`
	Version         string          `json:"version,omitempty"`
	Resource        string          `json:"resource,omitempty"`
	Namespace       string          `json:"namespace,omitempty"`
	Name            string          `json:"name,omitempty"`
	UID             string          `json:"uid,omitempty"`
	ResourceVersion string          `json:"resourceVersion,omitempty"`
	Subresource     string          `json:"subresource,omitempty"`
	Selector        string          `json:"selector,omitempty"`
	Body            json.RawMessage `json:"body,omitempty"`
	Options         json.RawMessage `json:"options,omitempty"`
}

// operationInputSpec closes top-level fields and options independently for each registered command.
// It grants no permission; discovery, current object preconditions and schema checks remain owner work.
// operationInputSpec закрывает top-level fields и options отдельно для каждой registered command.
// Не выдаёт permission; discovery, current object preconditions и schema checks остаются у owner.
type operationInputSpec struct {
	fields, required, options, requiredOptions []string
	body                                       string
	collection, existing, cluster, all         bool
}

// compiledOperationInput caches only owned schemas, never caller schema or externally fetched refs.
// compiledOperationInput кэширует только owned schemas, не caller schema либо внешние refs.
type compiledOperationInput struct {
	schema *jsonschema.Schema
	raw    []byte
	err    error
}

// operationInputSchemas stores immutable compilation results for the finite known command set.
// operationInputSchemas хранит immutable compilation results конечного known command set.
var operationInputSchemas sync.Map

// OperationInputOperations returns the exact input registry for package/owner drift checks.
// OperationInputOperations возвращает exact input registry для package/owner drift checks.
func OperationInputOperations() []string {
	keys := make([]string, 0, len(operationInputRegistry))
	for key := range operationInputRegistry {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// OperationInputSchema returns a copy of the owned closed schema; unknown commands have no fallback.
// OperationInputSchema возвращает копию owned closed schema; unknown commands не имеют fallback.
func OperationInputSchema(operation string) ([]byte, error) {
	compiled := compiledInputSchema(operation)
	if compiled.err != nil {
		return nil, ErrInvalidOperationInput
	}
	return bytes.Clone(compiled.raw), nil
}

// DecodeOperationInput rejects ambiguity, invalid UTF-8, unknown/case-aliased keys and null scalars
// before producing canonical JSON. It does not resolve a target or infer an authorization grant.
// DecodeOperationInput отклоняет ambiguity, invalid UTF-8, unknown/case-aliased keys и null scalars
// до canonical JSON. Не разрешает target и не выводит authorization grant.
func DecodeOperationInput(operation string, raw []byte) (OperationInput, []byte, error) {
	var input OperationInput
	if len(raw) == 0 || len(raw) > MaximumOperationInputBytes || !utf8.Valid(raw) {
		return input, nil, ErrInvalidOperationInput
	}
	compiled := compiledInputSchema(operation)
	var document map[string]any
	if compiled.err != nil || internalrpcauth.DecodeStrictJSON(raw, &document) != nil ||
		document == nil || compiled.schema.Validate(document) != nil {
		return input, nil, ErrInvalidOperationInput
	}
	canonical, err := json.Marshal(document)
	if err != nil || json.Unmarshal(canonical, &input) != nil || validateOperationSemantics(operation, input) != nil {
		return OperationInput{}, nil, ErrInvalidOperationInput
	}
	return input, canonical, nil
}

// ValidateGrantNamespace separates exact namespaces, explicit cluster operations and the two
// all-namespace collection reads. Neither reserved scope authorizes a fan-out child operation.
// ValidateGrantNamespace разделяет exact namespaces, explicit cluster operations и две
// all-namespace collection reads. Reserved scope не разрешает fan-out child operation.
func ValidateGrantNamespace(operation, namespace string) error {
	spec, ok := operationInputRegistry[operation]
	if !ok {
		return ErrInvalidOperationInput
	}
	switch namespace {
	case "@all-namespaces":
		if !spec.all {
			return ErrInvalidOperationInput
		}
	case "@cluster":
		if spec.all {
			return ErrInvalidOperationInput
		}
	default:
		if spec.all || spec.cluster || len(validation.IsDNS1123Label(namespace)) != 0 {
			return ErrInvalidOperationInput
		}
	}
	return nil
}

// ValidateGrantScope checks caller data against the stored namespace without inventing GVR scope.
// The broker must additionally match actual discovery, protected resources and child grants.
// ValidateGrantScope проверяет caller data по stored namespace, не выдумывая GVR scope.
// Broker дополнительно сверяет actual discovery, protected resources и child grants.
func (input OperationInput) ValidateGrantScope(operation, namespace string) error {
	if ValidateGrantNamespace(operation, namespace) != nil {
		return ErrInvalidOperationInput
	}
	if namespace == "@cluster" || namespace == "@all-namespaces" {
		if input.Namespace != "" {
			return ErrInvalidOperationInput
		}
	} else if input.Namespace != namespace {
		return ErrInvalidOperationInput
	}
	return nil
}

// compiledInputSchema compiles a closed owned document with external schema loading disabled.
// compiledInputSchema компилирует closed owned document с отключённой загрузкой внешних schema.
func compiledInputSchema(operation string) compiledOperationInput {
	if cached, ok := operationInputSchemas.Load(operation); ok {
		return cached.(compiledOperationInput)
	}
	spec, ok := operationInputRegistry[operation]
	if !ok {
		return compiledOperationInput{err: ErrInvalidOperationInput}
	}
	document := operationSchema(spec)
	raw, err := json.Marshal(document)
	var schemaDocument any
	if err == nil {
		err = json.Unmarshal(raw, &schemaDocument)
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err == nil {
		err = compiler.AddResource("urn:kodex:kubernetes:operation-input", schemaDocument)
	}
	var schema *jsonschema.Schema
	if err == nil {
		schema, err = compiler.Compile("urn:kodex:kubernetes:operation-input")
	}
	compiled := compiledOperationInput{schema: schema, raw: raw, err: err}
	actual, _ := operationInputSchemas.LoadOrStore(operation, compiled)
	return actual.(compiledOperationInput)
}

// validateOperationSemantics checks route syntax and immutable body identity after schema admission.
// Full discovered GVR validation and UID/RV injection into partial patches belong to the broker.
// validateOperationSemantics проверяет route syntax и immutable body identity после schema admission.
// Полная discovered GVR validation и UID/RV injection в partial patches принадлежат broker.
func validateOperationSemantics(operation string, input OperationInput) error {
	spec := operationInputRegistry[operation]
	if operation == "kubernetes.cp.upload" || operation == "kubernetes.cp.download" {
		if _, err := DecodeCopyProfile(operation, input); err != nil {
			return ErrInvalidOperationInput
		}
	}
	if validateSecretMaterialInput(operation, input) != nil {
		return ErrInvalidOperationInput
	}
	if slices.Contains(spec.fields, "resource") {
		address := ResourceAddress{Group: input.Group, Version: input.Version, Resource: input.Resource,
			Namespace: input.Namespace, Name: input.Name, UID: input.UID,
			ResourceVersion: input.ResourceVersion, Subresource: input.Subresource,
			Namespaced: input.Namespace != "", AllNamespaces: false}
		if address.Validate() != nil {
			return ErrInvalidOperationInput
		}
	}
	if spec.collection && (input.Name != "" || input.UID != "" || input.ResourceVersion != "") {
		return ErrInvalidOperationInput
	}
	if spec.existing && (input.Name == "" || input.UID == "" || input.ResourceVersion == "") {
		return ErrInvalidOperationInput
	}
	if (input.UID == "") != (input.ResourceVersion == "") {
		return ErrInvalidOperationInput
	}
	if input.Subresource != expectedInputSubresource(operation) {
		return ErrInvalidOperationInput
	}
	if len(input.Body) == 0 {
		return nil
	}
	var body map[string]any
	if spec.body == "full" {
		if json.Unmarshal(input.Body, &body) != nil {
			return ErrInvalidOperationInput
		}
		apiVersion := input.Version
		if input.Group != "" {
			apiVersion = input.Group + "/" + input.Version
		}
		metadata, ok := body["metadata"].(map[string]any)
		if !ok || body["apiVersion"] != apiVersion {
			return ErrInvalidOperationInput
		}
		if !matchingBodyString(metadata, "namespace", input.Namespace, input.Namespace != "") ||
			(operation != "kubernetes.create" && !matchingBodyString(metadata, "name", input.Name, input.Name != "")) ||
			!matchingBodyString(metadata, "uid", input.UID, spec.existing) ||
			!matchingBodyString(metadata, "resourceVersion", input.ResourceVersion, spec.existing) {
			return ErrInvalidOperationInput
		}
		if operation == "kubernetes.create" {
			_, hasUID := metadata["uid"]
			_, hasRV := metadata["resourceVersion"]
			name, named := metadata["name"].(string)
			generated, generating := metadata["generateName"].(string)
			if hasUID || hasRV || (!named && !generating) || (named && !validPathName(name)) || (generating && !validPathName(generated)) {
				return ErrInvalidOperationInput
			}
		}
	}
	if spec.body == "patch" {
		var options struct {
			PatchType string `json:"patchType"`
		}
		_ = json.Unmarshal(input.Options, &options)
		isArray := len(input.Body) != 0 && input.Body[0] == '['
		if (options.PatchType == "JSON") != isArray {
			return ErrInvalidOperationInput
		}
		if isArray && !validJSONPatch(input.Body) {
			return ErrInvalidOperationInput
		}
	}
	if spec.body == "delete" {
		_ = json.Unmarshal(input.Body, &body)
		if preconditions, ok := body["preconditions"].(map[string]any); ok &&
			(!matchingBodyString(preconditions, "uid", input.UID, false) ||
				!matchingBodyString(preconditions, "resourceVersion", input.ResourceVersion, false)) {
			return ErrInvalidOperationInput
		}
	}
	return nil
}

// validJSONPatch accepts RFC6901 pointers and the exact per-operation members after schema checks.
// Identity-changing patches still require broker reconstruction and current-object CAS checks.
// validJSONPatch принимает RFC6901 pointers и exact per-operation members после schema checks.
// Identity-changing patches всё равно требуют broker reconstruction и current-object CAS checks.
func validJSONPatch(raw []byte) bool {
	var items []map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return false
	}
	for _, item := range items {
		var operation, path, from string
		_ = json.Unmarshal(item["op"], &operation)
		_ = json.Unmarshal(item["path"], &path)
		if !validJSONPointer(path) {
			return false
		}
		_, hasFrom := item["from"]
		_, hasValue := item["value"]
		if hasFrom {
			_ = json.Unmarshal(item["from"], &from)
			if !validJSONPointer(from) {
				return false
			}
		}
		switch operation {
		case "add", "replace", "test":
			if hasFrom || !hasValue {
				return false
			}
		case "move", "copy":
			if !hasFrom || hasValue {
				return false
			}
		case "remove":
			if hasFrom || hasValue {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// validJSONPointer rejects malformed escape sequences and whole-document replacement paths.
// validJSONPointer отклоняет malformed escape sequences и whole-document replacement paths.
func validJSONPointer(pointer string) bool {
	if !strings.HasPrefix(pointer, "/") {
		return false
	}
	for index := 0; index < len(pointer); index++ {
		if pointer[index] == '~' {
			index++
			if index >= len(pointer) || (pointer[index] != '0' && pointer[index] != '1') {
				return false
			}
		}
	}
	return true
}

// matchingBodyString rejects explicit null, conflicting identity and missing required witnesses.
// matchingBodyString отклоняет explicit null, conflicting identity и missing required witnesses.
func matchingBodyString(object map[string]any, key, expected string, required bool) bool {
	value, present := object[key]
	if !present {
		return !required
	}
	text, ok := value.(string)
	return ok && text == expected
}

// expectedInputSubresource reserves registered routes rather than accepting arbitrary suffixes.
// expectedInputSubresource резервирует registered routes вместо произвольных suffixes.
func expectedInputSubresource(operation string) string {
	if strings.HasPrefix(operation, "kubernetes.status.") {
		return "status"
	}
	if operation == "kubernetes.scale" {
		return "scale"
	}
	return ""
}

// validateSecretMaterialInput refuses plaintext Secret material before immutable invocation JSON.
// The reference selects no authority: its protected owner and exact destination are re-resolved
// before broker-only material loading. Metadata mutations never receive a Secret value echo.
// validateSecretMaterialInput отклоняет plaintext Secret material до immutable invocation JSON.
// Reference не выбирает authority: protected owner и exact destination разрешаются заново
// до broker-only material loading. Metadata mutations не получают Secret value echo.
func validateSecretMaterialInput(operation string, input OperationInput) error {
	var options struct {
		SecretMaterialRef string `json:"secretMaterialRef"`
	}
	if len(input.Options) > 0 && json.Unmarshal(input.Options, &options) != nil {
		return ErrInvalidOperationInput
	}
	secret := input.Group == "" && input.Version == "v1" && input.Resource == "secrets"
	var object map[string]json.RawMessage
	isObject := len(input.Body) > 0 && input.Body[0] == '{'
	if isObject && json.Unmarshal(input.Body, &object) != nil {
		return ErrInvalidOperationInput
	}
	var kind, apiVersion string
	if isObject {
		_ = json.Unmarshal(object["kind"], &kind)
		_ = json.Unmarshal(object["apiVersion"], &apiVersion)
	}
	if kind == "Secret" && apiVersion == "v1" {
		secret = true
	}
	if options.SecretMaterialRef != "" && (!secret || !strings.HasPrefix(options.SecretMaterialRef, "krs_") || !ValidIdentityReference(options.SecretMaterialRef)) {
		return ErrInvalidOperationInput
	}
	if operation == "kubernetes.patch" || operation == "kubernetes.status.patch" {
		var patchArray []json.RawMessage
		emptyPatch := (isObject && len(object) == 0) || (len(input.Body) > 0 && input.Body[0] == '[' && json.Unmarshal(input.Body, &patchArray) == nil && len(patchArray) == 0)
		if options.SecretMaterialRef != "" && !emptyPatch || options.SecretMaterialRef == "" && emptyPatch {
			return ErrInvalidOperationInput
		}
	}
	if !secret {
		return nil
	}
	if isObject {
		for key := range object {
			if strings.EqualFold(key, "data") || strings.EqualFold(key, "stringData") {
				return ErrInvalidOperationInput
			}
		}
		var secretType string
		_ = json.Unmarshal(object["type"], &secretType)
		if secretType == "kubernetes.io/service-account-token" {
			return ErrInvalidOperationInput
		}
	} else if len(input.Body) > 0 && input.Body[0] == '[' {
		var patch []map[string]json.RawMessage
		if json.Unmarshal(input.Body, &patch) != nil {
			return ErrInvalidOperationInput
		}
		for _, item := range patch {
			for _, key := range []string{"path", "from"} {
				var pointer string
				if raw, ok := item[key]; ok {
					if json.Unmarshal(raw, &pointer) != nil {
						return ErrInvalidOperationInput
					}
					root := strings.SplitN(strings.TrimPrefix(pointer, "/"), "/", 2)[0]
					root = strings.ReplaceAll(strings.ReplaceAll(root, "~1", "/"), "~0", "~")
					if strings.EqualFold(root, "data") || strings.EqualFold(root, "stringData") {
						return ErrInvalidOperationInput
					}
				}
			}
		}
	}
	return nil
}
