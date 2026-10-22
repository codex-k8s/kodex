package workload

import (
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// hydrateRuntimeContext builds and validates a digest-bound skill/memory snapshot
// for the input scope, then publishes it only on success. File-catalog hydration
// happens first and may change input.FileCatalog even when a later check fails;
// callers must discard a rejected input rather than rely on rollback.
// hydrateRuntimeContext собирает и проверяет привязанный к digest снимок навыков
// и памяти для области input, назначая ContextSnapshot только после успеха.
// Каталог файлов обрабатывается раньше и может изменить input.FileCatalog даже
// при последующем отказе; отвергнутый input следует отбросить, отката здесь нет.
func hydrateRuntimeContext(input *runtimecontract.RunnerInput, revision *cp.RuntimeRevisionSnapshot) error {
	if input == nil || revision == nil {
		return runtimecontract.ErrRuntimeContext
	}
	if err := hydrateRuntimeFileCatalog(input, revision); err != nil {
		return err
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

// hydrateRuntimeFileCatalog clears the previous catalog, converts the closed
// purpose enum and validates an optional catalog for a project turn. It copies
// references and pins only; it neither fetches files nor authorizes their reads.
// hydrateRuntimeFileCatalog очищает прежний каталог, переводит закрытый enum
// назначений и проверяет необязательный каталог проектного хода. Копируются
// только ссылки и привязки; загрузка файлов и разрешение чтения здесь не выдаются.
func hydrateRuntimeFileCatalog(input *runtimecontract.RunnerInput, revision *cp.RuntimeRevisionSnapshot) error {
	input.FileCatalog = nil
	source := revision.GetFileCatalog()
	if source == nil {
		return nil
	}
	if input.Mode != runtimecontract.RunnerModeTurn || input.ProjectRef == "" {
		return runtimecontract.ErrRuntimeContext
	}
	catalog := &runtimecontract.RuntimeFileCatalog{Ref: source.GetRef(), Digest: source.GetDigest(), Total: source.GetTotal()}
	for _, purpose := range source.GetPurposes() {
		switch purpose {
		case cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_PROJECT:
			catalog.Purposes = append(catalog.Purposes, runtimecontract.FilePurposeProject)
		case cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_WORKSPACE_INPUT:
			catalog.Purposes = append(catalog.Purposes, runtimecontract.FilePurposeWorkspaceInput)
		case cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_RUN_RESULT:
			catalog.Purposes = append(catalog.Purposes, runtimecontract.FilePurposeRunResult)
		case cp.RuntimeFilePurpose_RUNTIME_FILE_PURPOSE_SKILL:
			catalog.Purposes = append(catalog.Purposes, runtimecontract.FilePurposeSkill)
		default:
			return runtimecontract.ErrRuntimeContext
		}
	}
	if catalog.Validate() != nil {
		return runtimecontract.ErrRuntimeContext
	}
	input.FileCatalog = catalog
	return nil
}

// contextProvenanceFromProto rejects missing provenance or an invalid timestamp,
// then copies attribution and revision fields with a UTC creation time. This
// conversion alone does not authenticate the actor or verify content digests.
// contextProvenanceFromProto отвергает отсутствие provenance и неверную временную
// метку, затем копирует авторство и версии с временем создания в UTC. Сам перевод
// не удостоверяет actor и не проверяет соответствие digest содержимому.
func contextProvenanceFromProto(value *cp.ContextProvenance) (runtimecontract.RuntimeContextProvenance, error) {
	if value == nil || value.GetCreatedAt().CheckValid() != nil {
		return runtimecontract.RuntimeContextProvenance{}, runtimecontract.ErrRuntimeContext
	}
	return runtimecontract.RuntimeContextProvenance{ActorRef: value.GetActorRef(), SourceKind: value.GetSourceKind(),
		SourceRef: value.GetSourceRef(), SourceRevision: value.GetSourceRevision(), Digest: value.GetDigest(), CreatedAt: value.GetCreatedAt().AsTime().UTC()}, nil
}
