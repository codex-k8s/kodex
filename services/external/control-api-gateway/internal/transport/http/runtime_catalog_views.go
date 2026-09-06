package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var errRuntimeCatalogView = errors.New("runtime catalog response is invalid")
var runtimeEffortPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// Порядок полей соответствует canonical JSON schema owner, включая пустые значения.
type canonicalOverlayField struct {
	Key           string   `json:"key"`
	ValueType     string   `json:"valueType"`
	AllowedValues []string `json:"allowedValues"`
	DefaultValue  string   `json:"defaultValue"`
	Description   string   `json:"description"`
	Completion    string   `json:"completion"`
	Hover         string   `json:"hover"`
}
type canonicalOverlaySchema struct {
	Revision     string                  `json:"revision"`
	Digest       string                  `json:"digest"`
	Fields       []canonicalOverlayField `json:"fields"`
	MaximumBytes int32                   `json:"maximumBytes"`
}

func validOverlaySchema(schema *cp.ConfigOverlaySchema) bool {
	if schema == nil || schema.MaximumBytes != 65536 || len(schema.Fields) != 4 || !modelCatalogDigest.MatchString(schema.Digest) || schema.Revision != "cos_"+schema.Digest {
		return false
	}
	canonical := canonicalOverlaySchema{MaximumBytes: schema.MaximumBytes, Fields: []canonicalOverlayField{}}
	seen := map[string]bool{}
	for _, field := range schema.Fields {
		if field == nil || seen[field.Key] || !slices.Contains([]string{"model_reasoning_effort", "personality", "allow_login_shell", "history.persistence"}, field.Key) || len(field.AllowedValues) > 16 || !safeOverlayText(field.Description, 512) || !safeOverlayText(field.Completion, 256) || !safeOverlayText(field.Hover, 1024) {
			return false
		}
		seen[field.Key] = true
		if field.ValueType != "string" && field.ValueType != "boolean" || (field.Key == "allow_login_shell") != (field.ValueType == "boolean") {
			return false
		}
		values := map[string]bool{}
		for _, value := range field.AllowedValues {
			if !runtimeEffortPattern.MatchString(value) || values[value] {
				return false
			}
			values[value] = true
		}
		if field.DefaultValue != "" && !values[field.DefaultValue] {
			return false
		}
		if field.Key == "allow_login_shell" && (!slices.Equal(field.AllowedValues, []string{"false"}) || field.DefaultValue != "false") {
			return false
		}
		canonical.Fields = append(canonical.Fields, canonicalOverlayField{field.Key, field.ValueType, append([]string{}, field.AllowedValues...), field.DefaultValue, field.Description, field.Completion, field.Hover})
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]) == schema.Digest
}

func safeOverlayText(text string, maximum int) bool {
	return len(text) <= maximum && utf8.ValidString(text) && !strings.ContainsAny(text, "\x00\r\n")
}

func validOverlayDiagnostic(diagnostic *cp.ConfigOverlayDiagnostic) bool {
	if diagnostic == nil || diagnostic.Line < 0 || diagnostic.Line > 65537 || diagnostic.Column < 0 || diagnostic.Column > 65537 {
		return false
	}
	if diagnostic.Key != "" && !slices.Contains([]string{"model_reasoning_effort", "personality", "allow_login_shell", "history.persistence", "model", "model_provider", "model_providers", "api_key", "approval_policy", "sandbox_mode", "permissions", "mcp_servers", "shell_environment_policy", "cli_auth_credentials_store"}, diagnostic.Key) {
		return false
	}
	switch diagnostic.Code {
	case "CONFIG_OVERLAY_SYNTAX_INVALID":
		return diagnostic.Message == "Overlay size or encoding is invalid" || diagnostic.Message == "Overlay TOML syntax is invalid"
	case "CONFIG_OVERLAY_KEY_FORBIDDEN":
		return diagnostic.Message == "Overlay key is not allowed" || diagnostic.Message == "Overlay contains protected content"
	case "CONFIG_OVERLAY_VALUE_INVALID":
		return diagnostic.Message == "Overlay value is not allowed"
	case "CONFIG_OVERLAY_EFFORT_UNSUPPORTED":
		return diagnostic.Message == "Reasoning effort is not supported by the selected model"
	default:
		return false
	}
}

func validateRuntimeCatalogMessage(message protoreflect.Message, depth int) error {
	if depth > 64 {
		return errRuntimeCatalogView
	}
	switch item := message.Interface().(type) {
	case *cp.Project:
		if !validProjectCard(item) {
			return errRuntimeCatalogView
		}
	case *cp.Agent:
		if item == nil || item.CurrentRunRef != "" && !fileTargetRef(item.CurrentRunRef) {
			return errRuntimeCatalogView
		}
	case *cp.WorkflowCardSummary:
		if !validWorkflowCardSummary(item) {
			return errRuntimeCatalogView
		}
	case *cp.ListArtifactsResponse:
		if !validCountedCatalogPage(item.GetTotal(), len(item.GetArtifacts()), item.GetPage()) {
			return errRuntimeCatalogView
		}
		for _, artifact := range item.GetArtifacts() {
			if artifact == nil {
				return errRuntimeCatalogView
			}
		}
	case *cp.ListRunsResponse:
		if !validCountedCatalogPage(item.GetTotal(), len(item.GetRuns()), item.GetPage()) {
			return errRuntimeCatalogView
		}
		for _, run := range item.GetRuns() {
			if run == nil {
				return errRuntimeCatalogView
			}
		}
	case *cp.AgentRuntimeConfigurationView:
		if !validOverlaySchema(item.OverlaySchema) {
			return errRuntimeCatalogView
		}
	case *cp.ProviderAccountCandidate:
		unpinned := item.CatalogRevision == "" && item.CatalogDigest == "" && item.ProviderDefinitionKey == ""
		if !unpinned && (!modelCatalogDigest.MatchString(item.CatalogDigest) || item.CatalogRevision != "mcat_"+item.CatalogDigest || !modelProviderKey.MatchString(item.ProviderDefinitionKey)) || item.DefaultReasoningEffort != "" && (!runtimeEffortPattern.MatchString(item.DefaultReasoningEffort) || unpinned) {
			return errRuntimeCatalogView
		}
	case *cp.ProviderAccountUsage:
		if !validProviderUsage(item) {
			return errRuntimeCatalogView
		}
	case *cp.ProviderAccount:
		if item.Usage != nil && item.Usage.AccountVersion != item.Version || !validProviderAccountLifecycle(item) {
			return errRuntimeCatalogView
		}
	case *cp.Workflow:
		if !validWorkflowLaunchReadiness(item) || !validWorkflowCardSummary(item.GetCardSummary()) {
			return errRuntimeCatalogView
		}
	case *cp.ConfigOverlayVersion:
		if len(item.Diagnostics) > 16 || (item.SchemaRevision != "" || item.SchemaDigest != "") && (!modelCatalogDigest.MatchString(item.SchemaDigest) || item.SchemaRevision != "cos_"+item.SchemaDigest) {
			return errRuntimeCatalogView
		}
		for _, diagnostic := range item.Diagnostics {
			if !validOverlayDiagnostic(diagnostic) {
				return errRuntimeCatalogView
			}
		}
	}
	var invalid error
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Message() == nil || field.IsMap() {
			return true
		}
		if field.IsList() {
			for index := 0; index < value.List().Len(); index++ {
				if err := validateRuntimeCatalogMessage(value.List().Get(index).Message(), depth+1); err != nil {
					invalid = err
					return false
				}
			}
		} else {
			invalid = validateRuntimeCatalogMessage(value.Message(), depth+1)
		}
		return invalid == nil
	})
	return invalid
}
