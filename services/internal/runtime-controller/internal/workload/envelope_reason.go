package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// diagnosticOpaque recognizes opaque references only for the post-rejection
// envelope diagnostic. It grants no execution rights; mutation tests compare
// diagnostic classifications with the shared validator and drift stays UNKNOWN.
// diagnosticOpaque распознаёт непрозрачные ссылки только для диагностики уже
// отвергнутого конверта. Допуск не выдаётся; mutation-тесты сверяют классификацию
// с общим валидатором, а нераспознанное расхождение остаётся UNKNOWN.
var diagnosticOpaque = regexp.MustCompile(`^[a-z][a-z0-9]{1,11}_[A-Za-z0-9_-]{8,84}$`)

// diagnosticProfile recognizes the bounded ASCII profile identifier used by
// envelope diagnostics; it does not resolve a profile or prove its ownership.
// diagnosticProfile распознаёт ограниченный ASCII-идентификатор профиля для
// диагностики конверта; профиль не загружается и его принадлежность не доказывается.
var diagnosticProfile = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// diagnosticImageDigest recognizes a lowercase SHA-256 image digest with its
// required algorithm prefix; matching does not verify image bytes or admission.
// diagnosticImageDigest распознаёт digest образа в нижнем регистре с обязательным
// префиксом SHA-256; совпадение не проверяет байты образа и его допуск.
var diagnosticImageDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// diagnosticSystemRevision recognizes the two platform revision-name forms as
// a diagnostic alternative to opaque references, not as evidence of authority.
// diagnosticSystemRevision распознаёт две формы имени платформенной ревизии
// для диагностики вместо opaque-ссылки, но не подтверждает полномочия.
var diagnosticSystemRevision = regexp.MustCompile(`^system-assistant-(?:core-v[1-9][0-9]*|runtime-[a-f0-9]{64})$`)

// diagnosticUUID matches the lowercase UUID form, supported version nibble and
// RFC variant expected by the diagnostic guard; it does not resolve a session.
// diagnosticUUID проверяет нижний регистр UUID, допустимую версию и RFC-вариант,
// ожидаемые диагностикой; существование сессии при этом не проверяется.
var diagnosticUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[1-8][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

// diagnosticDefinitionVersion recognizes a positive-major three-component
// integration version, not version ordering or compatibility of its package.
// diagnosticDefinitionVersion распознаёт трёхкомпонентную версию интеграции
// с положительной major-частью, но не порядок версий и совместимость пакета.
var diagnosticDefinitionVersion = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+\.[0-9]+$`)

// runtimeEnvelopeReason projects the first recognized envelope violation into a
// closed code without mutating input. It is called after validation rejects;
// UNKNOWN means no diagnostic match, never permission to execute the input.
// runtimeEnvelopeReason переводит первое распознанное нарушение конверта
// в закрытый код без изменения input. Функция вызывается после отказа валидатора;
// UNKNOWN означает отсутствие диагноза, а не разрешение выполнить input.
func runtimeEnvelopeReason(input runtimecontract.RunnerInput) string {
	if input.Schema != runtimecontract.RunnerInputSchemaV9 {
		return "ENVELOPE_SCHEMA"
	}
	if input.Mode != runtimecontract.RunnerModeTurn && input.Mode != runtimecontract.RunnerModeWarm {
		return "ENVELOPE_MODE"
	}
	if input.WorkloadInstance == "" || len(input.WorkloadInstance) > 128 {
		return "ENVELOPE_WORKLOAD"
	}
	if !diagnosticOpaque.MatchString(input.OrganizationRef) {
		return "ENVELOPE_ORGANIZATION_REF"
	}
	if !diagnosticOpaque.MatchString(input.SessionRef) {
		return "ENVELOPE_SESSION_REF"
	}
	if !diagnosticOpaque.MatchString(input.AgentRef) {
		return "ENVELOPE_AGENT_REF"
	}
	if !diagnosticOpaque.MatchString(input.RuntimeRevisionRef) && !diagnosticSystemRevision.MatchString(input.RuntimeRevisionRef) {
		return "ENVELOPE_REVISION_REF"
	}
	if input.RuntimeRevisionVersion < 1 {
		return "ENVELOPE_REVISION_VERSION"
	}
	if !runtimecontract.ValidSHA256Text(input.RuntimeRevisionDigest) {
		return "ENVELOPE_REVISION_DIGEST"
	}
	if !diagnosticPinnedImage(input.ImageReference, input.ImageManifestDigest) {
		return "ENVELOPE_IMAGE"
	}
	if input.RoleRuntimeContractRevision == 0 || !runtimecontract.ValidSHA256Text(input.RoleRuntimeContractSHA256) {
		return "ENVELOPE_ROLE_CONTRACT"
	}
	if !diagnosticOpaque.MatchString(input.RoleDefinitionRef) {
		return "ENVELOPE_ROLE_DEFINITION_REF"
	}
	if !diagnosticProfile.MatchString(input.RuntimeProfileRef) || input.RuntimeProfileRevision == "" || len(input.RuntimeProfileRevision) > 128 {
		return "ENVELOPE_RUNTIME_PROFILE"
	}
	if !diagnosticOpaque.MatchString(input.InstructionRef) || !runtimecontract.ValidSHA256Text(input.InstructionDigest) {
		return "ENVELOPE_INSTRUCTION_BINDING"
	}
	if !diagnosticOpaque.MatchString(input.PromptTemplateRef) || !runtimecontract.ValidSHA256Text(input.PromptTemplateDigest) {
		return "ENVELOPE_PROMPT_TEMPLATE"
	}
	if !runtimecontract.ValidSHA256Text(input.PromptMaterializationDigest) {
		return "ENVELOPE_PROMPT_MATERIALIZATION"
	}
	if strings.TrimSpace(input.Instructions) == "" || len(input.Instructions) > 1<<20 {
		return "ENVELOPE_INSTRUCTIONS"
	}
	if input.Provider == "" || len(input.Provider) > 64 || input.Model == "" || len(input.Model) > 128 {
		return "ENVELOPE_PROVIDER_MODEL"
	}
	if !diagnosticOpaque.MatchString(input.ProviderAccountRef) {
		return "ENVELOPE_PROVIDER_ACCOUNT_REF"
	}
	if !diagnosticOpaque.MatchString(input.ProviderCredentialRef) || input.ProviderCredentialRevision < 1 || !runtimecontract.ValidSHA256Text(input.ProviderCredentialSHA256) {
		return "ENVELOPE_PROVIDER_CREDENTIAL"
	}
	if !diagnosticOpaque.MatchString(input.RuntimeConfigRef) || input.RuntimeConfigVersion < 1 || !runtimecontract.ValidSHA256Text(input.RuntimeConfigDigest) {
		return "ENVELOPE_RUNTIME_CONFIGURATION"
	}
	if !diagnosticOpaque.MatchString(input.ProviderPolicyRef) || input.ProviderPolicyVersion < 1 || !runtimecontract.ValidSHA256Text(input.ProviderPolicyDigest) {
		return "ENVELOPE_PROVIDER_POLICY"
	}
	if !diagnosticOpaque.MatchString(input.ConfigOverlayRef) || input.ConfigOverlayVersion < 1 || !runtimecontract.ValidSHA256Text(input.ConfigOverlayDigest) {
		return "ENVELOPE_CONFIG_OVERLAY"
	}
	if !diagnosticOpaque.MatchString(input.RuntimeEnvironmentRef) || input.RuntimeEnvironmentVersion < 1 || !runtimecontract.ValidSHA256Text(input.RuntimeEnvironmentDigest) {
		return "ENVELOPE_RUNTIME_ENVIRONMENT"
	}
	if !diagnosticOpaque.MatchString(input.EnvironmentBindingRef) || input.EnvironmentBindingVersion < 1 || !runtimecontract.ValidSHA256Text(input.EnvironmentBindingDigest) {
		return "ENVELOPE_ENVIRONMENT_BINDING"
	}
	if input.CodexSandbox != "read-only" && input.CodexSandbox != "workspace-write" {
		return "ENVELOPE_CODEX_SANDBOX"
	}
	if input.CodexApprovalPolicy != "untrusted" && input.CodexApprovalPolicy != "on-request" && input.CodexApprovalPolicy != "never" {
		return "ENVELOPE_CODEX_APPROVAL_POLICY"
	}
	if input.CodexSessionID != "" && !diagnosticUUID.MatchString(input.CodexSessionID) {
		return "ENVELOPE_CODEX_SESSION"
	}
	if input.ValidateCallbackTransport() != nil {
		return "ENVELOPE_CALLBACK_TRANSPORT"
	}
	if !diagnosticSecretFile(input.ExecutionTicketFile) || !diagnosticSecretFile(input.ProviderAuthFile) || !diagnosticSecretFile(input.ProviderAuthSHA256File) {
		return "ENVELOPE_SECRET_PATHS"
	}
	if input.WorkspaceRoot != "/workspace" || input.OutboxRoot != "/workspace/.kodex/outbox" || input.CodexHome != "/workspace/.kodex/state/codex-home" {
		return "ENVELOPE_WORKSPACE_PATHS"
	}
	if len(input.SessionContext) > 128 || len(input.DelegationTargets) > 128 || len(input.IntegrationGrants) > 256 || len(input.AttachmentSets) > 128 || len(input.BoundedInput) > 256 || len(input.CodexSessionID) > 255 {
		return "ENVELOPE_COLLECTION_LIMITS"
	}

	if _, err := runtimecontract.ValidateRuntimeSessionContext(input); err != nil {
		for _, message := range input.SessionContext {
			if message.Role == "USER" && !runtimecontract.ValidAssistantTurnContent(message.Content) {
				return "ENVELOPE_SESSION_USER_CONTENT"
			}
		}
		return "ENVELOPE_SESSION_CONTEXT"
	}
	if reason := diagnosticIntegrationGrants(input.IntegrationGrants); reason != "" {
		return reason
	}
	if len(input.Capabilities) > 256 {
		return "ENVELOPE_CAPABILITIES"
	}
	seen := make(map[string]struct{}, len(input.Capabilities))
	for _, capability := range input.Capabilities {
		if capability == "" || len(capability) > 255 {
			return "ENVELOPE_CAPABILITIES"
		}
		if _, duplicate := seen[capability]; duplicate {
			return "ENVELOPE_CAPABILITIES"
		}
		seen[capability] = struct{}{}
	}
	if runtimecontract.ValidateRuntimeEnvironment(input.EnvironmentValues, input.SecretProjections) != nil {
		return "ENVELOPE_ENVIRONMENT_VALUES"
	}
	if input.EnvironmentImage.Reference != input.ImageReference || input.EnvironmentImage.Digest != input.ImageManifestDigest ||
		input.EnvironmentImage.ArtifactRef == "" || input.EnvironmentImage.RecipeRef == "" || input.EnvironmentImage.RecipeGeneration < 1 {
		return "ENVELOPE_ENVIRONMENT_IMAGE"
	}
	return "UNKNOWN"
}

// diagnosticIntegrationGrants classifies grant shape, approval/risk consistency,
// schema pins and duplicate bindings in validator order. An empty result means
// no diagnostic violation, not an authoritative grant or approved side effect.
// diagnosticIntegrationGrants классифицирует форму grants, согласованность
// подтверждения и риска, привязки схем и дубли в порядке валидатора. Пустой
// результат означает отсутствие диагноза, но не выдачу grant и не одобрение действия.
func diagnosticIntegrationGrants(grants []runtimecontract.RunnerIntegrationGrant) string {
	refs := make(map[string]struct{}, len(grants))
	bindings := make(map[string]struct{}, len(grants))
	for _, grant := range grants {
		if !diagnosticOpaque.MatchString(grant.Ref) || !diagnosticOpaque.MatchString(grant.ConnectionRef) || grant.GrantVersion < 1 || grant.ConnectionVersion < 1 {
			return "ENVELOPE_INTEGRATION_REFS"
		}
		if !diagnosticContains([]string{"NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"}, grant.ApprovalPolicy) ||
			grant.Risk == "READ" && grant.ApprovalPolicy != "NONE" {
			return "ENVELOPE_INTEGRATION_APPROVAL"
		}
		if grant.DefinitionKey == "" || len(grant.DefinitionKey) > 128 ||
			grant.ConnectionName == "" || len(grant.ConnectionName) > 160 ||
			grant.CapabilityKey == "" || len(grant.CapabilityKey) > 255 ||
			grant.CapabilityName == "" || len(grant.CapabilityName) > 160 ||
			len(grant.CapabilityDescription) > 2000 {
			return "ENVELOPE_INTEGRATION_TEXT"
		}
		if !diagnosticDefinitionVersion.MatchString(grant.DefinitionVersion) || !runtimecontract.ValidSHA256Text(grant.DefinitionDigest) ||
			grant.Operation == "" || len(grant.Operation) > 255 || !runtimecontract.ValidSHA256Text(grant.InputSchemaSHA256) {
			return "ENVELOPE_INTEGRATION_PACKAGE"
		}
		if !diagnosticIntegrationSchema(grant.InputSchema, grant.InputSchemaSHA256) {
			return "ENVELOPE_INTEGRATION_SCHEMA"
		}
		if !diagnosticContains([]string{"READ", "WRITE", "SENSITIVE", "DESTRUCTIVE"}, grant.Risk) {
			return "ENVELOPE_INTEGRATION_RISK"
		}
		binding := grant.ConnectionRef + "\x00" + grant.CapabilityKey
		if _, duplicate := refs[grant.Ref]; duplicate {
			return "ENVELOPE_INTEGRATION_DUPLICATE"
		}
		if _, duplicate := bindings[binding]; duplicate {
			return "ENVELOPE_INTEGRATION_DUPLICATE"
		}
		refs[grant.Ref] = struct{}{}
		bindings[binding] = struct{}{}
	}
	return ""
}

// diagnosticIntegrationSchema requires bounded valid object-schema JSON with
// additionalProperties=false and the SHA-256 of the exact raw text. It does not
// canonicalize JSON, compile the schema or authorize the described operation.
// diagnosticIntegrationSchema требует ограниченный корректный JSON схемы объекта
// с additionalProperties=false и SHA-256 точного исходного текста. JSON не
// канонизируется, схема не компилируется и право на операцию не выдаётся.
func diagnosticIntegrationSchema(raw, expectedDigest string) bool {
	if len(raw) == 0 || len(raw) > 64<<10 || !json.Valid([]byte(raw)) {
		return false
	}
	var schema map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&schema) != nil || schema["type"] != "object" || schema["additionalProperties"] != false {
		return false
	}
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:]) == expectedDigest
}

// diagnosticContains performs exact case-sensitive membership in a supplied
// closed diagnostic list; it does not normalize or infer unknown values.
// diagnosticContains проверяет точное регистрозависимое вхождение в переданный
// закрытый список диагностики, не нормализуя и не угадывая неизвестные значения.
func diagnosticContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// diagnosticSecretFile recognizes bounded, clean absolute paths under the two
// allowed secret roots without control delimiters. It does not touch the
// filesystem, resolve symlinks or prove that a secret exists or is readable.
// diagnosticSecretFile распознаёт ограниченные чистые абсолютные пути внутри
// двух разрешённых корней секретов без управляющих разделителей. Файловая система
// не читается, ссылки не разрешаются, наличие и доступность секрета не доказываются.
func diagnosticSecretFile(value string) bool {
	return len(value) <= 512 && !strings.ContainsAny(value, "\x00\r\n") && filepath.IsAbs(value) && filepath.Clean(value) == value &&
		(strings.HasPrefix(value, "/var/run/secrets/") || strings.HasPrefix(value, "/run/secrets/"))
}

// diagnosticPinnedImage checks the bounded reference and exact digest suffix,
// rejecting ASCII spaces, tabs, CR/LF and template delimiters. It does not pull the image,
// verify a signature or replace the platform admission decision.
// diagnosticPinnedImage проверяет длину ссылки и точный digest-суффикс,
// отвергая ASCII-пробел, табуляцию, CR/LF и разделители шаблонов. Образ не скачивается,
// подпись не проверяется и решение платформы о допуске не заменяется.
func diagnosticPinnedImage(reference, digest string) bool {
	return len(reference) >= 73 && len(reference) <= 512 && !strings.ContainsAny(reference, " \t\r\n") &&
		diagnosticImageDigest.MatchString(digest) && strings.HasSuffix(reference, "@"+digest) &&
		!strings.Contains(reference, "$") && !strings.Contains(reference, "{") && !strings.Contains(reference, "}")
}

// knownEnvelopeReason accepts only enumerated envelope diagnostic codes;
// UNKNOWN and caller-supplied lookalikes are deliberately not recognized.
// knownEnvelopeReason принимает только перечисленные диагностические коды
// конверта; UNKNOWN и похожие произвольные строки намеренно не распознаются.
func knownEnvelopeReason(reason string) bool {
	switch reason {
	case "ENVELOPE_SCHEMA",
		"ENVELOPE_MODE",
		"ENVELOPE_WORKLOAD",
		"ENVELOPE_ORGANIZATION_REF",
		"ENVELOPE_SESSION_REF",
		"ENVELOPE_AGENT_REF",
		"ENVELOPE_REVISION_REF",
		"ENVELOPE_REVISION_VERSION",
		"ENVELOPE_REVISION_DIGEST",
		"ENVELOPE_IMAGE",
		"ENVELOPE_ROLE_CONTRACT",
		"ENVELOPE_ROLE_DEFINITION_REF",
		"ENVELOPE_RUNTIME_PROFILE",
		"ENVELOPE_INSTRUCTION_BINDING",
		"ENVELOPE_PROMPT_TEMPLATE",
		"ENVELOPE_PROMPT_MATERIALIZATION",
		"ENVELOPE_INSTRUCTIONS",
		"ENVELOPE_PROVIDER_MODEL",
		"ENVELOPE_PROVIDER_ACCOUNT_REF",
		"ENVELOPE_PROVIDER_CREDENTIAL",
		"ENVELOPE_RUNTIME_CONFIGURATION",
		"ENVELOPE_PROVIDER_POLICY",
		"ENVELOPE_CONFIG_OVERLAY",
		"ENVELOPE_RUNTIME_ENVIRONMENT",
		"ENVELOPE_ENVIRONMENT_BINDING",
		"ENVELOPE_CODEX_SANDBOX",
		"ENVELOPE_CODEX_APPROVAL_POLICY",
		"ENVELOPE_CODEX_SESSION",
		"ENVELOPE_CALLBACK_TRANSPORT",
		"ENVELOPE_SECRET_PATHS",
		"ENVELOPE_WORKSPACE_PATHS",
		"ENVELOPE_COLLECTION_LIMITS",
		"ENVELOPE_SESSION_USER_CONTENT",
		"ENVELOPE_SESSION_CONTEXT",
		"ENVELOPE_INTEGRATION_REFS",
		"ENVELOPE_INTEGRATION_APPROVAL",
		"ENVELOPE_INTEGRATION_TEXT",
		"ENVELOPE_INTEGRATION_PACKAGE",
		"ENVELOPE_INTEGRATION_SCHEMA",
		"ENVELOPE_INTEGRATION_RISK",
		"ENVELOPE_INTEGRATION_DUPLICATE",
		"ENVELOPE_CAPABILITIES",
		"ENVELOPE_ENVIRONMENT_VALUES",
		"ENVELOPE_ENVIRONMENT_IMAGE":
		return true
	default:
		return false
	}
}
