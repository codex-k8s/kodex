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
	RuntimeContextSchema          = "kodex.runtime-context.v1"
	RuntimeContextRoot            = "/workspace/context"
	MaximumContextSkills          = 32
	MaximumContextMemories        = 64
	MaximumSkillFiles             = 128
	MaximumSkillFileBytes   int64 = 32 << 20
	MaximumSkillBundleBytes int64 = 64 << 20
	MaximumContextBytes     int64 = 512 << 20
)

var ErrRuntimeContext = errors.New("runtime context snapshot is invalid")

// RequiredContextSnapshot вызывается в исполняющем consumer. Отсутствующий
// snapshot не равен явно назначенному владельцем пустому набору контекста.
func (input RunnerInput) RequiredContextSnapshot(now time.Time) (RuntimeContextSnapshot, error) {
	if input.ContextSnapshot == nil || input.ContextSnapshot.ValidateFor(input, now) != nil {
		return RuntimeContextSnapshot{}, ErrRuntimeContext
	}
	return *input.ContextSnapshot, nil
}

// RuntimeContextSnapshot связывает CP pins с внешней execution lineage.
// Lifecycle eligibility проверяет CP до создания immutable RuntimeRevision.
type RuntimeContextSnapshot struct {
	Schema          string                `json:"schema"`
	OrganizationRef string                `json:"organization_ref"`
	ProjectRef      string                `json:"project_ref"`
	AgentRef        string                `json:"agent_ref"`
	Skills          []RuntimeSkillBundle  `json:"skills"`
	Memories        []RuntimeMemoryRecord `json:"memories"`
	Digest          string                `json:"digest"`
}

type RuntimeContextProvenance struct {
	ActorRef       string    `json:"actor_ref"`
	SourceKind     string    `json:"source_kind"`
	SourceRef      string    `json:"source_ref"`
	SourceRevision string    `json:"source_revision"`
	Digest         string    `json:"digest"`
	CreatedAt      time.Time `json:"created_at"`
}

type RuntimeSkillFile struct {
	Path             string `json:"path"`
	ArtifactRef      string `json:"artifact_ref"`
	ArtifactRevision int64  `json:"artifact_revision"`
	Digest           string `json:"digest"`
	SizeBytes        int64  `json:"size_bytes"`
}

// RuntimeSkillBundle соответствует RuntimeSkillBundleSnapshot CP #1046.
// Поля State/reviewer не дублируются: CP выдаёт только PUBLISHED/CLEAN revision.
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

// RuntimeMemoryRecord соответствует RuntimeMemoryRecordSnapshot CP #1046.
// Scope, отсутствие redaction и permission устанавливаются owner projection.
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

func (snapshot RuntimeContextSnapshot) ComputeDigest() (string, error) {
	snapshot.Digest = ""
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) > MaximumRunnerInputBytes {
		return "", ErrRuntimeContext
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// ValidateFor не назначает полномочия: outer input уже проверен по binding.
// now передаёт вызывающий, expiry проверяется перед каждым процессом.
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

// BoundExecutionContext прекращает использование памяти при ближайшем retention
// deadline; существующий cancel/join останавливает provider.
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

// ValidSkillPath проверяет пути одобренного bundle. Типы supporting files и
// разрешение исполнения scripts принадлежат scan/policy владельца.
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

func validContextPin(ref, revisionRef string, revision int64, digest, bindingRef string, bindingVersion int64) bool {
	return opaqueReferencePattern.MatchString(ref) && opaqueReferencePattern.MatchString(revisionRef) &&
		revision > 0 && sha256Pattern.MatchString(digest) && opaqueReferencePattern.MatchString(bindingRef) && bindingVersion > 0
}

func validContextText(value string, maximum int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validContextProvenance(value RuntimeContextProvenance) bool {
	return opaqueReferencePattern.MatchString(value.ActorRef) && validContextText(value.SourceKind, 64) &&
		len(value.SourceRef) <= 128 && len(value.SourceRevision) <= 128 &&
		sha256Pattern.MatchString(value.Digest) && !value.CreatedAt.IsZero()
}
