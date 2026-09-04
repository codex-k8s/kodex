package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func contextTimestamp(value *timestamppb.Timestamp) (time.Time, bool) {
	if value == nil || value.CheckValid() != nil {
		return time.Time{}, false
	}
	return value.AsTime(), true
}

func contextOptionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func contextProvenanceView(value *controlplanev1.ContextProvenance) (generated.ContextProvenance, bool) {
	created, ok := contextTimestamp(value.GetCreatedAt())
	if !ok || !opaqueHTTPReference.MatchString(value.GetActorRef()) || value.GetSourceKind() == "" || len(value.GetSourceKind()) > 64 || len(value.GetSourceRef()) > 128 || len(value.GetSourceRevision()) > 128 || !validManagedDigest(value.GetDigest()) {
		return generated.ContextProvenance{}, false
	}
	return generated.ContextProvenance{ActorRef: value.GetActorRef(), SourceKind: value.GetSourceKind(), SourceRef: contextOptionalText(value.GetSourceRef()), SourceRevision: contextOptionalText(value.GetSourceRevision()), Digest: value.GetDigest(), CreatedAt: created}, true
}

func contextResourceState(value controlplanev1.ContextResourceState) (generated.ContextResourceState, bool) {
	if value < controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_ACTIVE || value > controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_PURGED {
		return "", false
	}
	return generated.ContextResourceState(strings.TrimPrefix(value.String(), "CONTEXT_RESOURCE_STATE_")), true
}

func skillRevisionView(value *controlplanev1.SkillBundleRevision) (generated.SkillBundleRevision, bool) {
	provenance, ok := contextProvenanceView(value.GetProvenance())
	if !ok || !opaqueHTTPReference.MatchString(value.GetRef()) || !validManagedVersion(value.GetRevision()) || !validManagedDigest(value.GetDigest()) ||
		value.GetState() < controlplanev1.SkillRevisionState_SKILL_REVISION_STATE_DRAFT || value.GetState() > controlplanev1.SkillRevisionState_SKILL_REVISION_STATE_DISCARDED ||
		value.GetScanState() < controlplanev1.SkillScanState_SKILL_SCAN_STATE_PENDING || value.GetScanState() > controlplanev1.SkillScanState_SKILL_SCAN_STATE_ERROR ||
		len(value.GetFiles()) > 128 || len(value.GetDiagnostics()) > 128 || len(value.GetName()) > 160 || len(value.GetDescription()) > 4000 {
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
	for _, file := range value.GetFiles() {
		if !validSkillManifestPath(file.GetPath()) || seen[file.GetPath()] || !opaqueHTTPReference.MatchString(file.GetArtifactRef()) || !validManagedVersion(file.GetArtifactRevision()) || !validManagedDigest(file.GetDigest()) || file.GetSizeBytes() < 0 || file.GetSizeBytes() > maximumSafeJSONInteger {
			return result, false
		}
		seen[file.GetPath()] = true
		result.Files = append(result.Files, generated.SkillBundleFile{Path: file.GetPath(), ArtifactRef: file.GetArtifactRef(), ArtifactRevision: file.GetArtifactRevision(), Digest: file.GetDigest(), SizeBytes: file.GetSizeBytes()})
	}
	for _, diagnostic := range result.Diagnostics {
		if len(diagnostic) > 2000 {
			return result, false
		}
	}
	return result, true
}

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

func memoryRevisionView(value *controlplanev1.MemoryRecordRevision) (generated.MemoryRecordRevision, bool) {
	provenance, ok := contextProvenanceView(value.GetProvenance())
	retention, rok := contextTimestamp(value.GetRetentionUntil())
	if !ok || !rok || !opaqueHTTPReference.MatchString(value.GetRef()) || !validManagedVersion(value.GetRevision()) || !validManagedDigest(value.GetDigest()) ||
		len(value.GetTitle()) > 160 || len(value.GetSummary()) > 65536 || value.GetRedacted() && value.GetSummary() != "" || value.GetParentRevisionRef() != "" && !opaqueHTTPReference.MatchString(value.GetParentRevisionRef()) {
		return generated.MemoryRecordRevision{}, false
	}
	return generated.MemoryRecordRevision{Ref: value.GetRef(), Revision: value.GetRevision(), Title: value.GetTitle(), Summary: value.GetSummary(), Digest: value.GetDigest(), ParentRevisionRef: contextOptionalText(value.GetParentRevisionRef()), Provenance: provenance, RetentionUntil: retention, Redacted: value.GetRedacted()}, true
}

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

func writeSkillBundle(w http.ResponseWriter, value *controlplanev1.SkillBundle, ref string, status int) {
	result, ok := skillBundleView(value)
	if !ok || ref != "" && result.Ref != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(result.Version, 10)+"\"")
	writeJSON(w, status, result)
}

func writeMemoryRecord(w http.ResponseWriter, value *controlplanev1.KodexMemoryRecord, ref string, status int) {
	result, ok := memoryRecordView(value)
	if !ok || ref != "" && result.Ref != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(result.Version, 10)+"\"")
	writeJSON(w, status, result)
}

func writeAgentContextBinding(w http.ResponseWriter, value *controlplanev1.AgentContextBinding, agent, resource, revision string) {
	if !opaqueHTTPReference.MatchString(value.GetRef()) || !validManagedVersion(value.GetVersion()) || value.GetAgentRef() != agent || value.GetResourceRef() != resource || value.GetRevisionRef() != revision || !validManagedDigest(value.GetDigest()) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	// If-Match этой операции относится к agent, а CP возвращает только binding version.
	writeJSON(w, http.StatusOK, generated.AgentContextBinding{Ref: value.GetRef(), Version: value.GetVersion(), AgentRef: agent, ResourceRef: resource, RevisionRef: revision, Digest: value.GetDigest()})
}
