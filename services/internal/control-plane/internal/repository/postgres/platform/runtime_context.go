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

// queryRuntimeContextSkills embeds bounded ordered enabled skill bindings with
// project/agent/revision visibility, published clean reviewed revision pins and SHARE locks. It
// returns immutable revision projections for the caller snapshot; visibility is not permission to
// execute arbitrary skill effects.
// queryRuntimeContextSkills встраивает bounded ordered enabled skill bindings с
// project/agent/revision visibility, published clean reviewed revision pins и SHARE locks.
// Возвращает immutable revision projections caller snapshot; visibility не разрешает произвольные
// skill effects.
//
//go:embed sql/runtime_context_skills.sql
var queryRuntimeContextSkills string

// queryRuntimeContextMemories embeds bounded enabled memory bindings visible to the actor, with
// active record, current/pinned retention, optional source-run visibility and SHARE locks on
// bindings/record/revisions. The consumer still rejects redacted revision projections and
// validates the complete snapshot.
// queryRuntimeContextMemories встраивает bounded enabled memory bindings, видимые actor, с active
// record, current/pinned retention, optional source-run visibility и SHARE locks
// bindings/record/revisions. Consumer отдельно отклоняет redacted revision projections и проверяет
// complete snapshot.
//
//go:embed sql/runtime_context_memories.sql
var queryRuntimeContextMemories string

// queryPromptContextSkills embeds the same bounded clean/reviewed published skill eligibility
// projection as runtime reads without row-lock clauses. Preview still uses canonical visibility;
// absence of locks is not a broader authority rule.
// queryPromptContextSkills встраивает ту же bounded clean/reviewed published skill eligibility
// projection, что runtime reads, без row-lock clauses. Preview сохраняет canonical visibility;
// отсутствие locks не расширяет authority rule.
//
//go:embed sql/prompt_context_skills.sql
var queryPromptContextSkills string

// queryPromptContextMemories embeds preview memory selection with the runtime
// active/retention/source visibility predicates and bounds, but no SHARE locks. It reads
// metadata/revision projection only and issues no memory-write or execution capability.
// queryPromptContextMemories встраивает preview memory selection с runtime active/retention/source
// visibility predicates и bounds, но без SHARE locks. Читает только metadata/revision projection,
// не выдаёт memory-write либо execution capability.
//
//go:embed sql/prompt_context_memories.sql
var queryPromptContextMemories string

// runtimeContextProvenance copies actor/source/revision/digest provenance into the runtime
// contract and normalizes its timestamp to UTC. This is a projection only, not validation of the
// source, signature or actor authority.
// runtimeContextProvenance копирует actor/source/revision/digest provenance в runtime contract и
// переводит timestamp в UTC. Это только projection, а не проверка source, signature либо actor
// authority.
func runtimeContextProvenance(p entity.ContextProvenance) runtimecontract.RuntimeContextProvenance {
	return runtimecontract.RuntimeContextProvenance{ActorRef: p.ActorRef, SourceKind: p.SourceKind, SourceRef: p.SourceRef,
		SourceRevision: p.SourceRevision, Digest: p.Digest, CreatedAt: p.CreatedAt.UTC()}
}

// runtimeContextSnapshot for project contexts first resolves actor identity from the server-owned
// run query, mapping any failure to Conflict, then builds a locking skill/memory snapshot. An
// empty project skips actor replacement; trusted tenant/run inputs and transaction commit remain
// caller responsibilities.
// runtimeContextSnapshot для project context сначала разрешает actor identity server-owned run
// query, отображая любую ошибку в Conflict, затем строит locking skill/memory snapshot. Empty
// project пропускает actor replacement; trusted tenant/run inputs и transaction commit остаются
// ответственностью caller.
func (repository *Repository) runtimeContextSnapshot(ctx context.Context, tx pgx.Tx, current scope, runRef, projectRef, agentRef string) (runtimecontract.RuntimeContextSnapshot, error) {
	if projectRef != "" {
		if err := tx.QueryRow(ctx, querySTTRuntimeActor, current.organizationID, runRef).Scan(
			&current.actorID, &current.actorRef, &current.actorName, &current.organizationRef); err != nil {
			return runtimecontract.RuntimeContextSnapshot{}, errs.ErrConflict
		}
	}
	return repository.runtimeContextSnapshotForActorMode(ctx, tx, current, projectRef, agentRef, true)
}

// runtimeContextSnapshotForActor builds a nonlocking preview snapshot for the already resolved
// actor while preserving tenant/project/agent arguments. It neither derives actor identity from
// payload nor creates a runtime revision or grant.
// runtimeContextSnapshotForActor строит nonlocking preview snapshot уже разрешённого actor,
// сохраняя tenant/project/agent arguments. Не выводит actor identity из payload и не создаёт
// runtime revision либо grant.
func (repository *Repository) runtimeContextSnapshotForActor(ctx context.Context, tx pgx.Tx, current scope, projectRef, agentRef string) (runtimecontract.RuntimeContextSnapshot, error) {
	return repository.runtimeContextSnapshotForActorMode(ctx, tx, current, projectRef, agentRef, false)
}

// runtimeContextSnapshotForActorMode chooses locking runtime or nonlocking preview queries, reads
// only eligible bounded project skills/memories, checks scan/JSON/scan timestamp/redaction and
// iterator limits, then computes and validates the complete contract digest at one UTC time. Empty
// project yields empty collections. Errors may accompany partial snapshots and must be discarded;
// no transaction is opened/committed or capability issued.
// runtimeContextSnapshotForActorMode выбирает locking runtime либо nonlocking preview queries,
// читает только eligible bounded project skills/memories, проверяет scan/JSON/scan
// timestamp/redaction и iterator limits, затем вычисляет и проверяет complete contract digest в
// одном UTC time. Empty project даёт empty collections. Errors могут сопровождать partial
// snapshots, которые caller отбрасывает; transaction не открывается/фиксируется, capability не
// выдаётся.
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
