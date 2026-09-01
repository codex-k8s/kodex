package runtimecontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

const (
	MaximumConfigOverlayBytes      = 64 << 10
	MaximumRuntimeEnvironmentBytes = 64 << 10
)

var environmentNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,126}$`)
var secretNamePattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9.]{0,251}[a-z0-9])?$`)
var secretKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,253}$`)
var runtimeToolCommandPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,159}$`)

// ConfigOverlay содержит только owner-editable и не несущие authority ключи
// Codex. Provider, credentials, sandbox, approvals, permissions, MCP и shell
// environment всегда материализуются сервером и не входят в overlay.
type ConfigOverlay struct {
	ModelReasoningEffort string         `toml:"model_reasoning_effort,omitempty" json:"modelReasoningEffort,omitempty"`
	Personality          string         `toml:"personality,omitempty" json:"personality,omitempty"`
	AllowLoginShell      *bool          `toml:"allow_login_shell,omitempty" json:"allowLoginShell,omitempty"`
	History              OverlayHistory `toml:"history,omitempty" json:"history,omitempty"`
}

type OverlayHistory struct {
	Persistence string `toml:"persistence,omitempty" json:"persistence,omitempty"`
}

type RuntimeEnvironmentValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// RuntimeSecretProjection не содержит Secret value. Метаданные связывают
// RuntimeRevision с одной immutable Kubernetes Secret revision и одним key.
type RuntimeSecretProjection struct {
	Name                  string `json:"name"`
	SecretName            string `json:"secret_name"`
	SecretKey             string `json:"secret_key"`
	SecretUID             string `json:"secret_uid"`
	SecretResourceVersion string `json:"secret_resource_version"`
	ContentSHA256         string `json:"content_sha256"`
}

// RuntimeEnvironmentImage связывает версию окружения с exact promoted image.
// Для project-scoped окружений ArtifactRef/RecipeRef/RecipeGeneration обязательны.
// Always-hot system assistant использует platform-owned exact reference без DB artifact.
type RuntimeEnvironmentImage struct {
	ArtifactRef      string `json:"artifact_ref,omitempty"`
	RecipeRef        string `json:"recipe_ref,omitempty"`
	RecipeGeneration int64  `json:"recipe_generation,omitempty"`
	Reference        string `json:"reference"`
	Digest           string `json:"digest"`
}

// RuntimeEnvironmentTool является проверенным image executable, разрешенным
// конкретной immutable версией окружения.
type RuntimeEnvironmentTool struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description"`
	UsageHint   string `json:"usage_hint,omitempty"`
}

type SafeEffectiveConfigInput struct {
	Model, Provider, RuntimeProfileRef                          string
	RuntimeConfigRef, RuntimeConfigDigest                       string
	ProviderPolicyRef, ProviderPolicyMode, ProviderPolicyDigest string
	ConfigOverlayRef, ConfigOverlayDigest                       string
	RuntimeEnvironmentRef, RuntimeEnvironmentDigest             string
	EnvironmentBindingRef, EnvironmentBindingDigest             string
	Overlay                                                     string
	RuntimeConfigVersion, ProviderPolicyVersion                 int64
	ConfigOverlayVersion, RuntimeEnvironmentVersion             int64
	EnvironmentBindingVersion                                   int64
	Values                                                      []RuntimeEnvironmentValue
	Secrets                                                     []RuntimeSecretProjection
}

func RenderSafeEffectiveConfig(input SafeEffectiveConfigInput) (string, error) {
	overlay, err := ParseConfigOverlay(input.Overlay)
	if err != nil || ValidateRuntimeEnvironment(input.Values, input.Secrets) != nil {
		return "", errors.New("effective configuration input is invalid")
	}
	type safeSecret struct {
		Name, SecretName, SecretKey, SecretUID, SecretResourceVersion, ContentSHA256 string
	}
	type safeReadback struct {
		Model                string         `toml:"model"`
		ModelReasoningEffort string         `toml:"model_reasoning_effort,omitempty"`
		Personality          string         `toml:"personality,omitempty"`
		AllowLoginShell      *bool          `toml:"allow_login_shell,omitempty"`
		History              OverlayHistory `toml:"history"`
		Kodex                struct {
			RuntimeConfig struct {
				Ref, Digest string
				Version     int64
			}
			ProviderPolicy struct {
				Ref, Mode, Digest string
				Version           int64
			}
			Overlay struct {
				Ref, Digest string
				Version     int64
			}
			Environment struct {
				Ref, Digest string
				Version     int64
				Values      map[string]string
				Secrets     []safeSecret
			}
			Binding struct {
				Ref, Digest string
				Version     int64
			}
			Provider, RuntimeProfile string
		}
	}
	readback := safeReadback{Model: input.Model, ModelReasoningEffort: overlay.ModelReasoningEffort,
		Personality: overlay.Personality, AllowLoginShell: overlay.AllowLoginShell, History: overlay.History}
	if readback.History.Persistence == "" {
		readback.History.Persistence = "save-all"
	}
	readback.Kodex.Provider, readback.Kodex.RuntimeProfile = input.Provider, input.RuntimeProfileRef
	readback.Kodex.RuntimeConfig.Ref, readback.Kodex.RuntimeConfig.Version, readback.Kodex.RuntimeConfig.Digest = input.RuntimeConfigRef, input.RuntimeConfigVersion, input.RuntimeConfigDigest
	readback.Kodex.ProviderPolicy.Ref, readback.Kodex.ProviderPolicy.Version = input.ProviderPolicyRef, input.ProviderPolicyVersion
	readback.Kodex.ProviderPolicy.Mode, readback.Kodex.ProviderPolicy.Digest = input.ProviderPolicyMode, input.ProviderPolicyDigest
	readback.Kodex.Overlay.Ref, readback.Kodex.Overlay.Version, readback.Kodex.Overlay.Digest = input.ConfigOverlayRef, input.ConfigOverlayVersion, input.ConfigOverlayDigest
	readback.Kodex.Environment.Ref, readback.Kodex.Environment.Version, readback.Kodex.Environment.Digest = input.RuntimeEnvironmentRef, input.RuntimeEnvironmentVersion, input.RuntimeEnvironmentDigest
	readback.Kodex.Environment.Values = make(map[string]string, len(input.Values))
	for _, item := range input.Values {
		readback.Kodex.Environment.Values[item.Name] = item.Value
	}
	for _, item := range input.Secrets {
		readback.Kodex.Environment.Secrets = append(readback.Kodex.Environment.Secrets, safeSecret{
			Name: item.Name, SecretName: item.SecretName, SecretKey: item.SecretKey, SecretUID: item.SecretUID,
			SecretResourceVersion: item.SecretResourceVersion, ContentSHA256: item.ContentSHA256,
		})
	}
	readback.Kodex.Binding.Ref, readback.Kodex.Binding.Version, readback.Kodex.Binding.Digest = input.EnvironmentBindingRef, input.EnvironmentBindingVersion, input.EnvironmentBindingDigest
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(readback); err != nil {
		return "", errors.New("encode safe effective configuration")
	}
	return encoded.String(), nil
}

func ParseConfigOverlay(raw string) (ConfigOverlay, error) {
	if ValidateConfigOverlayDraftPayload(raw) != nil {
		return ConfigOverlay{}, errors.New("config overlay size or encoding is invalid")
	}
	var overlay ConfigOverlay
	metadata, err := toml.Decode(raw, &overlay)
	if err != nil {
		return ConfigOverlay{}, errors.New("config overlay TOML is invalid")
	}
	if undecoded := metadata.Undecoded(); len(undecoded) != 0 {
		return ConfigOverlay{}, errors.New("config overlay contains unsupported keys")
	}
	if !containsString([]string{"", "minimal", "low", "medium", "high", "xhigh"}, overlay.ModelReasoningEffort) ||
		!containsString([]string{"", "none", "friendly", "pragmatic"}, overlay.Personality) ||
		!containsString([]string{"", "save-all", "none"}, overlay.History.Persistence) ||
		(overlay.AllowLoginShell != nil && *overlay.AllowLoginShell) {
		return ConfigOverlay{}, errors.New("config overlay value is outside the allowlist")
	}
	return overlay, nil
}

func ValidateConfigOverlayDraftPayload(raw string) error {
	if len(raw) > MaximumConfigOverlayBytes || !utf8.ValidString(raw) || strings.ContainsRune(raw, '\x00') {
		return errors.New("config overlay size or encoding is invalid")
	}
	lower := strings.ToLower(raw)
	for _, marker := range []string{
		"api_key", "apikey", "access_token", "refresh_token", "bearer", "password", "credential",
		"private_key", "client_secret", "authorization", "cookie", "model_provider", "model_providers",
		"openai_base_url", "chatgpt_base_url", "mcp_servers", "approval_policy", "sandbox_mode",
		"default_permissions", "permissions", "shell_environment_policy", "cli_auth_credentials_store",
		"-----begin ",
	} {
		if strings.Contains(lower, marker) {
			return errors.New("config overlay contains a protected or credential key")
		}
	}
	return nil
}

func CanonicalConfigOverlay(raw string) (string, string, error) {
	overlay, err := ParseConfigOverlay(raw)
	if err != nil {
		return "", "", err
	}
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(overlay); err != nil {
		return "", "", errors.New("encode canonical config overlay")
	}
	canonical := strings.TrimSpace(encoded.String())
	if canonical != "" {
		canonical += "\n"
	}
	digest := sha256.Sum256([]byte(canonical))
	return canonical, hex.EncodeToString(digest[:]), nil
}

func ValidateRuntimeEnvironment(values []RuntimeEnvironmentValue, secrets []RuntimeSecretProjection) error {
	if len(values) > 128 || len(secrets) > 128 {
		return errors.New("runtime environment item limit exceeded")
	}
	names := make(map[string]struct{}, len(values)+len(secrets))
	totalBytes := 0
	for _, item := range values {
		if !validRuntimeEnvironmentName(item.Name) || len(item.Value) > 8<<10 ||
			!utf8.ValidString(item.Value) || strings.ContainsRune(item.Value, '\x00') {
			return errors.New("runtime environment value is invalid")
		}
		if _, duplicate := names[item.Name]; duplicate {
			return errors.New("runtime environment name is duplicated")
		}
		names[item.Name] = struct{}{}
		totalBytes += len(item.Name) + len(item.Value)
	}
	for _, item := range secrets {
		if !validRuntimeEnvironmentName(item.Name) || !secretNamePattern.MatchString(item.SecretName) ||
			!secretKeyPattern.MatchString(item.SecretKey) || item.SecretUID == "" || len(item.SecretUID) > 128 ||
			item.SecretResourceVersion == "" || len(item.SecretResourceVersion) > 128 ||
			!sha256Pattern.MatchString(item.ContentSHA256) {
			return errors.New("runtime Secret projection is invalid")
		}
		if _, duplicate := names[item.Name]; duplicate {
			return errors.New("runtime environment name is duplicated")
		}
		names[item.Name] = struct{}{}
		totalBytes += len(item.Name) + len(item.SecretName) + len(item.SecretKey) + len(item.SecretUID) + len(item.SecretResourceVersion) + len(item.ContentSHA256)
	}
	if totalBytes > MaximumRuntimeEnvironmentBytes {
		return errors.New("runtime environment byte limit exceeded")
	}
	return nil
}

func RuntimeEnvironmentDigest(values []RuntimeEnvironmentValue, secrets []RuntimeSecretProjection, image RuntimeEnvironmentImage, tools []RuntimeEnvironmentTool, policies ...RuntimeEnvironmentPolicy) (string, error) {
	coreDigest, err := RuntimeEnvironmentCoreDigest(values, secrets, image, tools)
	if err != nil {
		return "", err
	}
	policy := DefaultRuntimeEnvironmentPolicy()
	if len(policies) > 1 {
		return "", errors.New("runtime environment policy cardinality is invalid")
	}
	if len(policies) == 1 {
		policy = policies[0]
	}
	normalized, err := NormalizeRuntimeEnvironmentPolicy(policy)
	if err != nil {
		return "", err
	}
	return digestParts("runtime-environment-v2", coreDigest, normalized.ResourcesDigest,
		normalized.VolumesDigest, normalized.NetworkDigest, normalized.RBACDigest), nil
}

func RuntimeEnvironmentCoreDigest(values []RuntimeEnvironmentValue, secrets []RuntimeSecretProjection, image RuntimeEnvironmentImage, tools []RuntimeEnvironmentTool) (string, error) {
	if err := ValidateRuntimeEnvironment(values, secrets); err != nil {
		return "", err
	}
	if err := validateRuntimeEnvironmentImage(image); err != nil {
		return "", err
	}
	if err := validateRuntimeEnvironmentTools(tools); err != nil {
		return "", err
	}
	values = append([]RuntimeEnvironmentValue(nil), values...)
	secrets = append([]RuntimeSecretProjection(nil), secrets...)
	tools = append([]RuntimeEnvironmentTool(nil), tools...)
	sort.Slice(values, func(left, right int) bool { return values[left].Name < values[right].Name })
	sort.Slice(secrets, func(left, right int) bool { return secrets[left].Name < secrets[right].Name })
	sort.Slice(tools, func(left, right int) bool { return tools[left].Command < tools[right].Command })
	var payload bytes.Buffer
	payload.WriteString("image\x00" + image.ArtifactRef + "\x00" + image.RecipeRef + "\x00")
	payload.WriteString(strconv.FormatInt(image.RecipeGeneration, 10) + "\x00" + image.Reference + "\x00" + image.Digest + "\x00")
	for _, item := range values {
		payload.WriteString("value\x00" + item.Name + "\x00" + item.Value + "\x00")
	}
	for _, item := range secrets {
		payload.WriteString("secret\x00" + item.Name + "\x00" + item.SecretName + "\x00" + item.SecretKey + "\x00" + item.SecretUID + "\x00" + item.SecretResourceVersion + "\x00" + item.ContentSHA256 + "\x00")
	}
	for _, item := range tools {
		payload.WriteString("tool\x00" + item.Name + "\x00" + item.Command + "\x00" + item.Description + "\x00" + item.UsageHint + "\x00")
	}
	coreDigest := sha256.Sum256(payload.Bytes())
	return hex.EncodeToString(coreDigest[:]), nil
}

func validateRuntimeEnvironmentImage(image RuntimeEnvironmentImage) error {
	if !validPinnedImage(image.Reference, image.Digest) {
		return errors.New("runtime environment image is invalid")
	}
	if image.ArtifactRef == "" && image.RecipeRef == "" && image.RecipeGeneration == 0 {
		return nil
	}
	if !opaqueReferencePattern.MatchString(image.ArtifactRef) || !strings.HasPrefix(image.ArtifactRef, "imgart_") ||
		!opaqueReferencePattern.MatchString(image.RecipeRef) || !strings.HasPrefix(image.RecipeRef, "imgrec_") || image.RecipeGeneration < 1 {
		return errors.New("runtime environment image identity is invalid")
	}
	return nil
}

func validateRuntimeEnvironmentTools(tools []RuntimeEnvironmentTool) error {
	if len(tools) > 128 {
		return errors.New("runtime environment tools exceed limit")
	}
	seen := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) != tool.Name || tool.Name == "" || len(tool.Name) > 160 ||
			!runtimeToolCommandPattern.MatchString(tool.Command) || strings.TrimSpace(tool.Description) != tool.Description ||
			tool.Description == "" || len(tool.Description) > 500 || len(tool.UsageHint) > 500 ||
			!utf8.ValidString(tool.Name+tool.Description+tool.UsageHint) {
			return errors.New("runtime environment tool is invalid")
		}
		if _, duplicate := seen[tool.Command]; duplicate {
			return errors.New("runtime environment tool is duplicated")
		}
		seen[tool.Command] = struct{}{}
	}
	return nil
}

func DecodeRuntimeEnvironment(rawValues, rawSecrets []byte) ([]RuntimeEnvironmentValue, []RuntimeSecretProjection, error) {
	var values []RuntimeEnvironmentValue
	var secrets []RuntimeSecretProjection
	if err := strictJSON(rawValues, &values); err != nil {
		return nil, nil, errors.New("decode runtime environment values")
	}
	if err := strictJSON(rawSecrets, &secrets); err != nil {
		return nil, nil, errors.New("decode runtime Secret projections")
	}
	if err := ValidateRuntimeEnvironment(values, secrets); err != nil {
		return nil, nil, err
	}
	return values, secrets, nil
}

func DecodeRuntimeEnvironmentTools(raw []byte) ([]RuntimeEnvironmentTool, error) {
	var tools []RuntimeEnvironmentTool
	if err := strictJSON(raw, &tools); err != nil {
		return nil, errors.New("decode runtime environment tools")
	}
	if err := validateRuntimeEnvironmentTools(tools); err != nil {
		return nil, err
	}
	return tools, nil
}

func strictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		return errors.New("invalid JSON")
	}
	return nil
}

func validRuntimeEnvironmentName(value string) bool {
	if !environmentNamePattern.MatchString(value) {
		return false
	}
	for _, prefix := range []string{"KODEX_", "CODEX_", "OPENAI_", "OTEL_", "AWS_", "AZURE_", "GOOGLE_", "KUBERNETES_"} {
		if strings.HasPrefix(value, prefix) {
			return false
		}
	}
	switch value {
	case "HOME", "PATH", "PWD", "SHELL", "USER", "LOGNAME", "TMPDIR", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR":
		return false
	default:
		return true
	}
}
