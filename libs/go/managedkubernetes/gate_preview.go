package managedkubernetes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// GatePreviewListBytes and GatePreviewDetailBytes bound projections, not executable input.
// GatePreviewListBytes и GatePreviewDetailBytes ограничивают projection, не executable input.
const (
	GatePreviewListBytes   = 16 * 1024
	GatePreviewDetailBytes = 512 * 1024
	// GatePortForwardLogicalStreamDisclosure describes the server-fixed bounded logical stream, not a caller option or a new per-connection approval.
	// GatePortForwardLogicalStreamDisclosure описывает server-fixed bounded logical stream, не caller option либо новый per-connection approval.
	GatePortForwardLogicalStreamDisclosure = "PORT_FORWARD_LOGICAL_STREAM_16_ACTIVE"
	// gatePreviewMaximumEntries bounds all submitted-field traversal, including hidden markers.
	// gatePreviewMaximumEntries ограничивает обход submitted fields, включая hidden markers.
	gatePreviewMaximumEntries = 2048
)

// ErrInvalidGatePreview excludes submitted values and parser diagnostics.
// ErrInvalidGatePreview не содержит submitted values и parser diagnostics.
var ErrInvalidGatePreview = errors.New("invalid managed Kubernetes gate preview")

// GatePreviewOption describes a registered submitted option; hidden values are always empty.
// GatePreviewOption описывает registered submitted option; скрытые values всегда пусты.
type GatePreviewOption struct {
	Key       string `json:"key"`
	Kind      string `json:"kind"`
	ValueJSON string `json:"valueJson"`
	Redacted  bool   `json:"redacted"`
}

// GatePreviewChange describes a submitted path and allowlisted value, never provider before-state.
// GatePreviewChange описывает submitted path и allowlisted value, не provider before-state.
type GatePreviewChange struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	From      string `json:"from"`
	ValueKind string `json:"valueKind"`
	ValueJSON string `json:"valueJson"`
	Redacted  bool   `json:"redacted"`
}

// GateEffectPreview is read-only evidence of exact submitted input; it cannot authorize execution.
// Limitations explicitly distinguish hidden fields, protected material and bounded summaries.
// GateEffectPreview — read-only evidence exact submitted input; не разрешает execution.
// Limitations явно различают hidden fields, protected material и bounded summaries.
type GateEffectPreview struct {
	InputDigest       string              `json:"inputDigest"`
	Options           []GatePreviewOption `json:"options"`
	Changes           []GatePreviewChange `json:"changes"`
	SecretMaterialRef string              `json:"secretMaterialRef"`
	Limitations       []string            `json:"limitations"`
}

// gatePreviewBuilder owns the bounded public projection and never retains raw input.
// gatePreviewBuilder владеет bounded public projection и не сохраняет raw input.
type gatePreviewBuilder struct {
	preview               GateEffectPreview
	budget, used, entries int
	input                 OperationInput
}

// BuildGateEffectPreview validates canonical persisted input through the single operation parser.
// Disclosure and budget are server-selected; false disclosure exposes only the digest and reason.
// Known builtin fields and typed options expose submitted values; arbitrary/custom data stays hidden.
// BuildGateEffectPreview проверяет canonical persisted input единственным operation parser.
// Disclosure и budget выбирает сервер; false раскрывает только digest и reason.
// Known builtin fields и typed options показывают submitted values; arbitrary/custom data скрыты.
func BuildGateEffectPreview(operation string, canonicalInput []byte, disclose bool, budget int) (GateEffectPreview, error) {
	empty := GateEffectPreview{}
	if budget < 1024 || budget > GatePreviewDetailBytes {
		return empty, ErrInvalidGatePreview
	}
	input, canonical, err := DecodeOperationInput(operation, canonicalInput)
	defer clear(canonical)
	defer clear(input.Body)
	defer clear(input.Options)
	if err != nil || !bytes.Equal(canonicalInput, canonical) {
		return empty, ErrInvalidGatePreview
	}
	hash := sha256.Sum256(canonical)
	builder := gatePreviewBuilder{preview: GateEffectPreview{InputDigest: hex.EncodeToString(hash[:]),
		Options: []GatePreviewOption{}, Changes: []GatePreviewChange{}, Limitations: []string{}}, budget: budget, used: 512, input: input}
	if !disclose {
		builder.limit("NOT_AUTHORIZED")
		return builder.preview, nil
	}
	if operation == "kubernetes.port-forward" {
		builder.limit(GatePortForwardLogicalStreamDisclosure)
	}
	var options map[string]any
	if len(input.Options) > 0 && internalrpcauth.DecodeStrictJSON(input.Options, &options) != nil {
		return empty, ErrInvalidGatePreview
	}
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		value := options[key]
		if key == "secretMaterialRef" {
			builder.preview.SecretMaterialRef, _ = value.(string)
			builder.limit("PROTECTED_MATERIAL_HIDDEN")
			continue
		}
		safe := gatePreviewOptionSafe(operation, key, value)
		item := GatePreviewOption{Key: key, Kind: "NONE", Redacted: !safe}
		if safe {
			item.Kind, item.ValueJSON = gatePreviewValue(value)
		} else {
			builder.limit("SENSITIVE_OR_UNCLASSIFIED_FIELDS_HIDDEN")
		}
		size, _ := json.Marshal(item)
		if builder.used+len(size)+1 > budget {
			item.Kind, item.ValueJSON, item.Redacted = "NONE", "", true
			builder.limit("BUDGET_EXCEEDED")
			size, _ = json.Marshal(item)
		}
		if builder.used+len(size)+1 <= budget {
			builder.preview.Options = append(builder.preview.Options, item)
			builder.used += len(size) + 1
		} else {
			builder.limit("BUDGET_EXCEEDED")
		}
	}
	if len(input.Body) > 0 {
		var body any
		if internalrpcauth.DecodeStrictJSON(input.Body, &body) != nil {
			return empty, ErrInvalidGatePreview
		}
		if operation == "kubernetes.delete" {
			metadata, ok := body.(map[string]any)
			if !ok {
				return empty, ErrInvalidGatePreview
			}
			keys := make([]string, 0, len(metadata))
			for key := range metadata {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				builder.change("SUBMIT", "/"+key, "", metadata[key], true, true)
			}
		} else if patches, ok := body.([]any); ok {
			for _, raw := range patches {
				patch, ok := raw.(map[string]any)
				if !ok {
					return empty, ErrInvalidGatePreview
				}
				op, _ := patch["op"].(string)
				path, _ := patch["path"].(string)
				from, _ := patch["from"].(string)
				value, hasValue := patch["value"]
				if hasValue {
					builder.walk(op, path, from, value)
				} else {
					builder.change(op, path, from, nil, false, gatePreviewPathSafe(input, path) && (from == "" || gatePreviewPathSafe(input, from)))
				}
			}
		} else {
			builder.walk("SUBMIT", "", "", body)
		}
	}
	if builder.preview.ValidateForOperation(operation, budget) != nil {
		return empty, ErrInvalidGatePreview
	}
	return builder.preview, nil
}

// limit records each finite disclosure limitation once in deterministic discovery order.
// limit записывает каждое finite disclosure limitation один раз в deterministic discovery order.
func (builder *gatePreviewBuilder) limit(reason string) {
	if !slices.Contains(builder.preview.Limitations, reason) {
		builder.preview.Limitations = append(builder.preview.Limitations, reason)
	}
}

// walk visits only submitted fields; unknown subtrees produce one hidden marker without their data.
// walk посещает лишь submitted fields; unknown subtrees дают один hidden marker без своих данных.
func (builder *gatePreviewBuilder) walk(operation, path, from string, value any) {
	if builder.entries >= gatePreviewMaximumEntries {
		builder.limit("BUDGET_EXCEEDED")
		return
	}
	if gatePreviewPathSafe(builder.input, path) {
		safe := from == "" || gatePreviewPathSafe(builder.input, from)
		builder.change(operation, path, from, value, true, safe && gatePreviewSubmittedValueSafe(builder.input, path, value))
		return
	}
	if !gatePreviewPathContainer(builder.input, path) {
		builder.change(operation, path, from, nil, false, false)
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		if len(keys) == 0 && path != "" {
			builder.change(operation, path, from, nil, false, false)
		}
		for _, key := range keys {
			builder.walk(operation, path+"/"+strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1"), from, typed[key])
		}
	case []any:
		if len(typed) == 0 {
			builder.change(operation, path, from, nil, false, false)
		}
		for index, child := range typed {
			builder.walk(operation, path+"/"+strconv.Itoa(index), from, child)
		}
	default:
		builder.change(operation, path, from, nil, false, false)
	}
}

// change appends one bounded typed change or an explicit redacted budget marker.
// change добавляет одно bounded typed изменение либо explicit redacted budget marker.
func (builder *gatePreviewBuilder) change(operation, path, from string, value any, present, safe bool) {
	builder.entries++
	item := GatePreviewChange{Operation: operation, Path: path, From: from, ValueKind: "NONE", Redacted: !safe}
	if present && safe {
		item.ValueKind, item.ValueJSON = gatePreviewValue(value)
	}
	if !safe {
		builder.limit("SENSITIVE_OR_UNCLASSIFIED_FIELDS_HIDDEN")
	}
	raw, _ := json.Marshal(item)
	if builder.used+len(raw)+1 > builder.budget {
		builder.limit("BUDGET_EXCEEDED")
		return
	}
	builder.preview.Changes = append(builder.preview.Changes, item)
	builder.used += len(raw) + 1
}

// gatePreviewValue encodes an already parsed JSON value with a finite display type.
// gatePreviewValue кодирует уже разобранное JSON value с finite display type.
func gatePreviewValue(value any) (string, string) {
	kind := "NONE"
	switch value.(type) {
	case string:
		kind = "STRING"
	case bool:
		kind = "BOOLEAN"
	case float64, json.Number:
		kind = "NUMBER"
	case []any:
		kind = "ARRAY"
	case map[string]any:
		kind = "OBJECT"
	case nil:
		kind = "NULL"
	}
	raw, _ := json.Marshal(value)
	return kind, string(raw)
}

// gatePreviewOptionSafe allows registered data with known semantics, never arbitrary environment.
// Credential-like markers add redaction; they are not the proof used for unclassified fields.
// gatePreviewOptionSafe разрешает registered data с known semantics, не arbitrary environment.
// Credential-like markers дополняют redaction; не доказывают безопасность unclassified fields.
func gatePreviewOptionSafe(operation, key string, value any) bool {
	if gatePreviewSensitiveValue(value) {
		return false
	}
	switch key {
	case "environment":
		return false
	case "assignments":
		return operation == "kubernetes.label" || operation == "kubernetes.set.image"
	case "command", "args":
		values, ok := value.([]any)
		if !ok {
			return false
		}
		for _, raw := range values {
			text, ok := raw.(string)
			if !ok || strings.ContainsAny(text, "\n\r;|&`$") || text == "-c" || text == "-Command" || strings.HasPrefix(text, "--eval") {
				return false
			}
		}
		return true
	default:
		// The operation parser already rejects all unknown option keys and types.
		// Operation parser уже отклоняет все unknown option keys и types.
		return true
	}
}

// gatePreviewSensitiveValue applies secondary credential markers to an otherwise allowlisted value.
// Unknown body paths are hidden independently, even when none of these markers match.
// gatePreviewSensitiveValue применяет дополнительные credential markers к allowlisted value.
// Unknown body paths скрыты независимо от наличия этих markers.
func gatePreviewSensitiveValue(value any) bool {
	switch typed := value.(type) {
	case string:
		lower := strings.ToLower(typed)
		for _, marker := range []string{"password", "passwd", "bearer ", "authorization", "private key", "client-secret", "client_secret", "access_token", "refresh_token", "--token", "--secret", "kubeconfig", "https://", "http://"} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if gatePreviewSensitiveValue(child) {
				return true
			}
		}
	case map[string]any:
		for key, child := range typed {
			if gatePreviewSensitiveValue(key) || gatePreviewSensitiveValue(child) {
				return true
			}
		}
	}
	return false
}

// gatePreviewNormalizedPath replaces array indexes only, preserving exact submitted JSON pointers.
// gatePreviewNormalizedPath заменяет лишь array indexes, сохраняя exact submitted JSON pointers.
func gatePreviewNormalizedPath(path string) string {
	parts := strings.Split(path, "/")
	for index, part := range parts {
		if part != "" {
			if _, err := strconv.ParseUint(part, 10, 32); err == nil {
				parts[index] = "#"
			}
		}
	}
	return strings.Join(parts, "/")
}

// gatePreviewPathSafe recognizes submitted builtin values; no arbitrary custom schema is trusted.
// gatePreviewPathSafe распознаёт submitted builtin values; arbitrary custom schema не доверяется.
func gatePreviewPathSafe(input OperationInput, path string) bool {
	if gatePreviewSensitiveValue(path) {
		return false
	}
	normalized := gatePreviewNormalizedPath(path)
	if slices.Contains([]string{"/apiVersion", "/kind", "/metadata/name", "/metadata/generateName", "/metadata/namespace", "/metadata/uid", "/metadata/resourceVersion"}, normalized) {
		return true
	}
	if input.Group == "" && input.Resource == "secrets" {
		return normalized == "/type"
	}
	builtin := input.Group == "" && slices.Contains([]string{"pods", "services", "replicationcontrollers", "persistentvolumeclaims", "configmaps"}, input.Resource) || input.Group == "apps" && slices.Contains([]string{"deployments", "statefulsets", "daemonsets", "replicasets"}, input.Resource) || input.Group == "batch" && slices.Contains([]string{"jobs", "cronjobs"}, input.Resource) || input.Group == "rbac.authorization.k8s.io" && slices.Contains([]string{"roles", "clusterroles", "rolebindings", "clusterrolebindings"}, input.Resource)
	if !builtin {
		return false
	}
	for _, prefix := range []string{"/metadata/labels/", "/spec/template/metadata/labels/", "/spec/jobTemplate/spec/template/metadata/labels/", "/spec/selector/matchLabels/"} {
		if strings.HasPrefix(path, prefix) && !strings.Contains(strings.TrimPrefix(path, prefix), "/") {
			return true
		}
	}
	if input.Group == "" && input.Resource == "services" && strings.HasPrefix(path, "/spec/selector/") && !strings.Contains(strings.TrimPrefix(path, "/spec/selector/"), "/") {
		return true
	}
	if input.Group == "rbac.authorization.k8s.io" {
		return slices.Contains([]string{"/rules/#/apiGroups", "/rules/#/resources", "/rules/#/verbs", "/rules/#/resourceNames", "/rules/#/nonResourceURLs", "/subjects/#/kind", "/subjects/#/name", "/subjects/#/namespace", "/subjects/#/apiGroup", "/roleRef/kind", "/roleRef/name", "/roleRef/apiGroup"}, normalized)
	}
	if slices.Contains([]string{"/spec/replicas", "/spec/paused", "/spec/revisionHistoryLimit", "/spec/progressDeadlineSeconds", "/spec/type"}, normalized) {
		return true
	}
	for _, prefix := range []string{"/spec", "/spec/template/spec", "/spec/jobTemplate/spec/template/spec"} {
		suffix := strings.TrimPrefix(normalized, prefix)
		if suffix == normalized {
			continue
		}
		if slices.Contains([]string{"/serviceAccountName", "/automountServiceAccountToken", "/hostNetwork", "/hostPID", "/hostIPC", "/restartPolicy"}, suffix) {
			return true
		}
		for _, containers := range []string{"/containers/#", "/initContainers/#", "/ephemeralContainers/#"} {
			leaf := strings.TrimPrefix(suffix, containers)
			if leaf == suffix {
				continue
			}
			if slices.Contains([]string{"/name", "/image", "/imagePullPolicy"}, leaf) {
				return true
			}
			for _, resourcePrefix := range []string{"/resources/limits/", "/resources/requests/"} {
				if strings.HasPrefix(leaf, resourcePrefix) && slices.Contains([]string{"cpu", "memory", "ephemeral-storage", "storage"}, strings.TrimPrefix(leaf, resourcePrefix)) {
					return true
				}
			}
		}
	}
	return false
}

// gatePreviewSubmittedValueSafe enforces the selected display type in addition to the exact path.
// An object hidden inside a string/number field is never treated as a safe arbitrary JSON subtree.
// gatePreviewSubmittedValueSafe проверяет selected display type помимо exact path.
// Object внутри string/number field не считается safe arbitrary JSON subtree.
func gatePreviewSubmittedValueSafe(input OperationInput, path string, value any) bool {
	if !gatePreviewPathSafe(input, path) || gatePreviewSensitiveValue(value) {
		return false
	}
	normalized := gatePreviewNormalizedPath(path)
	if input.Group == "rbac.authorization.k8s.io" && strings.HasPrefix(normalized, "/rules/#/") {
		values, ok := value.([]any)
		if !ok {
			return false
		}
		for _, item := range values {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	}
	if slices.Contains([]string{"/spec/replicas", "/spec/revisionHistoryLimit", "/spec/progressDeadlineSeconds"}, normalized) {
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		integer, err := number.Int64()
		return err == nil && integer >= 0
	}
	if normalized == "/spec/paused" || strings.HasSuffix(normalized, "/automountServiceAccountToken") || strings.HasSuffix(normalized, "/hostNetwork") || strings.HasSuffix(normalized, "/hostPID") || strings.HasSuffix(normalized, "/hostIPC") {
		_, ok := value.(bool)
		return ok
	}
	_, ok := value.(string)
	return ok
}

// gatePreviewPathContainer allows traversal through known builtin parents, not custom payload trees.
// gatePreviewPathContainer разрешает обход known builtin parents, не custom payload trees.
func gatePreviewPathContainer(input OperationInput, path string) bool {
	if path == "" || path == "/metadata" {
		return true
	}
	if input.Group == "" && input.Resource == "secrets" {
		return false
	}
	if gatePreviewPathSafe(input, "/spec/replicas") || input.Group == "rbac.authorization.k8s.io" {
		return !gatePreviewSensitiveValue(path) && !strings.Contains(path, "/env") && !strings.Contains(path, "/annotations") && !strings.Contains(path, "/data") && !strings.Contains(path, "/stringData")
	}
	return false
}

// Validate checks the finite public shape and encoded value budget without granting disclosure.
// Validate проверяет finite public shape и budget encoded values, не выдавая disclosure.
func (preview GateEffectPreview) Validate(budget int) error {
	if budget < 1024 || budget > GatePreviewDetailBytes || !ValidDigest(preview.InputDigest) || preview.Options == nil || preview.Changes == nil || preview.Limitations == nil || len(preview.Options) > 128 || len(preview.Changes) > gatePreviewMaximumEntries || len(preview.Limitations) > 4 {
		return ErrInvalidGatePreview
	}
	if preview.SecretMaterialRef != "" && (!strings.HasPrefix(preview.SecretMaterialRef, "krs_") || !ValidLocator(preview.SecretMaterialRef) || len(preview.SecretMaterialRef) > 96) {
		return ErrInvalidGatePreview
	}
	seenReasons := map[string]bool{}
	for _, reason := range preview.Limitations {
		if seenReasons[reason] {
			return ErrInvalidGatePreview
		}
		seenReasons[reason] = true
		if !slices.Contains([]string{"NOT_AUTHORIZED", "SENSITIVE_OR_UNCLASSIFIED_FIELDS_HIDDEN", "PROTECTED_MATERIAL_HIDDEN", "BUDGET_EXCEEDED", GatePortForwardLogicalStreamDisclosure}, reason) {
			return ErrInvalidGatePreview
		}
	}
	if seenReasons["NOT_AUTHORIZED"] && (len(preview.Options) > 0 || len(preview.Changes) > 0 || preview.SecretMaterialRef != "" || seenReasons[GatePortForwardLogicalStreamDisclosure]) {
		return ErrInvalidGatePreview
	}
	seenOptions := map[string]bool{}
	for _, item := range preview.Options {
		if seenOptions[item.Key] || item.Kind == "NONE" && !item.Redacted {
			return ErrInvalidGatePreview
		}
		seenOptions[item.Key] = true
		if item.Key == "" || len(item.Key) > 64 || !utf8.ValidString(item.Key) || validateGatePreviewValue(item.Kind, item.ValueJSON, item.Redacted) != nil {
			return ErrInvalidGatePreview
		}
	}
	for _, item := range preview.Changes {
		if !slices.Contains([]string{"SUBMIT", "add", "remove", "replace", "copy", "move", "test"}, item.Operation) || len(item.Path) > 4096 || len(item.From) > 4096 || !utf8.ValidString(item.Path+item.From) || !strings.HasPrefix(item.Path, "/") || item.From != "" && !strings.HasPrefix(item.From, "/") || validateGatePreviewValue(item.ValueKind, item.ValueJSON, item.Redacted) != nil {
			return ErrInvalidGatePreview
		}
	}
	raw, err := json.Marshal(preview)
	if err != nil || len(raw) > budget {
		return ErrInvalidGatePreview
	}
	return nil
}

// validateGatePreviewValue rejects malformed or mismatched display JSON, including hidden values.
// validateGatePreviewValue отклоняет malformed либо mismatched display JSON, включая hidden values.
func validateGatePreviewValue(kind, raw string, redacted bool) error {
	if kind == "NONE" {
		if raw != "" {
			return ErrInvalidGatePreview
		}
		return nil
	}
	if redacted || !utf8.ValidString(raw) {
		return ErrInvalidGatePreview
	}
	var value any
	if internalrpcauth.DecodeStrictJSON([]byte(raw), &value) != nil {
		return ErrInvalidGatePreview
	}
	actual, canonical := gatePreviewValue(value)
	if actual != kind || canonical != raw {
		return ErrInvalidGatePreview
	}
	return nil
}

// ValidateForOperation binds the closed server disclosure to PF alone and requires it for every disclosed PF decision; opaque unauthorized projections remain unchanged.
// It validates evidence only, never actor eligibility, selected ports, budget, deadline or effect authority.
// ValidateForOperation связывает closed server disclosure только с PF и требует его для каждого disclosed PF decision; opaque unauthorized projections не меняются.
// Проверяются лишь сведения, не actor eligibility, selected ports, budget, deadline либо effect authority.
func (preview GateEffectPreview) ValidateForOperation(operation string, budget int) error {
	if preview.Validate(budget) != nil {
		return ErrInvalidGatePreview
	}
	present := slices.Contains(preview.Limitations, GatePortForwardLogicalStreamDisclosure)
	expected := operation == "kubernetes.port-forward" && !slices.Contains(preview.Limitations, "NOT_AUTHORIZED")
	if expected {
		for _, option := range preview.Options {
			if option.Key != "ports" && option.Key != "timeoutSeconds" {
				return ErrInvalidGatePreview
			}
		}
	}
	if present != expected {
		return ErrInvalidGatePreview
	}
	return nil
}
