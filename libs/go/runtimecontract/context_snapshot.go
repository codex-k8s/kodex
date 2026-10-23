package runtimecontract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// RuntimeContextSchema identifies the owner-materialized skills and memory snapshot.
	// RuntimeContextSchema обозначает материализованный владельцем снимок навыков и памяти.
	RuntimeContextSchema = "kodex.runtime-context.v1"
	// RuntimeContextRoot is the protected root used for admitted context files.
	// RuntimeContextRoot задаёт защищённый корень допущенных файлов контекста.
	RuntimeContextRoot = "/workspace/context"
	// MaximumContextSkills limits distinct skill bundles in one runtime revision.
	// MaximumContextSkills ограничивает количество различных навыков одной версии runtime.
	MaximumContextSkills = 32
	// MaximumContextMemories limits memory records delivered with one snapshot.
	// MaximumContextMemories ограничивает количество записей памяти одного снимка.
	MaximumContextMemories = 64
	// MaximumSkillFiles limits the number of files in each admitted skill bundle.
	// MaximumSkillFiles ограничивает количество файлов каждого допущенного навыка.
	MaximumSkillFiles = 128
	// MaximumSkillFileBytes bounds one immutable skill file before materialization.
	// MaximumSkillFileBytes ограничивает размер неизменяемого файла навыка до материализации.
	MaximumSkillFileBytes int64 = 32 << 20
	// MaximumSkillBundleBytes bounds the sum of file sizes within one skill bundle.
	// MaximumSkillBundleBytes ограничивает суммарный размер файлов одного навыка.
	MaximumSkillBundleBytes int64 = 64 << 20
	// MaximumContextBytes bounds all skill bytes and memory summaries in a snapshot.
	// MaximumContextBytes ограничивает общий объём файлов навыков и выдержек памяти в снимке.
	MaximumContextBytes int64 = 512 << 20
)

// ErrRuntimeContext is the safe rejection for missing, expired, or incorrectly bound context.
// ErrRuntimeContext обозначает безопасный отказ при отсутствии, истечении или неверной привязке контекста.
var ErrRuntimeContext = errors.New("runtime context snapshot is invalid")

// RequiredContextSnapshot requires a valid explicit owner snapshot at the execution consumer;
// absence is not interpreted as an intentionally empty context.
// RequiredContextSnapshot требует явный корректный снимок владельца у потребителя исполнения;
// отсутствие не трактуется как намеренно пустой контекст.
func (input RunnerInput) RequiredContextSnapshot(now time.Time) (RuntimeContextSnapshot, error) {
	if input.ContextSnapshot == nil || input.ContextSnapshot.ValidateFor(input, now) != nil {
		return RuntimeContextSnapshot{}, ErrRuntimeContext
	}
	return *input.ContextSnapshot, nil
}

// RuntimeContextSnapshot binds control-plane context pins to the outer execution lineage;
// the control plane verifies lifecycle eligibility before creating the immutable RuntimeRevision.
// RuntimeContextSnapshot связывает контекст control plane с внешней цепочкой исполнения;
// control plane проверяет доступность по lifecycle до создания неизменной RuntimeRevision.
type RuntimeContextSnapshot struct {
	Schema          string                `json:"schema"`
	OrganizationRef string                `json:"organization_ref"`
	ProjectRef      string                `json:"project_ref"`
	AgentRef        string                `json:"agent_ref"`
	Skills          []RuntimeSkillBundle  `json:"skills"`
	Memories        []RuntimeMemoryRecord `json:"memories"`
	Digest          string                `json:"digest"`
}

// RuntimeContextProvenance records the owner-projected actor, source revision, digest, and creation time.
// RuntimeContextProvenance хранит назначенные проекцией владельца actor, версию источника, дайджест и время создания.
type RuntimeContextProvenance struct {
	ActorRef       string    `json:"actor_ref"`
	SourceKind     string    `json:"source_kind"`
	SourceRef      string    `json:"source_ref"`
	SourceRevision string    `json:"source_revision"`
	Digest         string    `json:"digest"`
	CreatedAt      time.Time `json:"created_at"`
}

// RuntimeSkillFile pins one relative bundle path to exact artifact revision, digest, and byte size.
// RuntimeSkillFile закрепляет относительный путь навыка за точными версией артефакта, дайджестом и размером.
type RuntimeSkillFile struct {
	Path             string `json:"path"`
	ArtifactRef      string `json:"artifact_ref"`
	ArtifactRevision int64  `json:"artifact_revision"`
	Digest           string `json:"digest"`
	SizeBytes        int64  `json:"size_bytes"`
}

// RuntimeSkillBundle carries the exact published clean revision selected by the owner;
// lifecycle state and reviewer are not re-created by the execution consumer.
// RuntimeSkillBundle содержит точную опубликованную чистую версию, выбранную владельцем;
// состояние lifecycle и reviewer не создаются заново потребителем исполнения.
type RuntimeSkillBundle struct {
	BindingRef     string                   `json:"binding_ref"`
	BindingVersion int64                    `json:"binding_version"`
	BundleRef      string                   `json:"bundle_ref"`
	RevisionRef    string                   `json:"revision_ref"`
	Revision       int64                    `json:"revision"`
	Digest         string                   `json:"digest"`
	ScanEngine     string                   `json:"scan_engine"`
	ScanDigest     string                   `json:"scan_digest"`
	ScannedAt      time.Time                `json:"scanned_at"`
	Files          []RuntimeSkillFile       `json:"files"`
	Provenance     RuntimeContextProvenance `json:"provenance"`
	Name           string                   `json:"name"`
	Description    string                   `json:"description"`
}

// RuntimeMemoryRecord carries a pinned memory summary and retention deadline;
// scope, redaction eligibility, and permissions are resolved by the owner projection.
// RuntimeMemoryRecord содержит закреплённую выдержку памяти и срок хранения;
// область, допустимость по redaction и права разрешаются проекцией владельца.
type RuntimeMemoryRecord struct {
	BindingRef     string                   `json:"binding_ref"`
	BindingVersion int64                    `json:"binding_version"`
	RecordRef      string                   `json:"record_ref"`
	RevisionRef    string                   `json:"revision_ref"`
	Revision       int64                    `json:"revision"`
	Digest         string                   `json:"digest"`
	Title          string                   `json:"title"`
	Summary        string                   `json:"summary"`
	RetentionUntil time.Time                `json:"retention_until"`
	Provenance     RuntimeContextProvenance `json:"provenance"`
}

// ComputeDigest hashes the bounded typed snapshot with its own digest field cleared.
// ComputeDigest хеширует ограниченный типизированный снимок с очищенным собственным полем дайджеста.
func (snapshot RuntimeContextSnapshot) ComputeDigest() (string, error) {
	snapshot.Digest = ""
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) > MaximumRunnerInputBytes {
		return "", ErrRuntimeContext
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// ValidateFor checks exact outer-input pins, unique context identities, safe skill paths, byte budgets,
// and memory expiry at caller-supplied time; it does not replace outer execution authorization.
// ValidateFor проверяет привязки внешнего входа, уникальность контекста, пути навыков, объём и срок памяти
// по переданному времени; проверка не заменяет внешнюю авторизацию исполнения.
func (snapshot RuntimeContextSnapshot) ValidateFor(input RunnerInput, now time.Time) error {
	digest, err := snapshot.ComputeDigest()
	if err != nil || snapshot.Schema != RuntimeContextSchema || snapshot.Digest != digest ||
		!sha256Pattern.MatchString(snapshot.Digest) || snapshot.OrganizationRef != input.OrganizationRef ||
		snapshot.ProjectRef != input.ProjectRef || snapshot.AgentRef != input.AgentRef ||
		!opaqueReferencePattern.MatchString(snapshot.OrganizationRef) || !opaqueReferencePattern.MatchString(snapshot.AgentRef) ||
		(snapshot.ProjectRef != "" && !opaqueReferencePattern.MatchString(snapshot.ProjectRef)) ||
		(snapshot.ProjectRef == "" && (len(snapshot.Skills) != 0 || len(snapshot.Memories) != 0)) ||
		len(snapshot.Skills) > MaximumContextSkills || len(snapshot.Memories) > MaximumContextMemories || now.IsZero() {
		return ErrRuntimeContext
	}
	seen, names := map[string]bool{}, map[string]bool{}
	var total int64
	for _, skill := range snapshot.Skills {
		if !validContextPin(skill.BundleRef, skill.RevisionRef, skill.Revision, skill.Digest, skill.BindingRef, skill.BindingVersion) ||
			!validContextText(skill.Name, 640) || !validContextText(skill.Description, 8000) ||
			!validContextProvenance(skill.Provenance) || !validContextText(skill.ScanEngine, 256) ||
			!sha256Pattern.MatchString(skill.ScanDigest) || skill.ScannedAt.IsZero() ||
			len(skill.Files) == 0 || len(skill.Files) > MaximumSkillFiles || seen[skill.BundleRef] || seen[skill.BindingRef] || names[skill.Name] {
			return ErrRuntimeContext
		}
		seen[skill.BundleRef], seen[skill.BindingRef], names[skill.Name] = true, true, true
		paths := map[string]bool{}
		var size int64
		for _, file := range skill.Files {
			if !ValidSkillPath(file.Path) || paths[file.Path] || !opaqueReferencePattern.MatchString(file.ArtifactRef) ||
				file.ArtifactRevision < 1 || !imageDigestPattern.MatchString(file.Digest) ||
				file.SizeBytes < 0 || file.SizeBytes > MaximumSkillFileBytes {
				return ErrRuntimeContext
			}
			for existing := range paths {
				if strings.EqualFold(existing, file.Path) {
					return ErrRuntimeContext
				}
			}
			paths[file.Path] = true
			size += file.SizeBytes
		}
		for file := range paths {
			for parent := path.Dir(file); parent != "."; parent = path.Dir(parent) {
				if paths[parent] {
					return ErrRuntimeContext
				}
			}
		}
		if !paths["SKILL.md"] || size > MaximumSkillBundleBytes {
			return ErrRuntimeContext
		}
		total += size
	}
	for _, memory := range snapshot.Memories {
		if !validContextPin(memory.RecordRef, memory.RevisionRef, memory.Revision, memory.Digest, memory.BindingRef, memory.BindingVersion) ||
			!memory.RetentionUntil.After(now) || !validContextText(memory.Title, 640) || !validContextText(memory.Summary, 65536) ||
			!validContextProvenance(memory.Provenance) || seen[memory.RecordRef] || seen[memory.BindingRef] {
			return ErrRuntimeContext
		}
		seen[memory.RecordRef], seen[memory.BindingRef] = true, true
		total += int64(len(memory.Summary))
	}
	if total > MaximumContextBytes {
		return ErrRuntimeContext
	}
	return nil
}

// BoundExecutionContext inherits the earliest memory-retention deadline so the existing
// cancellation/join lifecycle stops the provider when any delivered memory expires.
// BoundExecutionContext наследует ближайший срок хранения памяти, чтобы существующий
// cancel/join остановил провайдера при истечении любой переданной записи.
func (snapshot RuntimeContextSnapshot) BoundExecutionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	var deadline time.Time
	for _, memory := range snapshot.Memories {
		if deadline.IsZero() || memory.RetentionUntil.Before(deadline) {
			deadline = memory.RetentionUntil
		}
	}
	if deadline.IsZero() {
		return context.WithCancel(ctx)
	}
	return context.WithDeadline(ctx, deadline)
}

// ValidSkillPath accepts bounded canonical relative bundle paths without traversal or hidden segments;
// supporting file types and script execution permissions remain the owner's scan/policy responsibility.
// ValidSkillPath принимает ограниченные канонические относительные пути без traversal и скрытых сегментов;
// типы вспомогательных файлов и запуск скриптов остаются ответственностью проверки и политики владельца.
func ValidSkillPath(value string) bool {
	if value == "" || len(value) > 240 || !utf8.ValidString(value) || strings.ContainsAny(value, "\\\x00\r\n:") ||
		path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.TrimSpace(part) != part {
			return false
		}
	}
	return !strings.EqualFold(value, "SKILL.md") || value == "SKILL.md"
}

// validContextPin requires immutable resource/revision references and a positive versioned binding.
// validContextPin требует неизменные ссылки ресурса и версии, а также положительную версию привязки.
func validContextPin(ref, revisionRef string, revision int64, digest, bindingRef string, bindingVersion int64) bool {
	return opaqueReferencePattern.MatchString(ref) && opaqueReferencePattern.MatchString(revisionRef) &&
		revision > 0 && sha256Pattern.MatchString(digest) && opaqueReferencePattern.MatchString(bindingRef) && bindingVersion > 0
}

// validContextText accepts bounded nonblank UTF-8 text without a NUL byte.
// validContextText принимает ограниченный непустой текст UTF-8 без NUL-байта.
func validContextText(value string, maximum int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

// validContextProvenance checks actor/source shape, source-reference bounds, digest, and creation time.
// validContextProvenance проверяет форму actor и источника, пределы ссылок, дайджест и время создания.
func validContextProvenance(value RuntimeContextProvenance) bool {
	return opaqueReferencePattern.MatchString(value.ActorRef) && validContextText(value.SourceKind, 64) &&
		len(value.SourceRef) <= 128 && len(value.SourceRevision) <= 128 &&
		sha256Pattern.MatchString(value.Digest) && !value.CreatedAt.IsZero()
}
