package runtimecontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// WorkspaceStorageProfile selects durable employee work without changing runtime quotas.
	// WorkspaceStorageProfile выбирает устойчивый work сотрудника без изменения runtime-квот.
	WorkspaceStorageProfile = "SESSION_WORKSPACE_V2"
	// WorkspaceArchiveSchema identifies private checkpoint metadata, never artifact acceptance.
	// WorkspaceArchiveSchema обозначает private checkpoint metadata, не приёмку артефакта.
	WorkspaceArchiveSchema = "kodex.session-workspace-archive.v2"
	// MaximumWorkspaceArchiveParts bounds one checkpoint's data objects and manifest cardinality.
	// MaximumWorkspaceArchiveParts ограничивает data objects и cardinality manifest одного checkpoint.
	MaximumWorkspaceArchiveParts = 32
	// MaximumWorkspaceArchivePartBytes retains the existing archive source-object ceiling.
	// MaximumWorkspaceArchivePartBytes сохраняет прежний предел исходного archive object.
	MaximumWorkspaceArchivePartBytes int64 = 64 << 20
	// MaximumWorkspaceManifestBytes retains the existing private task/manifest ceiling.
	// MaximumWorkspaceManifestBytes сохраняет прежний предел private task/manifest.
	MaximumWorkspaceManifestBytes = 64 << 10
	// MaximumWorkspaceArchivePathBytes bounds descriptor-relative paths without allowing traversal.
	// MaximumWorkspaceArchivePathBytes ограничивает descriptor-relative paths без traversal.
	MaximumWorkspaceArchivePathBytes = 4096
	// WorkspaceArchiveEntryOverheadBytes bounds canonical PAX headers, path metadata and padding per entry.
	// WorkspaceArchiveEntryOverheadBytes ограничивает canonical PAX headers, path metadata и padding на entry.
	WorkspaceArchiveEntryOverheadBytes int64 = 8192
)

// errWorkspaceStorageInvalid hides rejected private metadata and source object commitments.
// errWorkspaceStorageInvalid скрывает отклонённые private metadata и object commitments.
var errWorkspaceStorageInvalid = errors.New("runtime workspace storage binding is invalid")

// WorkspaceStorageRuntimeContractRevision pins the first complete V2 role ABI; legacy images are not upgraded by opt-in.
// WorkspaceStorageRuntimeContractRevision закрепляет первый полный V2 role ABI; opt-in не повышает legacy image.
const WorkspaceStorageRuntimeContractRevision uint64 = 4

// workspaceStorageRefPattern closes the owner-assigned work identity used in mount subpaths.
// workspaceStorageRefPattern замыкает owner-assigned work identity в mount subpaths.
var workspaceStorageRefPattern = regexp.MustCompile(`^wks_[A-Za-z0-9_-]{8,89}$`)

// workspaceCheckpointRefPattern closes the immutable checkpoint namespace without accepting paths.
// workspaceCheckpointRefPattern замыкает namespace immutable checkpoint без принятия путей.
var workspaceCheckpointRefPattern = regexp.MustCompile(`^wcp_[A-Za-z0-9_-]{8,89}$`)

// WorkspaceStorageScope binds data to its employee session; these references are not read grants.
// WorkspaceStorageScope связывает данные с employee session; ссылки не являются read grants.
type WorkspaceStorageScope struct {
	OrganizationRef string `json:"organization_ref"`
	ProjectRef      string `json:"project_ref"`
	SessionRef      string `json:"session_ref"`
	AgentRef        string `json:"agent_ref"`
}

// Validate rejects absent or malformed scope without inferring authority from equal references.
// Validate отвергает пустой или неверный scope, не выводя authority из равных ссылок.
func (scope WorkspaceStorageScope) Validate() error {
	for prefix, ref := range map[string]string{"org_": scope.OrganizationRef, "prj_": scope.ProjectRef, "ses_": scope.SessionRef, "agt_": scope.AgentRef} {
		if !strings.HasPrefix(ref, prefix) || !opaqueReferencePattern.MatchString(ref) {
			return errWorkspaceStorageInvalid
		}
	}
	return nil
}

// WorkspaceObjectCommitment pins a private immutable object, including a nonempty exact version.
// WorkspaceObjectCommitment закрепляет private immutable object с непустой exact version.
type WorkspaceObjectCommitment struct {
	Key       string `json:"key"`
	Version   string `json:"version"`
	ETag      string `json:"etag"`
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"size_bytes"`
}

// Validate preserves existing key/version/ETag/object ceilings and requires exact digest syntax.
// Validate сохраняет прежние key/version/ETag/object ceilings и требует точную форму digest.
func (value WorkspaceObjectCommitment) Validate(maximum int64) error {
	if len(value.Key) < 1 || len(value.Key) > 1024 || len(value.Version) < 1 || len(value.Version) > 1024 ||
		len(value.ETag) < 1 || len(value.ETag) > 256 || value.SizeBytes < 1 || value.SizeBytes > maximum ||
		!strings.HasPrefix(value.Digest, "sha256:") || !sha256Pattern.MatchString(strings.TrimPrefix(value.Digest, "sha256:")) {
		return errWorkspaceStorageInvalid
	}
	for _, metadata := range []string{value.Key, value.Version, value.ETag} {
		if !utf8.ValidString(metadata) || strings.IndexFunc(metadata, func(char rune) bool { return char < 0x20 || char == 0x7f }) >= 0 {
			return errWorkspaceStorageInvalid
		}
	}
	return nil
}

// WorkspaceCheckpointBinding is an owner-selected prior checkpoint, not caller-selected recovery.
// WorkspaceCheckpointBinding — выбранный owner прежний checkpoint, не caller-selected recovery.
type WorkspaceCheckpointBinding struct {
	Scope         WorkspaceStorageScope     `json:"scope"`
	WorkspaceRef  string                    `json:"workspace_ref"`
	Generation    int64                     `json:"generation"`
	CheckpointRef string                    `json:"checkpoint_ref"`
	Manifest      WorkspaceObjectCommitment `json:"manifest"`
	TreeSHA256    string                    `json:"tree_sha256"`
	Bytes         int64                     `json:"bytes"`
	Entries       int64                     `json:"entries"`
}

// Validate proves canonical private metadata and limits; owner lineage is checked separately in CP.
// Validate доказывает canonical metadata и limits; owner lineage отдельно проверяется в CP.
func (value WorkspaceCheckpointBinding) Validate() error {
	if value.Scope.Validate() != nil || !workspaceStorageRefPattern.MatchString(value.WorkspaceRef) || value.Generation < 1 ||
		!workspaceCheckpointRefPattern.MatchString(value.CheckpointRef) || !sha256Pattern.MatchString(value.TreeSHA256) ||
		value.Bytes < 0 || value.Bytes > RuntimeWorkspaceWritableBytes || value.Entries < 0 || value.Entries > RuntimeWorkspaceMaximumFiles ||
		value.Manifest.Validate(MaximumWorkspaceManifestBytes) != nil || value.Manifest.Key != value.ObjectPrefix()+"manifest.json" {
		return errWorkspaceStorageInvalid
	}
	return nil
}

// ObjectPrefix derives exact object keys from owned scope and generation rather than a supplied path.
// ObjectPrefix выводит exact object keys из owned scope и generation, не из переданного пути.
func (value WorkspaceCheckpointBinding) ObjectPrefix() string {
	return "session-archive/v2/" + value.Scope.OrganizationRef + "/" + value.Scope.ProjectRef + "/" +
		value.Scope.SessionRef + "/" + value.WorkspaceRef + "/g" + strconv.FormatInt(value.Generation, 10) + "/" + value.CheckpointRef + "/"
}

// WorkspaceStorageBinding assigns one isolated writable generation without embedding its own RR digest.
// Its optional source may belong to a predecessor session only after an authoritative retry proof.
// WorkspaceStorageBinding назначает изолированную writable generation без включения собственного RR digest.
// Optional source может относиться к predecessor session лишь после authoritative retry proof.
type WorkspaceStorageBinding struct {
	Profile      string                      `json:"profile"`
	Scope        WorkspaceStorageScope       `json:"scope"`
	WorkspaceRef string                      `json:"workspace_ref"`
	Generation   int64                       `json:"generation"`
	Source       *WorkspaceCheckpointBinding `json:"source,omitempty"`
}

// ValidateForInput verifies employee identity and quota shape, not the right to acquire a writer.
// Source selection and current-writer CAS remain owner transaction responsibilities.
// ValidateForInput проверяет employee identity и quota shape, не право получить writer.
// Выбор source и current-writer CAS остаются ответственностью owner-транзакции.
func (value WorkspaceStorageBinding) ValidateForInput(input RunnerInput) error {
	if value.Profile != WorkspaceStorageProfile || value.Scope.Validate() != nil || value.Generation < 1 ||
		input.RoleRuntimeContractRevision != WorkspaceStorageRuntimeContractRevision ||
		!workspaceStorageRefPattern.MatchString(value.WorkspaceRef) || input.Mode != RunnerModeTurn || input.AssistantScope != AssistantScopeNone ||
		value.Scope.OrganizationRef != input.OrganizationRef || value.Scope.ProjectRef != input.ProjectRef ||
		value.Scope.SessionRef != input.SessionRef || value.Scope.AgentRef != input.AgentRef {
		return errWorkspaceStorageInvalid
	}
	if value.Source != nil && value.Source.Scope.SessionRef == value.Scope.SessionRef &&
		value.Source.WorkspaceRef == value.WorkspaceRef && value.Source.Generation >= value.Generation {
		return errWorkspaceStorageInvalid
	}
	if value.Source != nil && (value.Source.Validate() != nil ||
		value.Source.Scope.OrganizationRef != value.Scope.OrganizationRef || value.Source.Scope.ProjectRef != value.Scope.ProjectRef ||
		value.Source.Scope.AgentRef != value.Scope.AgentRef || value.Source.Bytes > input.WorkspacePolicy.MaximumWritableBytes ||
		value.Source.Entries > input.WorkspacePolicy.MaximumFileCount) {
		return errWorkspaceStorageInvalid
	}
	return nil
}

// GenerationSubPath returns only the current tree; parent and older generations never become role mounts.
// GenerationSubPath возвращает только current tree; parent и старые generations не становятся role mounts.
func (value WorkspaceStorageBinding) GenerationSubPath() (string, error) {
	if value.Profile != WorkspaceStorageProfile || value.Scope.Validate() != nil ||
		!workspaceStorageRefPattern.MatchString(value.WorkspaceRef) || value.Generation < 1 {
		return "", errWorkspaceStorageInvalid
	}
	return "workspaces/" + value.WorkspaceRef + "/g" + strconv.FormatInt(value.Generation, 10) + "/tree", nil
}

// WorkspaceWriterCommitment identifies a stopped execution without storing its secret lease fence.
// JoinSHA256 names the exact trusted quiescence record; syntactic validity does not prove that join.
// WorkspaceWriterCommitment обозначает остановленное исполнение без secret lease fence.
// JoinSHA256 обозначает trusted quiescence record; корректная форма не доказывает join.
type WorkspaceWriterCommitment struct {
	RunRef                 string `json:"run_ref"`
	NodeRef                string `json:"node_ref"`
	TurnRef                string `json:"turn_ref"`
	RuntimeRevisionRef     string `json:"runtime_revision_ref"`
	RuntimeRevisionVersion int64  `json:"runtime_revision_version"`
	RuntimeRevisionDigest  string `json:"runtime_revision_digest"`
	ExecutionBindingDigest string `json:"execution_binding_digest"`
	InputDigest            string `json:"input_digest"`
	LeaseRef               string `json:"lease_ref"`
	LeaseGeneration        int64  `json:"lease_generation"`
	Attempt                int32  `json:"attempt"`
	PodUID                 string `json:"pod_uid"`
	PVCUID                 string `json:"pvc_uid"`
	JoinSHA256             string `json:"join_sha256"`
}

// Validate rejects incomplete commitments; exact Pod/PVC ownership is proved by the controller.
// Validate отклоняет неполные commitments; exact Pod/PVC ownership доказывает controller.
func (value WorkspaceWriterCommitment) Validate() error {
	return validateWorkspaceWriter(value, true)
}

// validateWorkspaceWriter shares tuple validation while rejecting recursive join hashes in a source record.
// validateWorkspaceWriter разделяет tuple validation, отвергая recursive join hash в source record.
func validateWorkspaceWriter(value WorkspaceWriterCommitment, requireJoin bool) error {
	for _, ref := range []string{value.RunRef, value.NodeRef, value.TurnRef, value.RuntimeRevisionRef, value.LeaseRef} {
		if !opaqueReferencePattern.MatchString(ref) {
			return errWorkspaceStorageInvalid
		}
	}
	for _, digest := range []string{value.RuntimeRevisionDigest, value.ExecutionBindingDigest, value.InputDigest} {
		if !sha256Pattern.MatchString(digest) {
			return errWorkspaceStorageInvalid
		}
	}
	if requireJoin && !sha256Pattern.MatchString(value.JoinSHA256) || !requireJoin && value.JoinSHA256 != "" {
		return errWorkspaceStorageInvalid
	}
	if value.LeaseGeneration < 1 || value.RuntimeRevisionVersion < 1 || value.Attempt < 1 || !workspaceUID(value.PodUID) || !workspaceUID(value.PVCUID) {
		return errWorkspaceStorageInvalid
	}
	return nil
}

// Matches compares every current execution commitment while leaving Pod observation to the controller.
// Matches сравнивает все current execution commitments, оставляя Pod observation controller.
func (value WorkspaceWriterCommitment) Matches(input RunnerInput) bool {
	return value.Validate() == nil && workspaceWriterTupleMatches(value, input)
}

// workspaceWriterTupleMatches compares identity only; callers separately require pre- or post-join shape.
// This helper never validates a record, observes a process or grants permission to write.
// workspaceWriterTupleMatches сравнивает только identity; caller отдельно требует pre-/post-join shape.
// Helper не валидирует record, не наблюдает процесс и не разрешает запись.
func workspaceWriterTupleMatches(value WorkspaceWriterCommitment, input RunnerInput) bool {
	return input.Mode == RunnerModeTurn && value.RunRef == input.RunRef &&
		value.NodeRef == input.NodeRef && value.TurnRef == input.TurnRef && value.RuntimeRevisionRef == input.RuntimeRevisionRef &&
		value.RuntimeRevisionVersion == input.RuntimeRevisionVersion && value.RuntimeRevisionDigest == input.RuntimeRevisionDigest &&
		value.ExecutionBindingDigest == input.ExecutionBindingDigest && value.InputDigest == input.InputDigest &&
		value.LeaseRef == input.LeaseRef && value.LeaseGeneration == input.LeaseGeneration && value.Attempt == input.Attempt
}

// workspaceUID permits bounded Kubernetes UID metadata and excludes paths or control characters.
// workspaceUID допускает bounded Kubernetes UID metadata, исключая пути и control characters.
func workspaceUID(value string) bool {
	if len(value) < 1 || len(value) > 128 || value[0] == '-' {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
			return false
		}
	}
	return true
}

// WorkspaceArchivePart omits an arbitrary key: index derives it inside the checkpoint namespace.
// WorkspaceArchivePart исключает произвольный key: index выводит его внутри checkpoint namespace.
type WorkspaceArchivePart struct {
	Index     int32  `json:"index"`
	Version   string `json:"version"`
	ETag      string `json:"etag"`
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"size_bytes"`
}

// WorkspaceArchiveManifest is a closed root receipt for a bounded private work stream.
// It contains no provider auth, free text, caller path or business acceptance decision.
// WorkspaceArchiveManifest — закрытый root receipt bounded private work stream.
// Он не содержит provider auth, свободный текст, caller path или business acceptance.
type WorkspaceArchiveManifest struct {
	Schema        string                    `json:"schema"`
	Scope         WorkspaceStorageScope     `json:"scope"`
	WorkspaceRef  string                    `json:"workspace_ref"`
	Generation    int64                     `json:"generation"`
	CheckpointRef string                    `json:"checkpoint_ref"`
	Writer        WorkspaceWriterCommitment `json:"writer"`
	TreeSHA256    string                    `json:"tree_sha256"`
	Bytes         int64                     `json:"bytes"`
	Entries       int64                     `json:"entries"`
	StreamBytes   int64                     `json:"stream_bytes"`
	Parts         []WorkspaceArchivePart    `json:"parts"`
}

// Validate checks all parts and aggregate limits without reading or trusting source contents.
// Validate проверяет все parts и aggregate limits без чтения или доверия source contents.
func (value WorkspaceArchiveManifest) Validate() error {
	if value.Schema != WorkspaceArchiveSchema || value.Scope.Validate() != nil || value.Writer.Validate() != nil ||
		!workspaceStorageRefPattern.MatchString(value.WorkspaceRef) || !workspaceCheckpointRefPattern.MatchString(value.CheckpointRef) ||
		value.Generation < 1 || !sha256Pattern.MatchString(value.TreeSHA256) || value.Bytes < 0 || value.Bytes > RuntimeWorkspaceWritableBytes ||
		value.Entries < 0 || value.Entries > RuntimeWorkspaceMaximumFiles || len(value.Parts) < 1 || len(value.Parts) > MaximumWorkspaceArchiveParts {
		return errWorkspaceStorageInvalid
	}
	var total int64
	for index, part := range value.Parts {
		commitment := WorkspaceObjectCommitment{Key: "derived", Version: part.Version, ETag: part.ETag, Digest: part.Digest, SizeBytes: part.SizeBytes}
		if part.Index != int32(index+1) || commitment.Validate(MaximumWorkspaceArchivePartBytes) != nil ||
			index < len(value.Parts)-1 && part.SizeBytes != MaximumWorkspaceArchivePartBytes {
			return errWorkspaceStorageInvalid
		}
		total += part.SizeBytes
	}
	maximum, err := MaximumWorkspaceStreamBytes(value.Bytes, value.Entries)
	if err != nil || value.StreamBytes != total || total < value.Bytes+512*value.Entries+1024 ||
		value.Bytes > 0 && value.Entries == 0 || total > maximum ||
		total > int64(MaximumWorkspaceArchiveParts)*MaximumWorkspaceArchivePartBytes {
		return errWorkspaceStorageInvalid
	}
	return nil
}

// MaximumWorkspaceStreamBytes derives a fixed overflow-safe ceiling from existing data/file quotas.
// The canonical encoder must reject every entry exceeding the path or per-entry metadata bound.
// MaximumWorkspaceStreamBytes выводит fixed overflow-safe ceiling из прежних data/file quotas.
// Canonical encoder обязан отвергать entry сверх path или per-entry metadata bound.
func MaximumWorkspaceStreamBytes(bytes, entries int64) (int64, error) {
	if bytes < 0 || bytes > RuntimeWorkspaceWritableBytes || entries < 0 || entries > RuntimeWorkspaceMaximumFiles || bytes > 0 && entries == 0 {
		return 0, errWorkspaceStorageInvalid
	}
	return bytes + entries*WorkspaceArchiveEntryOverheadBytes + 1024, nil
}

// EncodeWorkspaceArchiveManifest enforces the unchanged 64KiB boundary before object publication.
// EncodeWorkspaceArchiveManifest соблюдает прежнюю границу64KiB до object publication.
func EncodeWorkspaceArchiveManifest(value WorkspaceArchiveManifest) ([]byte, error) {
	if value.Validate() != nil {
		return nil, errWorkspaceStorageInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumWorkspaceManifestBytes {
		return nil, errWorkspaceStorageInvalid
	}
	return raw, nil
}

// DecodeWorkspaceArchiveManifest rejects duplicate/unknown keys, excess depth, trailing data and noncanonical bytes.
// DecodeWorkspaceArchiveManifest отвергает duplicate/unknown keys, excess depth, trailing data и noncanonical bytes.
func DecodeWorkspaceArchiveManifest(raw []byte) (WorkspaceArchiveManifest, error) {
	var value WorkspaceArchiveManifest
	if len(raw) < 1 || len(raw) > MaximumWorkspaceManifestBytes ||
		boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 5) != nil {
		return value, errWorkspaceStorageInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.Validate() != nil {
		return WorkspaceArchiveManifest{}, errWorkspaceStorageInvalid
	}
	canonical, err := EncodeWorkspaceArchiveManifest(value)
	if err != nil || !bytes.Equal(canonical, raw) {
		return WorkspaceArchiveManifest{}, errWorkspaceStorageInvalid
	}
	return value, nil
}
