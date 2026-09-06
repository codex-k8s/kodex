package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_context_skills.sql
var queryRuntimeContextSkills string

//go:embed sql/runtime_context_memories.sql
var queryRuntimeContextMemories string

//go:embed sql/prompt_context_skills.sql
var queryPromptContextSkills string

//go:embed sql/prompt_context_memories.sql
var queryPromptContextMemories string

func runtimeContextSessionID(sessionID, previousDigest, currentDigest string) string {
	if previousDigest == "" || currentDigest == "" || previousDigest != currentDigest {
		return ""
	}
	return sessionID
}

func runtimeContextProvenance(p entity.ContextProvenance) runtimecontract.RuntimeContextProvenance {
	return runtimecontract.RuntimeContextProvenance{ActorRef: p.ActorRef, SourceKind: p.SourceKind, SourceRef: p.SourceRef,
		SourceRevision: p.SourceRevision, Digest: p.Digest, CreatedAt: p.CreatedAt.UTC()}
}

func (repository *Repository) runtimeContextSnapshot(ctx context.Context, tx pgx.Tx, current scope, runRef, projectRef, agentRef string) (runtimecontract.RuntimeContextSnapshot, error) {
	if projectRef != "" {
		if err := tx.QueryRow(ctx, querySTTRuntimeActor, current.organizationID, runRef).Scan(
			&current.actorID, &current.actorRef, &current.actorName, &current.organizationRef); err != nil {
			return runtimecontract.RuntimeContextSnapshot{}, errs.ErrConflict
		}
	}
	return repository.runtimeContextSnapshotForActorMode(ctx, tx, current, projectRef, agentRef, true)
}

func (repository *Repository) runtimeContextSnapshotForActor(ctx context.Context, tx pgx.Tx, current scope, projectRef, agentRef string) (runtimecontract.RuntimeContextSnapshot, error) {
	return repository.runtimeContextSnapshotForActorMode(ctx, tx, current, projectRef, agentRef, false)
}

func (repository *Repository) runtimeContextSnapshotForActorMode(ctx context.Context, tx pgx.Tx, current scope, projectRef, agentRef string, lock bool) (runtimecontract.RuntimeContextSnapshot, error) {
	skillsQuery, memoriesQuery := queryPromptContextSkills, queryPromptContextMemories
	if lock {
		skillsQuery, memoriesQuery = queryRuntimeContextSkills, queryRuntimeContextMemories
	}
	result := runtimecontract.RuntimeContextSnapshot{Schema: runtimecontract.RuntimeContextSchema,
		OrganizationRef: current.organizationRef, ProjectRef: projectRef, AgentRef: agentRef,
		Skills: []runtimecontract.RuntimeSkillBundle{}, Memories: []runtimecontract.RuntimeMemoryRecord{}}
	now := time.Now().UTC()
	if projectRef != "" {
		args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID,
			"agent_ref": agentRef, "project_ref": projectRef, "evaluated_at": now}
		rows, err := tx.Query(ctx, skillsQuery, args)
		if err != nil {
			return result, errs.ErrUnavailable
		}
		for rows.Next() {
			var skill runtimecontract.RuntimeSkillBundle
			var revision entity.SkillBundleRevision
			var raw []byte
			if err := rows.Scan(&skill.BindingRef, &skill.BindingVersion, &skill.BundleRef, &raw); err != nil || json.Unmarshal(raw, &revision) != nil || revision.ScannedAt == nil {
				rows.Close()
				return result, errs.ErrConflict
			}
			skill.RevisionRef, skill.Revision, skill.Digest = revision.Ref, revision.Revision, revision.Digest
			skill.Name, skill.Description = revision.Name, revision.Description
			skill.ScanEngine, skill.ScanDigest, skill.ScannedAt = revision.ScanEngine, revision.ScanDigest, revision.ScannedAt.UTC()
			skill.Provenance = runtimeContextProvenance(revision.Provenance)
			for _, file := range revision.Files {
				skill.Files = append(skill.Files, runtimecontract.RuntimeSkillFile{Path: file.Path, ArtifactRef: file.ArtifactRef,
					ArtifactRevision: file.ArtifactRevision, Digest: file.Digest, SizeBytes: file.SizeBytes})
			}
			result.Skills = append(result.Skills, skill)
		}
		rows.Close()
		if rows.Err() != nil || len(result.Skills) > runtimecontract.MaximumContextSkills {
			return result, errs.ErrConflict
		}
		rows, err = tx.Query(ctx, memoriesQuery, args)
		if err != nil {
			return result, errs.ErrUnavailable
		}
		for rows.Next() {
			var memory runtimecontract.RuntimeMemoryRecord
			var revision entity.MemoryRecordRevision
			var raw []byte
			if err := rows.Scan(&memory.BindingRef, &memory.BindingVersion, &memory.RecordRef, &raw); err != nil || json.Unmarshal(raw, &revision) != nil || revision.Redacted {
				rows.Close()
				return result, errs.ErrConflict
			}
			memory.RevisionRef, memory.Revision, memory.Digest = revision.Ref, revision.Revision, revision.Digest
			memory.Title, memory.Summary, memory.RetentionUntil = revision.Title, revision.Summary, revision.RetentionUntil.UTC()
			memory.Provenance = runtimeContextProvenance(revision.Provenance)
			result.Memories = append(result.Memories, memory)
		}
		rows.Close()
		if rows.Err() != nil || len(result.Memories) > runtimecontract.MaximumContextMemories {
			return result, errs.ErrConflict
		}
	}
	var err error
	result.Digest, err = result.ComputeDigest()
	if err != nil || result.ValidateFor(runtimecontract.RunnerInput{OrganizationRef: result.OrganizationRef, ProjectRef: projectRef, AgentRef: agentRef}, now) != nil {
		return result, errs.ErrConflict
	}
	return result, nil
}
