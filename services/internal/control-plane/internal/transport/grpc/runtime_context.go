package grpc

import (
	"encoding/json"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func castRuntimeContext(result *cp.RuntimeRevisionSnapshot, value any, projectRef string) bool {
	if value == nil {
		return true
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var snapshot runtimecontract.RuntimeContextSnapshot
	if json.Unmarshal(raw, &snapshot) != nil {
		return false
	}
	digest, err := snapshot.ComputeDigest()
	if err != nil || snapshot.Schema != runtimecontract.RuntimeContextSchema || snapshot.Digest != digest ||
		snapshot.OrganizationRef != result.OrganizationRef || snapshot.AgentRef != result.AgentRef || snapshot.ProjectRef != projectRef {
		return false
	}
	for _, skill := range snapshot.Skills {
		item := &cp.RuntimeSkillBundleSnapshot{BindingRef: skill.BindingRef, BindingVersion: skill.BindingVersion,
			BundleRef: skill.BundleRef, RevisionRef: skill.RevisionRef, Revision: skill.Revision, Digest: skill.Digest,
			ScanEngine: skill.ScanEngine, ScanDigest: skill.ScanDigest, ScannedAt: timestamppb.New(skill.ScannedAt),
			Provenance: castRuntimeContextProvenance(skill.Provenance), Name: skill.Name, Description: skill.Description}
		for _, file := range skill.Files {
			item.Files = append(item.Files, &cp.SkillBundleFile{Path: file.Path, ArtifactRef: file.ArtifactRef,
				ArtifactRevision: file.ArtifactRevision, Digest: file.Digest, SizeBytes: file.SizeBytes})
		}
		result.SkillBundles = append(result.SkillBundles, item)
	}
	for _, memory := range snapshot.Memories {
		result.MemoryRecords = append(result.MemoryRecords, &cp.RuntimeMemoryRecordSnapshot{BindingRef: memory.BindingRef,
			BindingVersion: memory.BindingVersion, RecordRef: memory.RecordRef, RevisionRef: memory.RevisionRef, Revision: memory.Revision,
			Digest: memory.Digest, Title: memory.Title, Summary: memory.Summary, RetentionUntil: timestamppb.New(memory.RetentionUntil),
			Provenance: castRuntimeContextProvenance(memory.Provenance)})
	}
	return true
}

func castRuntimeContextProvenance(p runtimecontract.RuntimeContextProvenance) *cp.ContextProvenance {
	return &cp.ContextProvenance{ActorRef: p.ActorRef, SourceKind: p.SourceKind, SourceRef: p.SourceRef,
		SourceRevision: p.SourceRevision, Digest: p.Digest, CreatedAt: timestamppb.New(p.CreatedAt)}
}
