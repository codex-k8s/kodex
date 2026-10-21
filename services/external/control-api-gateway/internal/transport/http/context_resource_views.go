package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// contextTimestamp requires a present, valid protobuf timestamp and converts it
// to time.Time; nil or out-of-range metadata is not replaced with a made-up date.
// contextTimestamp требует присутствующий корректный protobuf timestamp
// и преобразует его в time.Time; nil или неверная дата не подменяется выдуманной.
func contextTimestamp(value *timestamppb.Timestamp) (time.Time, bool) {
	if value == nil || value.CheckValid() != nil {
		return time.Time{}, false
	}
	return value.AsTime(), true
}

// contextOptionalText represents an empty optional field as nil and otherwise
// returns a pointer to the unchanged string without trimming or normalization.
// contextOptionalText представляет пустое необязательное поле как nil, иначе
// возвращает указатель на неизменённую строку без обрезки и нормализации.
func contextOptionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// contextProvenanceView validates provenance time, actor, bounded source metadata
// and digest before projecting optional strings. This is wire validation, not
// proof that an actor owns the referenced source or may change the resource.
// contextProvenanceView проверяет время, actor, ограниченные source-поля и digest
// до преобразования optional строк. Это wire-проверка, а не доказательство
// владения источником или права изменить ресурс.
func contextProvenanceView(value *controlplanev1.ContextProvenance) (generated.ContextProvenance, bool) {
	created, ok := contextTimestamp(value.GetCreatedAt())
	if !ok || !opaqueHTTPReference.MatchString(value.GetActorRef()) || value.GetSourceKind() == "" || len(value.GetSourceKind()) > 64 || len(value.GetSourceRef()) > 128 || len(value.GetSourceRevision()) > 128 || !validManagedDigest(value.GetDigest()) {
		return generated.ContextProvenance{}, false
	}
	return generated.ContextProvenance{ActorRef: value.GetActorRef(), SourceKind: value.GetSourceKind(), SourceRef: contextOptionalText(value.GetSourceRef()), SourceRevision: contextOptionalText(value.GetSourceRevision()), Digest: value.GetDigest(), CreatedAt: created}, true
}

// contextResourceState accepts only ACTIVE through PURGED and strips the exact
// protobuf prefix; unspecified or unknown numeric states cannot reach the UI.
// contextResourceState допускает только ACTIVE–PURGED и снимает точный protobuf
// префикс; unspecified и неизвестное числовое состояние не попадает в UI.
func contextResourceState(value controlplanev1.ContextResourceState) (generated.ContextResourceState, bool) {
	if value < controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_ACTIVE || value > controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_PURGED {
		return "", false
	}
	return generated.ContextResourceState(strings.TrimPrefix(value.String(), "CONTEXT_RESOURCE_STATE_")), true
}

// skillRevisionView validates revision/provenance, enums, optional references
// and scan/review times, then checks bounded file metadata and diagnostics.
// Case-folded path uniqueness and the64MiB aggregate limit are enforced before
// returning a successful view; callers must not publish a value when ok is false.
// skillRevisionView проверяет revision/provenance, enum, optional ссылки
// и scan/review timestamps, затем ограниченные file metadata и diagnostics.
// Уникальность путей без учёта регистра и общий предел64МиБ проверяются
// до успеха; при ok=false вызывающий код не должен публиковать значение.
func skillRevisionView(value *controlplanev1.SkillBundleRevision) (generated.SkillBundleRevision, bool) {
	provenance, ok := contextProvenanceView(value.GetProvenance())
	if !ok || !opaqueHTTPReference.MatchString(value.GetRef()) || !validManagedVersion(value.GetRevision()) || !validManagedDigest(value.GetDigest()) ||
		value.GetState() < controlplanev1.SkillRevisionState_SKILL_REVISION_STATE_DRAFT || value.GetState() > controlplanev1.SkillRevisionState_SKILL_REVISION_STATE_DISCARDED ||
		value.GetScanState() < controlplanev1.SkillScanState_SKILL_SCAN_STATE_PENDING || value.GetScanState() > controlplanev1.SkillScanState_SKILL_SCAN_STATE_ERROR ||
		len(value.GetFiles()) > 128 || len(value.GetDiagnostics()) > 128 || utf8.RuneCountInString(value.GetName()) > 160 || utf8.RuneCountInString(value.GetDescription()) > 2000 || strings.ContainsRune(value.GetName()+value.GetDescription(), 0) {
		return generated.SkillBundleRevision{}, false
	}
	result := generated.SkillBundleRevision{Ref: value.GetRef(), Revision: value.GetRevision(), State: generated.SkillBundleRevisionState(strings.TrimPrefix(value.GetState().String(), "SKILL_REVISION_STATE_")),
		Name: value.GetName(), Description: value.GetDescription(), Digest: value.GetDigest(), Provenance: provenance, ParentRevisionRef: contextOptionalText(value.GetParentRevisionRef()),
		ScanState: generated.SkillBundleRevisionScanState(strings.TrimPrefix(value.GetScanState().String(), "SKILL_SCAN_STATE_")), ScanEngine: contextOptionalText(value.GetScanEngine()), ScanDigest: contextOptionalText(value.GetScanDigest()),
		ReviewedBy: contextOptionalText(value.GetReviewedBy()), Files: []generated.SkillBundleFile{}, Diagnostics: append([]string{}, value.GetDiagnostics()...)}
	if result.ParentRevisionRef != nil && !opaqueHTTPReference.MatchString(*result.ParentRevisionRef) || result.ReviewedBy != nil && !opaqueHTTPReference.MatchString(*result.ReviewedBy) || result.ScanDigest != nil && !validManagedDigest(*result.ScanDigest) || len(value.GetScanEngine()) > 128 {
		return result, false
	}
	for _, pair := range []struct {
		source *timestamppb.Timestamp
		target **time.Time
	}{{value.GetScannedAt(), &result.ScannedAt}, {value.GetReviewedAt(), &result.ReviewedAt}} {
		if pair.source != nil {
			stamp, valid := contextTimestamp(pair.source)
			if !valid {
				return result, false
			}
			*pair.target = &stamp
		}
	}
	seen := make(map[string]bool)
	var totalSize int64
	for _, file := range value.GetFiles() {
		key := strings.ToLower(file.GetPath())
		if !validSkillManifestPath(file.GetPath()) || seen[key] || !opaqueHTTPReference.MatchString(file.GetArtifactRef()) || !validManagedVersion(file.GetArtifactRevision()) || !strings.HasPrefix(file.GetDigest(), "sha256:") || !validManagedDigest(strings.TrimPrefix(file.GetDigest(), "sha256:")) || file.GetSizeBytes() < 0 || file.GetSizeBytes() > 32<<20 {
			return result, false
		}
		totalSize += file.GetSizeBytes()
		if totalSize > 64<<20 {
			return result, false
		}
		seen[key] = true
		result.Files = append(result.Files, generated.SkillBundleFile{Path: file.GetPath(), ArtifactRef: file.GetArtifactRef(), ArtifactRevision: file.GetArtifactRevision(), Digest: file.GetDigest(), SizeBytes: file.GetSizeBytes()})
	}
	for _, diagnostic := range result.Diagnostics {
		if len(diagnostic) > 2000 {
			return result, false
		}
	}
	return result, true
}

// skillBundleView validates resource refs, version, state and timestamps, then
// independently converts present current/draft revisions without inventing an
// absent revision or accepting a partially invalid one.
// skillBundleView проверяет ссылки, версию, состояние и timestamps, затем
// независимо преобразует присутствующие current/draft ревизии без выдумывания
// отсутствующей ревизии или принятия частично неверной.
func skillBundleView(value *controlplanev1.SkillBundle) (generated.SkillBundle, bool) {
	state, ok := contextResourceState(value.GetState())
	created, cok := contextTimestamp(value.GetCreatedAt())
	updated, uok := contextTimestamp(value.GetUpdatedAt())
	if !ok || !cok || !uok || !opaqueHTTPReference.MatchString(value.GetRef()) || !opaqueHTTPReference.MatchString(value.GetProjectRef()) || !validManagedVersion(value.GetVersion()) {
		return generated.SkillBundle{}, false
	}
	result := generated.SkillBundle{Ref: value.GetRef(), Version: value.GetVersion(), ProjectRef: value.GetProjectRef(), State: state, CreatedAt: created, UpdatedAt: updated}
	if value.GetCurrentRevision() != nil {
		revision, valid := skillRevisionView(value.GetCurrentRevision())
		if !valid {
			return result, false
		}
		result.CurrentRevision = &revision
	}
	if value.GetDraftRevision() != nil {
		revision, valid := skillRevisionView(value.GetDraftRevision())
		if !valid {
			return result, false
		}
		result.DraftRevision = &revision
	}
	return result, true
}

// memoryRevisionView requires valid provenance, retention, revision and digest;
// it bounds title by Unicode runes and summary by bytes, rejects NUL and requires
// a redacted revision to have no summary while preserving allowed empty parents.
// memoryRevisionView требует корректные provenance, retention, revision и digest;
// title ограничен Unicode-символами, summary — байтами. NUL запрещён, redacted
// ревизия не содержит summary; допустимый пустой parent сохраняется.
func memoryRevisionView(value *controlplanev1.MemoryRecordRevision) (generated.MemoryRecordRevision, bool) {
	provenance, ok := contextProvenanceView(value.GetProvenance())
	retention, rok := contextTimestamp(value.GetRetentionUntil())
	if !ok || !rok || !opaqueHTTPReference.MatchString(value.GetRef()) || !validManagedVersion(value.GetRevision()) || !validManagedDigest(value.GetDigest()) ||
		utf8.RuneCountInString(value.GetTitle()) > 160 || strings.ContainsRune(value.GetTitle()+value.GetSummary(), 0) || len(value.GetSummary()) > 65536 || value.GetRedacted() && value.GetSummary() != "" || value.GetParentRevisionRef() != "" && !opaqueHTTPReference.MatchString(value.GetParentRevisionRef()) {
		return generated.MemoryRecordRevision{}, false
	}
	return generated.MemoryRecordRevision{Ref: value.GetRef(), Revision: value.GetRevision(), Title: value.GetTitle(), Summary: value.GetSummary(), Digest: value.GetDigest(), ParentRevisionRef: contextOptionalText(value.GetParentRevisionRef()), Provenance: provenance, RetentionUntil: retention, Redacted: value.GetRedacted()}, true
}

// memoryRecordView validates state/time/resource pins and the current revision.
// Optional agent refs retain their own syntax check; EXPIRED/PURGED records must
// carry a redacted revision rather than exposing their former summary.
// memoryRecordView проверяет state/time/resource и текущую ревизию.
// Необязательный agent ref сохраняет проверку формы; EXPIRED/PURGED должны
// содержать redacted-ревизию, не раскрывая прежний summary.
func memoryRecordView(value *controlplanev1.KodexMemoryRecord) (generated.KodexMemoryRecord, bool) {
	state, ok := contextResourceState(value.GetState())
	created, cok := contextTimestamp(value.GetCreatedAt())
	updated, uok := contextTimestamp(value.GetUpdatedAt())
	revision, rok := memoryRevisionView(value.GetCurrentRevision())
	if !ok || !cok || !uok || !rok || !opaqueHTTPReference.MatchString(value.GetRef()) || !opaqueHTTPReference.MatchString(value.GetProjectRef()) || !validManagedVersion(value.GetVersion()) ||
		value.GetAgentRef() != "" && !opaqueHTTPReference.MatchString(value.GetAgentRef()) || (state == generated.ContextResourceStateEXPIRED || state == generated.ContextResourceStatePURGED) && !revision.Redacted {
		return generated.KodexMemoryRecord{}, false
	}
	return generated.KodexMemoryRecord{Ref: value.GetRef(), Version: value.GetVersion(), ProjectRef: value.GetProjectRef(), AgentRef: contextOptionalText(value.GetAgentRef()), State: state, CurrentRevision: revision, CreatedAt: created, UpdatedAt: updated}, true
}

// writeSkillBundle publishes only a valid bundle matching the requested ref,
// using its exact version as ETag; a mismatched upstream value yields502 instead.
// writeSkillBundle публикует только корректный bundle с запрошенным ref
// и его точной версией в ETag; неверный upstream ответ заменяется502.
func writeSkillBundle(w http.ResponseWriter, value *controlplanev1.SkillBundle, ref string, status int) {
	result, ok := skillBundleView(value)
	if !ok || ref != "" && result.Ref != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(result.Version, 10)+"\"")
	writeJSON(w, status, result)
}

// writeMemoryRecord applies the memory-specific converter and exact ref check
// before writing ETag/body; conversion failure never emits a partial memory view.
// writeMemoryRecord применяет memory-specific преобразование и точный ref
// до ETag/body; ошибка не выдаёт частичное представление памяти.
func writeMemoryRecord(w http.ResponseWriter, value *controlplanev1.KodexMemoryRecord, ref string, status int) {
	result, ok := memoryRecordView(value)
	if !ok || ref != "" && result.Ref != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(result.Version, 10)+"\"")
	writeJSON(w, status, result)
}

// writeAgentContextBinding requires exact agent/resource/revision and valid
// binding metadata before writing JSON. It deliberately emits no agent ETag:
// the command's If-Match pins the agent, while the response version is the binding.
// writeAgentContextBinding требует точные agent/resource/revision и корректные
// metadata до JSON. ETag агента намеренно не выдаётся: If-Match команды
// закрепляет агента, а версия ответа принадлежит binding.
func writeAgentContextBinding(w http.ResponseWriter, value *controlplanev1.AgentContextBinding, agent, resource, revision string) {
	if !validAgentContextBinding(value, agent) || value.GetResourceRef() != resource || value.GetRevisionRef() != revision {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, generated.AgentContextBinding{Ref: value.GetRef(), Version: value.GetVersion(), AgentRef: agent, ResourceRef: resource, RevisionRef: revision, Digest: value.GetDigest()})
}

// validAgentContextBinding checks wire shape, positive version, digest and exact
// agent equality; resource/revision equality is enforced by the writing caller
// and these response checks never establish owner authority.
// validAgentContextBinding проверяет wire-форму, положительную версию, digest
// и точное равенство agent; resource/revision сравнивает вызывающий writer,
// а полномочий владельца эти проверки ответа не выдают.
func validAgentContextBinding(value *controlplanev1.AgentContextBinding, agent string) bool {
	return opaqueHTTPReference.MatchString(value.GetRef()) && validManagedVersion(value.GetVersion()) && value.GetAgentRef() == agent && opaqueHTTPReference.MatchString(value.GetResourceRef()) && opaqueHTTPReference.MatchString(value.GetRevisionRef()) && validManagedDigest(value.GetDigest())
}
