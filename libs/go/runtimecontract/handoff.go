// Package runtimecontract defines canonical wire DTOs between agent-runner and runtime-controller;
// transport and domain policy remain outside this package.
// Package runtimecontract задаёт канонические wire DTO между agent-runner и runtime-controller;
// транспорт и доменная политика находятся за границей пакета.
package runtimecontract

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// HandoffSchemaV1 identifies the single-result terminal handoff payload.
	// HandoffSchemaV1 обозначает завершающую передачу с одним результатом.
	HandoffSchemaV1 = "kodex.runtime-turn-handoff.v1"
	// HandoffSchemaV2 identifies the terminal handoff with ordered multi-kind outputs.
	// HandoffSchemaV2 обозначает завершающую передачу с упорядоченными результатами разных видов.
	HandoffSchemaV2 = "kodex.runtime-turn-handoff.v2"
	// SignedHandoffSchemaV1 identifies the detached signature envelope around a handoff payload.
	// SignedHandoffSchemaV1 обозначает конверт отделённой подписи данных завершающей передачи.
	SignedHandoffSchemaV1 = "kodex.signed-runtime-turn-handoff.v1"
	// MaximumOutputs limits the number of deliverable descriptors in one handoff.
	// MaximumOutputs ограничивает количество описаний результатов одной передачи.
	MaximumOutputs = 32
	// MaximumOutputBytes bounds individual and aggregate inline output payloads.
	// MaximumOutputBytes ограничивает отдельный и суммарный объём встроенных результатов.
	MaximumOutputBytes = 512 << 10
	// MaximumHandoffBytes bounds both the signed payload and its serialized envelope.
	// MaximumHandoffBytes ограничивает подписываемые данные и сериализованный конверт.
	MaximumHandoffBytes = 900 << 10
)

// sha256Pattern requires canonical unprefixed lowercase SHA-256 text for runtime bindings.
// sha256Pattern требует каноническую строчную запись SHA-256 без префикса для привязок runtime.
var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// uuidPattern accepts canonical lowercase UUIDs with supported version and variant bits.
// uuidPattern допускает канонические строчные UUID с поддерживаемыми версией и вариантом.
var uuidPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[1-8][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

// HandoffV1 binds one terminal Markdown result to exact execution, revision, input, and session pins.
// HandoffV1 связывает один завершающий Markdown-результат с точными исполнением, версией, входом и сессией.
type HandoffV1 struct {
	Schema                  string    `json:"schema"`
	ExecutionID             string    `json:"execution_id"`
	ExecutionVersion        uint64    `json:"execution_version"`
	Fence                   uint64    `json:"fence"`
	GrantGeneration         uint64    `json:"grant_generation"`
	RuntimeRevisionSHA256   string    `json:"runtime_revision_sha256"`
	EffectiveRuntimeSHA256  string    `json:"effective_runtime_sha256"`
	ImmutableInputSHA256    string    `json:"immutable_input_sha256"`
	AgentSessionID          int64     `json:"agent_session_id"`
	AgentSessionTurnID      int64     `json:"agent_session_turn_id"`
	AgentRunID              string    `json:"agent_run_id"`
	AgentBindingSHA256      string    `json:"agent_binding_sha256"`
	Outcome                 string    `json:"outcome"`
	TerminalReference       string    `json:"terminal_reference"`
	TerminalSHA256          string    `json:"terminal_sha256"`
	ResultArtifactID        string    `json:"result_artifact_id"`
	ResultArtifactVersion   uint64    `json:"result_artifact_version"`
	ResultArtifactSHA256    string    `json:"result_artifact_sha256"`
	ResultArtifactName      string    `json:"result_artifact_name"`
	ResultArtifactMediaType string    `json:"result_artifact_media_type"`
	ResultArtifactPayload   []byte    `json:"result_artifact_payload"`
	ObservedAt              time.Time `json:"observed_at"`
}

// Validate checks required execution/session fields and the bounded Markdown payload against its digest.
// Validate проверяет обязательные поля исполнения и сессии и ограниченный Markdown по его дайджесту.
func (handoff HandoffV1) Validate() error {
	resultDigest := sha256.Sum256(handoff.ResultArtifactPayload)
	if handoff.Schema != HandoffSchemaV1 || handoff.ExecutionID == "" ||
		handoff.ExecutionVersion == 0 || handoff.Fence == 0 || handoff.GrantGeneration == 0 ||
		!sha256Pattern.MatchString(handoff.RuntimeRevisionSHA256) ||
		!sha256Pattern.MatchString(handoff.EffectiveRuntimeSHA256) ||
		!sha256Pattern.MatchString(handoff.ImmutableInputSHA256) ||
		handoff.AgentSessionID <= 0 || handoff.AgentSessionTurnID <= 0 || handoff.AgentRunID == "" ||
		!sha256Pattern.MatchString(handoff.AgentBindingSHA256) || handoff.Outcome == "" ||
		handoff.TerminalReference == "" || !sha256Pattern.MatchString(handoff.TerminalSHA256) ||
		handoff.ResultArtifactID == "" || handoff.ResultArtifactVersion == 0 ||
		!sha256Pattern.MatchString(handoff.ResultArtifactSHA256) ||
		handoff.ResultArtifactSHA256 != hex.EncodeToString(resultDigest[:]) || handoff.ResultArtifactName == "" ||
		handoff.ResultArtifactMediaType != "text/markdown" || len(handoff.ResultArtifactPayload) == 0 ||
		len(handoff.ResultArtifactPayload) > 160<<10 ||
		handoff.ObservedAt.IsZero() {
		return errors.New("runtime handoff is invalid")
	}
	return nil
}

// EncodeHandoff serializes a single-result handoff only after structural and payload validation.
// EncodeHandoff сериализует передачу одного результата после проверки структуры и содержимого.
func EncodeHandoff(handoff HandoffV1) ([]byte, error) {
	if err := handoff.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(handoff)
}

// DecodeHandoff rejects unknown fields and trailing JSON before validating a single-result payload.
// DecodeHandoff отклоняет неизвестные поля и лишний JSON до проверки данных одного результата.
func DecodeHandoff(raw []byte) (HandoffV1, error) {
	var handoff HandoffV1
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&handoff); err != nil {
		return HandoffV1{}, errors.New("decode runtime handoff")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return HandoffV1{}, errors.New("runtime handoff has trailing data")
	}
	if err := handoff.Validate(); err != nil {
		return HandoffV1{}, err
	}
	return handoff, nil
}

// OutputV2 describes one owner-deliverable Markdown, file, or image result, with either
// a bounded inline payload or immutable storage reference and exact digest/sequence metadata.
// OutputV2 описывает Markdown, файл или изображение для владельца со встроенными ограниченными
// данными либо неизменной ссылкой хранилища и точными дайджестом и порядковыми полями.
type OutputV2 struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Version    uint64 `json:"version"`
	SHA256     string `json:"sha256"`
	Name       string `json:"name"`
	MediaType  string `json:"media_type"`
	Payload    []byte `json:"payload"`
	StorageRef string `json:"storage_ref,omitempty"`
	SizeBytes  uint64 `json:"size_bytes"`
	Sequence   uint32 `json:"sequence"`
	Total      uint32 `json:"total"`
}

// validate checks closed output kinds, safe names, size/sequence bounds, and inline digest
// or storage-reference shape, including Markdown encoding and image media type.
// validate проверяет закрытые виды, безопасные имена, размер, порядок и дайджест встроенных данных
// либо форму ссылки хранилища, включая кодировку Markdown и тип изображения.
func (output OutputV2) validate() error {
	if output.Kind != "FINAL_MARKDOWN" && output.Kind != "FILE" && output.Kind != "IMAGE" {
		return errors.New("runtime output kind is invalid")
	}
	if output.ID == "" || output.Version == 0 || output.Name == "" || len(output.Name) > 255 ||
		strings.ContainsAny(output.Name, "/\\\x00\r\n") || output.MediaType == "" || len(output.MediaType) > 255 ||
		!sha256Pattern.MatchString(output.SHA256) || output.SizeBytes == 0 || output.SizeBytes > 256<<20 ||
		output.Sequence == 0 || output.Total == 0 || output.Sequence > output.Total {
		return errors.New("runtime output is invalid")
	}
	if len(output.Payload) != 0 {
		digest := sha256.Sum256(output.Payload)
		if output.StorageRef != "" || output.SizeBytes != uint64(len(output.Payload)) ||
			len(output.Payload) > MaximumOutputBytes || output.SHA256 != hex.EncodeToString(digest[:]) {
			return errors.New("runtime inline output is invalid")
		}
	} else if !strings.HasPrefix(output.StorageRef, "s3://") || len(output.StorageRef) > 2048 ||
		strings.ContainsAny(output.StorageRef, "\x00\r\n") {
		return errors.New("runtime referenced output is invalid")
	}
	if output.Kind == "FINAL_MARKDOWN" &&
		(output.MediaType != "text/markdown" || len(output.Payload) != 0 && !utf8.Valid(output.Payload)) {
		return errors.New("runtime markdown output is invalid")
	}
	if output.Kind == "IMAGE" && !strings.HasPrefix(output.MediaType, "image/") {
		return errors.New("runtime image output is invalid")
	}
	return nil
}

// HandoffV2 binds terminal outputs and archive provenance to an exact execution attempt,
// runtime/input revisions, session, and provider binding, with an optional scheduled outcome.
// HandoffV2 связывает результаты и происхождение архива с точной попыткой исполнения,
// версиями runtime и входа, сессией и провайдером, включая необязательный исход расписания.
type HandoffV2 struct {
	Schema                 string     `json:"schema"`
	ExecutionID            string     `json:"execution_id"`
	ExecutionVersion       uint64     `json:"execution_version"`
	Fence                  uint64     `json:"fence"`
	GrantGeneration        uint64     `json:"grant_generation"`
	RuntimeRevisionSHA256  string     `json:"runtime_revision_sha256"`
	EffectiveRuntimeSHA256 string     `json:"effective_runtime_sha256"`
	ImmutableInputSHA256   string     `json:"immutable_input_sha256"`
	SessionID              string     `json:"session_id"`
	TurnID                 string     `json:"turn_id"`
	ScheduleOccurrenceID   string     `json:"schedule_occurrence_id,omitempty"`
	Attempt                uint32     `json:"attempt"`
	ProviderBindingID      string     `json:"provider_binding_id"`
	ProviderBindingVersion uint64     `json:"provider_binding_version"`
	ProviderBindingSHA256  string     `json:"provider_binding_sha256"`
	Outcome                string     `json:"outcome"`
	ScheduledOutcome       string     `json:"scheduled_outcome,omitempty"`
	TerminalReference      string     `json:"terminal_reference"`
	TerminalSHA256         string     `json:"terminal_sha256"`
	Outputs                []OutputV2 `json:"outputs"`
	CodexSessionID         string     `json:"codex_session_id"`
	ArchiveRelativePath    string     `json:"archive_relative_path"`
	ArchiveSHA256          string     `json:"archive_sha256"`
	ArchiveProvenance      string     `json:"archive_provenance"`
	ObservedAt             time.Time  `json:"observed_at"`
}

// Validate checks execution/archive pins, compatible scheduled outcomes, unique complete
// output sequences by kind, and the aggregate inline byte budget.
// Validate проверяет привязки исполнения и архива, совместимость исхода расписания, уникальные
// полные последовательности результатов каждого вида и общий объём встроенных данных.
func (handoff HandoffV2) Validate() error {
	if handoff.Schema != HandoffSchemaV2 || handoff.ExecutionID == "" || handoff.ExecutionVersion == 0 ||
		handoff.Fence == 0 || handoff.GrantGeneration == 0 ||
		!sha256Pattern.MatchString(handoff.RuntimeRevisionSHA256) ||
		!sha256Pattern.MatchString(handoff.EffectiveRuntimeSHA256) ||
		!sha256Pattern.MatchString(handoff.ImmutableInputSHA256) ||
		handoff.SessionID == "" || handoff.TurnID == "" || handoff.Attempt == 0 ||
		handoff.ProviderBindingID == "" || handoff.ProviderBindingVersion == 0 ||
		!sha256Pattern.MatchString(handoff.ProviderBindingSHA256) ||
		(handoff.Outcome != "SUCCEEDED" && handoff.Outcome != "FAILED" && handoff.Outcome != "BLOCKED") ||
		handoff.TerminalReference == "" || len(handoff.TerminalReference) > 1024 ||
		!sha256Pattern.MatchString(handoff.TerminalSHA256) || len(handoff.Outputs) == 0 ||
		len(handoff.Outputs) > MaximumOutputs || !validCodexTerminalBinding(handoff) ||
		len(handoff.ArchiveProvenance) > 1024 || handoff.ObservedAt.IsZero() {
		return errors.New("runtime handoff is invalid")
	}
	if (handoff.ScheduleOccurrenceID == "") != (handoff.ScheduledOutcome == "") ||
		(handoff.ScheduleOccurrenceID != "" && (!uuidPattern.MatchString(handoff.ScheduleOccurrenceID) ||
			!validScheduledOutcome(handoff.ScheduledOutcome) ||
			!scheduledOutcomeMatchesRuntime(handoff.ScheduledOutcome, handoff.Outcome))) {
		return errors.New("runtime handoff scheduled outcome is invalid")
	}
	seen := make(map[string]struct{}, len(handoff.Outputs))
	kindTotals := make(map[string]uint32, 3)
	kindCounts := make(map[string]uint32, 3)
	totalBytes := 0
	for _, output := range handoff.Outputs {
		if output.validate() != nil {
			return errors.New("runtime handoff output is invalid")
		}
		key := output.Kind + ":" + output.ID
		if _, duplicate := seen[key]; duplicate {
			return errors.New("runtime handoff output is duplicated")
		}
		seen[key] = struct{}{}
		sequenceKey := output.Kind + ":sequence:" + strconv.FormatUint(uint64(output.Sequence), 10)
		if _, duplicate := seen[sequenceKey]; duplicate ||
			(kindTotals[output.Kind] != 0 && kindTotals[output.Kind] != output.Total) {
			return errors.New("runtime handoff output sequence is invalid")
		}
		seen[sequenceKey] = struct{}{}
		kindTotals[output.Kind], kindCounts[output.Kind] = output.Total, kindCounts[output.Kind]+1
		totalBytes += len(output.Payload)
	}
	for kind, total := range kindTotals {
		if kindCounts[kind] != total {
			return errors.New("runtime handoff output sequence is incomplete")
		}
	}
	if totalBytes > MaximumOutputBytes {
		return errors.New("runtime handoff output budget exceeded")
	}
	return nil
}

// validScheduledOutcome closes the set of scheduler-visible business outcomes.
// validScheduledOutcome задаёт закрытый набор бизнес-исходов, видимых планировщику.
func validScheduledOutcome(value string) bool {
	return value == "no_action" || value == "action_taken" ||
		value == "requires_human" || value == "failed"
}

// scheduledOutcomeMatchesRuntime permits a failed scheduled result only for failed/blocked
// runtime outcomes and requires successful runtime completion for all other scheduled results.
// scheduledOutcomeMatchesRuntime допускает ошибку расписания лишь при FAILED или BLOCKED runtime,
// а для остальных исходов расписания требует успешного исполнения.
func scheduledOutcomeMatchesRuntime(scheduled, runtime string) bool {
	if scheduled == "failed" {
		return runtime == "FAILED" || runtime == "BLOCKED"
	}
	return runtime == "SUCCEEDED"
}

// validCodexTerminalBinding requires exact session/archive provenance except for a blocked preflight
// that has no provider archive at all.
// validCodexTerminalBinding требует точное происхождение сессии и архива, кроме блокировки preflight,
// при которой архив провайдера полностью отсутствует.
func validCodexTerminalBinding(handoff HandoffV2) bool {
	if handoff.CodexSessionID == "" && handoff.ArchiveRelativePath == "" && handoff.ArchiveSHA256 == "" && handoff.ArchiveProvenance == "" {
		return handoff.Outcome == "BLOCKED" && strings.HasPrefix(handoff.TerminalReference, "preflight://")
	}
	return ValidateCodexArchiveIdentity(handoff.CodexSessionID, handoff.ArchiveRelativePath) == nil &&
		sha256Pattern.MatchString(handoff.ArchiveSHA256) && handoff.ArchiveProvenance != "" &&
		validCodexArchiveProvenance(handoff.ArchiveProvenance, handoff.ArchiveRelativePath, handoff.ArchiveSHA256)
}

// validCodexArchiveProvenance binds the canonical rollout marker to source execution UUID, path, and digest.
// validCodexArchiveProvenance связывает канонический признак rollout с UUID исходного исполнения, путём и дайджестом.
func validCodexArchiveProvenance(value, path, digest string) bool {
	// prefix identifies the supported archive provenance serialization.
	// prefix обозначает поддерживаемую сериализацию происхождения архива.
	const prefix = "codex-app-server-rollout-v1:"
	suffix := ":" + path + ":" + digest
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return false
	}
	sourceExecutionID := strings.TrimSuffix(strings.TrimPrefix(value, prefix), suffix)
	return uuidPattern.MatchString(sourceExecutionID)
}

// validArchiveRelativePath accepts only the bounded dated rollout path without backslashes or traversal.
// validArchiveRelativePath допускает лишь ограниченный датированный путь rollout без обратных слешей и traversal.
func validArchiveRelativePath(value string) bool {
	return regexp.MustCompile(`^\.kodex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[A-Za-z0-9._-]+\.jsonl$`).MatchString(value) &&
		len(value) <= 255 &&
		!strings.Contains(value, "\\") && !strings.Contains(value, "..")
}

// ValidateCodexArchiveIdentity requires a canonical Codex rollout path ending in the exact session UUID;
// the preceding filename portion may include the provider's timestamp.
// ValidateCodexArchiveIdentity требует канонический путь Codex rollout, заканчивающийся точным UUID сессии;
// предшествующая часть имени может включать временную метку провайдера.
func ValidateCodexArchiveIdentity(sessionID, relativePath string) error {
	if !uuidPattern.MatchString(sessionID) || !validArchiveRelativePath(relativePath) ||
		!strings.HasSuffix(relativePath, "-"+sessionID+".jsonl") {
		return errors.New("Codex archive identity is invalid")
	}
	return nil
}

// SignedHandoffV1 carries a detached payload signature; the controller resolves KeyID only
// against its own explicit trusted key set.
// SignedHandoffV1 содержит отделённую подпись данных; контроллер разрешает KeyID только
// по собственному явному набору доверенных ключей.
type SignedHandoffV1 struct {
	Schema    string          `json:"schema"`
	KeyID     string          `json:"key_id"`
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

// SignHandoffV2 validates the handoff and key inputs, signs exact JSON payload bytes with Ed25519,
// and enforces the complete serialized envelope budget.
// SignHandoffV2 проверяет передачу и ключ, подписывает точные байты JSON через Ed25519
// и ограничивает размер всего сериализованного конверта.
func SignHandoffV2(handoff HandoffV2, keyID string, privateKey ed25519.PrivateKey) ([]byte, error) {
	if handoff.Validate() != nil || keyID == "" || len(keyID) > 128 || strings.ContainsAny(keyID, "\x00\r\n") ||
		len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("runtime handoff signing input is invalid")
	}
	payload, err := json.Marshal(handoff)
	if err != nil || len(payload) > MaximumHandoffBytes {
		return nil, errors.New("encode runtime handoff payload")
	}
	envelope := SignedHandoffV1{Schema: SignedHandoffSchemaV1, KeyID: keyID,
		Payload:   payload,
		Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, payload))}
	raw, err := json.Marshal(envelope)
	if err != nil || len(raw) > MaximumHandoffBytes {
		return nil, errors.New("encode signed runtime handoff")
	}
	return raw, nil
}

// DecodeSignedHandoffV2 bounds and decodes the envelope, verifies its payload against the controller's
// trusted key, then validates the typed handoff while rejecting unknown fields and trailing values.
// DecodeSignedHandoffV2 ограничивает и разбирает конверт, проверяет подпись доверенным ключом контроллера,
// затем проверяет типизированную передачу, отклоняя неизвестные поля и лишние значения.
func DecodeSignedHandoffV2(raw []byte, trustedKeys map[string]ed25519.PublicKey) (HandoffV2, error) {
	if len(raw) == 0 || len(raw) > MaximumHandoffBytes {
		return HandoffV2{}, errors.New("signed runtime handoff size is invalid")
	}
	var envelope SignedHandoffV1
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		envelope.Schema != SignedHandoffSchemaV1 || envelope.KeyID == "" {
		return HandoffV2{}, errors.New("decode signed runtime handoff")
	}
	publicKey := trustedKeys[envelope.KeyID]
	payload := []byte(envelope.Payload)
	signature, signatureErr := base64.RawURLEncoding.DecodeString(envelope.Signature)
	if len(payload) == 0 || signatureErr != nil || len(payload) > MaximumHandoffBytes ||
		len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(publicKey, payload, signature) {
		return HandoffV2{}, errors.New("verify signed runtime handoff")
	}
	var handoff HandoffV2
	payloadDecoder := json.NewDecoder(bytes.NewReader(payload))
	payloadDecoder.DisallowUnknownFields()
	if payloadDecoder.Decode(&handoff) != nil || payloadDecoder.Decode(&struct{}{}) != io.EOF ||
		handoff.Validate() != nil {
		return HandoffV2{}, errors.New("decode runtime handoff payload")
	}
	handoff.Outputs = slices.Clone(handoff.Outputs)
	return handoff, nil
}
