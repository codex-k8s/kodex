package runtimecontract

import (
	"errors"
	"regexp"
)

const (
	// ProviderProcessObservationSchema identifies the closed observation shape,
	// not the version of the provider executable or an admission verdict.
	// ProviderProcessObservationSchema определяет закрытый формат наблюдения,
	// а не версию исполняемого провайдера или решение о допуске.
	ProviderProcessObservationSchema = "kodex.provider-process-observation.v1"
	// ProviderProcessInitializedProgress retains the existing user-facing progress
	// code rather than introducing another execution state machine.
	// ProviderProcessInitializedProgress сохраняет существующий progress code,
	// не вводя дополнительную пользовательскую машину состояний исполнения.
	ProviderProcessInitializedProgress = "MODEL_REQUEST_RUNNING"
)

var (
	// providerProcessVersionPattern allows bounded numeric semver and explicit
	// alpha/beta/rc suffixes without arbitrary build or host metadata.
	// providerProcessVersionPattern допускает ограниченный числовой semver
	// и alpha/beta/rc без произвольных метаданных сборки или хоста.
	providerProcessVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})(-(alpha|beta|rc)\.(0|[1-9][0-9]{0,5}))?$`)
	// errProviderProcessObservation is the safe common error for malformed pins.
	// errProviderProcessObservation — единая безопасная ошибка неверных привязок.
	errProviderProcessObservation = errors.New("provider process observation is invalid")
)

// ProviderProcessObservation binds a diagnostic process handshake to the exact
// execution, image and runtime revision. It is not authority, admission, an image
// inventory or evidence of live workload readiness.
// ProviderProcessObservation связывает диагностический handshake с точным
// исполнением, образом и runtime revision. Это не полномочие, допуск,
// инвентарь образа или доказательство готовности живого workload.
type ProviderProcessObservation struct {
	Schema                 string `json:"schema"`
	Version                string `json:"version"`
	OrganizationRef        string `json:"organization_ref"`
	ProjectRef             string `json:"project_ref"`
	RunRef                 string `json:"run_ref"`
	NodeRef                string `json:"node_ref"`
	SessionRef             string `json:"session_ref"`
	TurnRef                string `json:"turn_ref"`
	Attempt                int32  `json:"attempt"`
	RuntimeRevisionRef     string `json:"runtime_revision_ref"`
	RuntimeRevisionVersion int64  `json:"runtime_revision_version"`
	RuntimeRevisionDigest  string `json:"runtime_revision_digest"`
	ImageReference         string `json:"image_reference"`
	ImageManifestDigest    string `json:"image_manifest_digest"`
	InputDigest            string `json:"input_digest"`
	ExecutionBindingDigest string `json:"execution_binding_digest"`
}

// ValidProviderProcessVersion validates canonical bounded semver without trimming
// or normalizing untrusted input into a valid-looking version.
// ValidProviderProcessVersion проверяет ограниченный канонический semver,
// не превращая недоверенный ввод в допустимую версию обрезкой или нормализацией.
func ValidProviderProcessVersion(version string) bool {
	return len(version) <= 64 && providerProcessVersionPattern.MatchString(version)
}

// BindProviderProcessObservation copies immutable execution pins and the observed
// provider version into a value that must still be validated before consumption.
// BindProviderProcessObservation копирует immutable-привязки и наблюдаемую версию
// в значение, которое ещё требуется проверить перед использованием.
func BindProviderProcessObservation(input RunnerInput, version string) ProviderProcessObservation {
	return ProviderProcessObservation{Schema: ProviderProcessObservationSchema, Version: version,
		OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, RunRef: input.RunRef, NodeRef: input.NodeRef,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt,
		RuntimeRevisionRef: input.RuntimeRevisionRef, RuntimeRevisionVersion: input.RuntimeRevisionVersion, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		ImageReference: input.ImageReference, ImageManifestDigest: input.ImageManifestDigest,
		InputDigest: input.InputDigest, ExecutionBindingDigest: input.ExecutionBindingDigest}
}

// Validate checks the schema, references, positive attempt/revision, digests and
// pinned image consistency without including rejected values in its error.
// Validate проверяет схему, ссылки, положительную попытку/ревизию, дайджесты
// и согласованность pinned образа, не включая отклонённые значения в ошибку.
func (value ProviderProcessObservation) Validate() error {
	if value.Schema != ProviderProcessObservationSchema || !ValidProviderProcessVersion(value.Version) ||
		!opaqueReferencePattern.MatchString(value.OrganizationRef) ||
		(value.ProjectRef != "" && !opaqueReferencePattern.MatchString(value.ProjectRef)) ||
		!opaqueReferencePattern.MatchString(value.RunRef) || !opaqueReferencePattern.MatchString(value.NodeRef) ||
		!opaqueReferencePattern.MatchString(value.SessionRef) || !opaqueReferencePattern.MatchString(value.TurnRef) || value.Attempt < 1 ||
		(!opaqueReferencePattern.MatchString(value.RuntimeRevisionRef) && !systemRuntimeRevisionPattern.MatchString(value.RuntimeRevisionRef)) ||
		value.RuntimeRevisionVersion < 1 || !sha256Pattern.MatchString(value.RuntimeRevisionDigest) ||
		!validPinnedImage(value.ImageReference, value.ImageManifestDigest) ||
		!sha256Pattern.MatchString(value.InputDigest) || !sha256Pattern.MatchString(value.ExecutionBindingDigest) {
		return errProviderProcessObservation
	}
	return nil
}

// Matches requires semantic validity and equality of every execution pin to a
// fresh input binding, keeping the provider version as a diagnostic observation.
// Matches требует корректности и совпадения всех привязок с новым binding входа,
// сохраняя версию провайдера только как диагностическое наблюдение.
func (value ProviderProcessObservation) Matches(input RunnerInput) bool {
	return value.Validate() == nil && value == BindProviderProcessObservation(input, value.Version)
}

// UnmarshalJSON updates the receiver only after the complete canonical typed
// representation passes validation; aliases or missing fields cannot recover a
// partial observation. Any rejection preserves the previous receiver value.
// UnmarshalJSON заменяет получателя только после проверки полного канонического
// значения; иные имена или пропущенные поля не восстанавливают частичное
// наблюдение. Любой отказ сохраняет прежнее значение получателя.
func (value *ProviderProcessObservation) UnmarshalJSON(raw []byte) error {
	// wire preserves the typed layout without inheriting this decoding method.
	// wire сохраняет типизированную структуру, не наследуя этот метод разбора.
	type wire ProviderProcessObservation
	var decoded wire
	if !decodeCanonicalProviderObservation(raw, &decoded, func() bool {
		return ProviderProcessObservation(decoded).Validate() == nil
	}) {
		return errProviderProcessObservation
	}
	*value = ProviderProcessObservation(decoded)
	return nil
}
