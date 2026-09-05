package workload

import (
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func hydrateRuntimeContext(input *runtimecontract.RunnerInput, revision *cp.RuntimeRevisionSnapshot) error {
	if input == nil || revision == nil {
		return runtimecontract.ErrRuntimeContext
	}
	snapshot := runtimecontract.RuntimeContextSnapshot{Schema: runtimecontract.RuntimeContextSchema,
		OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, AgentRef: input.AgentRef,
		Skills: []runtimecontract.RuntimeSkillBundle{}, Memories: []runtimecontract.RuntimeMemoryRecord{}}
	if len(revision.GetSkillBundles()) > runtimecontract.MaximumContextSkills || len(revision.GetMemoryRecords()) > runtimecontract.MaximumContextMemories {
		return runtimecontract.ErrRuntimeContext
	}
	for _, item := range revision.GetSkillBundles() {
		if item == nil || item.GetScannedAt().CheckValid() != nil || len(item.GetFiles()) > runtimecontract.MaximumSkillFiles {
			return runtimecontract.ErrRuntimeContext
		}
		provenance, err := contextProvenanceFromProto(item.GetProvenance())
		if err != nil {
			return err
		}
		skill := runtimecontract.RuntimeSkillBundle{BindingRef: item.GetBindingRef(), BindingVersion: item.GetBindingVersion(),
			BundleRef: item.GetBundleRef(), RevisionRef: item.GetRevisionRef(), Revision: item.GetRevision(), Digest: item.GetDigest(),
			ScanEngine: item.GetScanEngine(), ScanDigest: item.GetScanDigest(), ScannedAt: item.GetScannedAt().AsTime().UTC(),
			Provenance: provenance, Name: item.GetName(), Description: item.GetDescription()}
		for _, file := range item.GetFiles() {
			if file == nil {
				return runtimecontract.ErrRuntimeContext
			}
			skill.Files = append(skill.Files, runtimecontract.RuntimeSkillFile{Path: file.GetPath(), ArtifactRef: file.GetArtifactRef(),
				ArtifactRevision: file.GetArtifactRevision(), Digest: file.GetDigest(), SizeBytes: file.GetSizeBytes()})
		}
		snapshot.Skills = append(snapshot.Skills, skill)
	}
	for _, item := range revision.GetMemoryRecords() {
		if item == nil || item.GetRetentionUntil().CheckValid() != nil {
			return runtimecontract.ErrRuntimeContext
		}
		provenance, err := contextProvenanceFromProto(item.GetProvenance())
		if err != nil {
			return err
		}
		snapshot.Memories = append(snapshot.Memories, runtimecontract.RuntimeMemoryRecord{BindingRef: item.GetBindingRef(), BindingVersion: item.GetBindingVersion(),
			RecordRef: item.GetRecordRef(), RevisionRef: item.GetRevisionRef(), Revision: item.GetRevision(), Digest: item.GetDigest(),
			Title: item.GetTitle(), Summary: item.GetSummary(), RetentionUntil: item.GetRetentionUntil().AsTime().UTC(), Provenance: provenance})
	}
	var err error
	snapshot.Digest, err = snapshot.ComputeDigest()
	if err != nil || snapshot.ValidateFor(*input, time.Now()) != nil {
		return runtimecontract.ErrRuntimeContext
	}
	input.ContextSnapshot = &snapshot
	return nil
}

func contextProvenanceFromProto(value *cp.ContextProvenance) (runtimecontract.RuntimeContextProvenance, error) {
	if value == nil || value.GetCreatedAt().CheckValid() != nil {
		return runtimecontract.RuntimeContextProvenance{}, runtimecontract.ErrRuntimeContext
	}
	return runtimecontract.RuntimeContextProvenance{ActorRef: value.GetActorRef(), SourceKind: value.GetSourceKind(),
		SourceRef: value.GetSourceRef(), SourceRevision: value.GetSourceRevision(), Digest: value.GetDigest(), CreatedAt: value.GetCreatedAt().AsTime().UTC()}, nil
}
