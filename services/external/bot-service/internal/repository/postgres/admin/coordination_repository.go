package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var _ adminrepo.CoordinationRepository = (*Repository)(nil)
var _ adminrepo.CoordinationPolicyPresetRepository = (*Repository)(nil)

func (repo *Repository) ApplyCoordinationPolicyPreset(ctx context.Context, projectID int64, topCoordinatorRoleID int64, waveCoordinatorRoleIDs []int64) error {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin apply coordination policy preset: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock($1)", projectID); err != nil {
		return fmt.Errorf("lock project policy: %w", err)
	}
	var revisionID int64
	var presetApplied bool
	err = tx.QueryRow(ctx, `
			select id, coalesce(settings->>'preset', '') = 'director-manager-v1'
			from matter_codex_policy_revisions where project_id = $1 and status = 'active'
		`, projectID).Scan(&revisionID, &presetApplied)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			insert into matter_codex_policy_revisions (project_id, version, status, activated_at)
			select $1, coalesce(max(version), 0) + 1, 'active', now()
			from matter_codex_policy_revisions where project_id = $1
			returning id
		`, projectID).Scan(&revisionID)
	}
	if err != nil {
		return fmt.Errorf("get policy revision for preset: %w", err)
	}
	if presetApplied {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `delete from matter_codex_role_capabilities where policy_revision_id = $1`, revisionID); err != nil {
		return fmt.Errorf("clear role capabilities: %w", err)
	}
	if _, err := tx.Exec(ctx, `delete from matter_codex_role_relationship_policies where policy_revision_id = $1`, revisionID); err != nil {
		return fmt.Errorf("clear role relationships: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into matter_codex_role_capabilities (policy_revision_id, role_id, capability)
		select $1, r.id, c.capability
		from matter_codex_agent_roles r
			cross join lateral unnest(array[
				'callbacks.return','memory.project.read','memory.role.read','memory.role.write',
				'work.project.read','work.own.update','sync.request'
		]::text[]) c(capability)
		where r.project_id = $2 and r.enabled
	`, revisionID, projectID); err != nil {
		return fmt.Errorf("insert baseline role capabilities: %w", err)
	}
	coordinators := append([]int64{topCoordinatorRoleID}, waveCoordinatorRoleIDs...)
	if _, err := tx.Exec(ctx, `
		insert into matter_codex_role_capabilities (policy_revision_id, role_id, capability)
		select $1, r.id, c.capability
		from matter_codex_agent_roles r
		cross join lateral unnest(array[
			'agents.start','callbacks.receive','owner_attention.request','memory.project.write',
			'work.project.manage','sync.receive'
		]::text[]) c(capability)
		where r.project_id = $2 and r.id = any($3::bigint[])
	`, revisionID, projectID, coordinators); err != nil {
		return fmt.Errorf("insert coordinator capabilities: %w", err)
	}
	if len(waveCoordinatorRoleIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into matter_codex_role_relationship_policies (policy_revision_id, source_role_id, action, target_role_id)
			select $1, $2, 'start', unnest($3::bigint[])
		`, revisionID, topCoordinatorRoleID, waveCoordinatorRoleIDs); err != nil {
			return fmt.Errorf("insert top coordinator start relationships: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			insert into matter_codex_role_relationship_policies (policy_revision_id, source_role_id, action, target_role_id)
			select $1, unnest($2::bigint[]), 'callback', $3
		`, revisionID, waveCoordinatorRoleIDs, topCoordinatorRoleID); err != nil {
			return fmt.Errorf("insert wave coordinator callbacks: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		insert into matter_codex_role_relationship_policies (policy_revision_id, source_role_id, action, target_role_id)
		select $1, source.id, 'request_sync', target.id
		from matter_codex_agent_roles source
		join matter_codex_agent_roles target on target.project_id = source.project_id and target.enabled and target.id <> source.id
		where source.project_id = $2 and source.enabled
	`, revisionID, projectID); err != nil {
		return fmt.Errorf("insert role sync relationships: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into matter_codex_role_relationship_policies (policy_revision_id, source_role_id, action, target_role_id)
		select $1, source.id, 'start', target.id
		from matter_codex_agent_roles source
		join matter_codex_agent_roles target on target.project_id = source.project_id and target.enabled
		where source.project_id = $2 and source.id = any($3::bigint[])
			and target.id <> $4 and not (target.id = any($3::bigint[]))
	`, revisionID, projectID, waveCoordinatorRoleIDs, topCoordinatorRoleID); err != nil {
		return fmt.Errorf("insert wave coordinator start relationships: %w", err)
	}
	if len(waveCoordinatorRoleIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into matter_codex_role_relationship_policies (policy_revision_id, source_role_id, action, target_role_id)
			select $1, worker.id, 'callback', coordinator.id
			from matter_codex_agent_roles worker
			cross join unnest($3::bigint[]) c(role_id)
			join matter_codex_agent_roles coordinator on coordinator.id = c.role_id
			where worker.project_id = $2 and worker.enabled
				and worker.id <> $4 and not (worker.id = any($3::bigint[]))
		`, revisionID, projectID, waveCoordinatorRoleIDs, topCoordinatorRoleID); err != nil {
			return fmt.Errorf("insert worker callback relationships: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		update matter_codex_policy_revisions
		set settings = settings || '{"preset":"director-manager-v1"}'::jsonb
		where id = $1
	`, revisionID); err != nil {
		return fmt.Errorf("mark coordination policy preset: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit coordination policy preset: %w", err)
	}
	return nil
}

func (repo *Repository) EnsureTurnProcess(ctx context.Context, input adminrepo.EnsureTurnProcessInput) (entity.ProcessContext, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.ProcessContext{}, fmt.Errorf("begin ensure turn process: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock($1)", input.ProjectID); err != nil {
		return entity.ProcessContext{}, fmt.Errorf("lock project policy: %w", err)
	}
	var processID int64
	if input.ParentTurnID > 0 {
		err = tx.QueryRow(ctx, `select process_run_id from matter_codex_process_turns where turn_id = $1`, input.ParentTurnID).Scan(&processID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return entity.ProcessContext{}, fmt.Errorf("get parent turn process: %w", err)
		}
	}
	if processID == 0 && input.ParentTurnID == 0 && strings.TrimSpace(input.InitiatorUserID) != "" {
		err = tx.QueryRow(ctx, `
			select process.id
			from matter_codex_process_runs process
			join matter_codex_owner_attention_requests attention on attention.process_run_id = process.id
			join matter_codex_agent_session_turns attention_turn on attention_turn.id = attention.turn_id
			where process.project_id = $1
				and attention.request_kind = 'generic'
				and attention_turn.mattermost_channel_id = $2
				and attention_turn.mattermost_root_post_id = $3
				and process.root_initiator_user_id = $4
				and attention.status = 'open'
			order by attention.updated_at desc
			limit 1
			for update of process
		`, input.ProjectID, input.MattermostChannelID, input.MattermostRootPostID, input.InitiatorUserID).Scan(&processID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return entity.ProcessContext{}, fmt.Errorf("find process awaiting owner response: %w", err)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			processID = 0
		} else {
			if _, err := tx.Exec(ctx, `
				update matter_codex_owner_attention_requests attention
				set status = 'resolved', resolved_at = now(), resolved_by_user_id = $2,
					resolved_by_post_id = $3, updated_at = now()
				from matter_codex_agent_session_turns attention_turn
				where attention.process_run_id = $1 and attention.status = 'open'
					and attention.request_kind = 'generic'
					and attention_turn.id = attention.turn_id
					and attention_turn.mattermost_channel_id = $4
					and attention_turn.mattermost_root_post_id = $5
			`, processID, input.InitiatorUserID, input.TriggerPostID,
				input.MattermostChannelID, input.MattermostRootPostID); err != nil {
				return entity.ProcessContext{}, fmt.Errorf("resolve owner attention: %w", err)
			}
		}
	}
	if processID == 0 {
		var revisionID int64
		err = tx.QueryRow(ctx, `select id from matter_codex_policy_revisions where project_id = $1 and status = 'active'`, input.ProjectID).Scan(&revisionID)
		if errors.Is(err, pgx.ErrNoRows) {
			if err := tx.QueryRow(ctx, `
				insert into matter_codex_policy_revisions (project_id, version, status, activated_at)
				select $1, coalesce(max(version), 0) + 1, 'active', now()
				from matter_codex_policy_revisions where project_id = $1
				returning id
			`, input.ProjectID).Scan(&revisionID); err != nil {
				return entity.ProcessContext{}, fmt.Errorf("create project policy revision: %w", err)
			}
		} else if err != nil {
			return entity.ProcessContext{}, fmt.Errorf("get active project policy revision: %w", err)
		}
		publicID := fmt.Sprintf("process-%d", input.TurnID)
		if err := tx.QueryRow(ctx, `
			insert into matter_codex_process_runs (
				public_id, project_id, policy_revision_id, root_role_id,
				root_initiator_user_id, root_initiator_user_name, root_trigger_post_id,
				root_channel_id, root_thread_post_id
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			returning id
		`, publicID, input.ProjectID, revisionID, input.RoleID, input.InitiatorUserID,
			input.InitiatorUserName, input.TriggerPostID, input.MattermostChannelID,
			input.MattermostRootPostID).Scan(&processID); err != nil {
			return entity.ProcessContext{}, fmt.Errorf("create process run: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		insert into matter_codex_process_turns (turn_id, process_run_id, parent_turn_id, launch_post_id)
		values ($1,$2,nullif($3,0),$4)
		on conflict (turn_id) do nothing
	`, input.TurnID, processID, input.ParentTurnID, input.TriggerPostID); err != nil {
		return entity.ProcessContext{}, fmt.Errorf("bind turn to process: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into matter_codex_work_claims (process_run_id, turn_id, role_id, status)
		values ($1,$2,$3,'active')
		on conflict (turn_id) do update set status = 'active', updated_at = now()
	`, processID, input.TurnID, input.RoleID); err != nil {
		return entity.ProcessContext{}, fmt.Errorf("create turn work claim: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update matter_codex_process_runs process
		set status = case when exists (
				select 1 from matter_codex_owner_attention_requests attention
				where attention.process_run_id = process.id and attention.status = 'open'
			) then 'waiting_owner' else 'running' end,
			updated_at = now(), finished_at = null
		where process.id = $1
	`, processID); err != nil {
		return entity.ProcessContext{}, fmt.Errorf("mark process running: %w", err)
	}
	item, err := scanProcessContext(tx.QueryRow(ctx, processContextSQL, input.TurnID))
	if err != nil {
		return entity.ProcessContext{}, fmt.Errorf("scan ensured turn process: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ProcessContext{}, fmt.Errorf("commit ensure turn process: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetTurnProcess(ctx context.Context, turnID int64) (entity.ProcessContext, error) {
	item, err := scanProcessContext(repo.db.QueryRow(ctx, processContextSQL, turnID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ProcessContext{}, adminrepo.ErrNotFound
	}
	if err != nil {
		return entity.ProcessContext{}, fmt.Errorf("get turn process: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetTurnLineage(ctx context.Context, turnID int64) ([]entity.ProcessLineageStep, error) {
	rows, err := repo.db.Query(ctx, `
		with recursive lineage as (
			select pt.turn_id, pt.parent_turn_id, pt.launch_post_id, 0 as depth
			from matter_codex_process_turns pt
			where pt.turn_id = $1
			union all
			select parent.turn_id, parent.parent_turn_id, parent.launch_post_id, child.depth + 1
			from matter_codex_process_turns parent
			join lineage child on child.parent_turn_id = parent.turn_id
		)
		select lineage.turn_id, coalesce(lineage.parent_turn_id, 0), sessions.role_id,
			roles.name, coalesce(nullif(roles.bot_identity, ''), roles.name), turns.run_id,
			lineage.launch_post_id
		from lineage
		join matter_codex_agent_session_turns turns on turns.id = lineage.turn_id
		join matter_codex_agent_sessions sessions on sessions.id = turns.session_id
		join matter_codex_agent_roles roles on roles.id = sessions.role_id
		order by lineage.depth desc
	`, turnID)
	if err != nil {
		return nil, fmt.Errorf("get turn lineage: %w", err)
	}
	defer rows.Close()
	items := make([]entity.ProcessLineageStep, 0)
	for rows.Next() {
		var item entity.ProcessLineageStep
		if err := rows.Scan(&item.TurnID, &item.ParentTurnID, &item.RoleID, &item.RoleName,
			&item.BotIdentity, &item.RunID, &item.LaunchPostID); err != nil {
			return nil, fmt.Errorf("scan turn lineage: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate turn lineage: %w", err)
	}
	if len(items) == 0 {
		return nil, adminrepo.ErrNotFound
	}
	return items, nil
}

func (repo *Repository) IsRoleCapabilityAllowed(ctx context.Context, turnID int64, projectID int64, roleID int64, capability string) (bool, error) {
	var allowed bool
	err := repo.db.QueryRow(ctx, `
		with selected_policy as (
			select candidate.id
			from (
				select pr.policy_revision_id as id, 0 as priority
				from matter_codex_process_turns pt
				join matter_codex_process_runs pr on pr.id = pt.process_run_id
				where pt.turn_id = $1
				union all
				select id, 1 as priority
				from matter_codex_policy_revisions
				where project_id = $2 and status = 'active'
			) candidate
			order by candidate.priority
			limit 1
		)
		select exists (
			select 1 from matter_codex_role_capabilities c
			join selected_policy p on p.id = c.policy_revision_id
			where c.role_id = $3 and c.capability = $4 and c.enabled
		)
	`, turnID, projectID, roleID, capability).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("check role capability: %w", err)
	}
	return allowed, nil
}

func (repo *Repository) IsRoleRelationshipAllowed(ctx context.Context, turnID int64, projectID int64, sourceRoleID int64, action string, targetRoleID int64) (bool, error) {
	var allowed bool
	err := repo.db.QueryRow(ctx, `
		with selected_policy as (
			select candidate.id
			from (
				select pr.policy_revision_id as id, 0 as priority
				from matter_codex_process_turns pt
				join matter_codex_process_runs pr on pr.id = pt.process_run_id
				where pt.turn_id = $1
				union all
				select id, 1 as priority
				from matter_codex_policy_revisions
				where project_id = $2 and status = 'active'
			) candidate
			order by candidate.priority
			limit 1
		)
		select exists (
			select 1 from matter_codex_role_relationship_policies r
			join selected_policy p on p.id = r.policy_revision_id
			where r.source_role_id = $3 and r.action = $4 and r.target_role_id = $5 and r.enabled
		)
	`, turnID, projectID, sourceRoleID, action, targetRoleID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("check role relationship: %w", err)
	}
	return allowed, nil
}

func (repo *Repository) UpdateWorkClaim(ctx context.Context, input adminrepo.UpdateWorkClaimInput) (entity.WorkClaim, error) {
	row := repo.db.QueryRow(ctx, `
		update matter_codex_work_claims
		set summary = case when $2 = '' then summary else $2 end,
			domains = case when cardinality(coalesce($3::text[], '{}'::text[])) = 0 then domains else $3 end,
			resource_keys = case when cardinality(coalesce($4::text[], '{}'::text[])) = 0 then resource_keys else $4 end,
			links = case when $5::jsonb = '[]'::jsonb then links else $5::jsonb end,
			status = case when $6 = '' then status else $6 end,
			updated_at = now()
		where turn_id = $1
		returning id, process_run_id, turn_id, role_id, summary, domains, resource_keys, links, status, updated_at
	`, input.TurnID, input.Summary, input.Domains, input.ResourceKeys, marshalStrings(input.Links), input.Status)
	item, err := scanWorkClaim(row, "")
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.WorkClaim{}, adminrepo.ErrNotFound
	}
	if err != nil {
		return entity.WorkClaim{}, fmt.Errorf("update work claim: %w", err)
	}
	return item, nil
}

func (repo *Repository) ListActiveWork(ctx context.Context, processRunID int64, projectID int64, limit int) ([]entity.WorkClaim, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := repo.db.Query(ctx, `
		select w.id, w.process_run_id, w.turn_id, w.role_id, r.name, w.summary,
			w.domains, w.resource_keys, w.links, w.status, w.updated_at
		from matter_codex_work_claims w
		join matter_codex_process_runs p on p.id = w.process_run_id
		join matter_codex_agent_roles r on r.id = w.role_id
		where w.status = 'active' and (($1 > 0 and w.process_run_id = $1) or ($1 = 0 and p.project_id = $2))
		order by w.updated_at desc limit $3
	`, processRunID, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("list active work: %w", err)
	}
	defer rows.Close()
	items := make([]entity.WorkClaim, 0)
	for rows.Next() {
		item, err := scanWorkClaim(rows, "with-role")
		if err != nil {
			return nil, fmt.Errorf("scan active work: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (repo *Repository) RememberMemory(ctx context.Context, input adminrepo.RememberMemoryInput) (entity.MemoryRecord, error) {
	sum := sha256.Sum256([]byte(strings.TrimSpace(input.Title) + "\x00" + strings.TrimSpace(input.Content)))
	hash := hex.EncodeToString(sum[:])
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.MemoryRecord{}, fmt.Errorf("begin remember memory: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var recordID int64
	var roleID any
	if input.Scope == "role" {
		roleID = input.RoleID
	}
	if err := tx.QueryRow(ctx, `
		insert into matter_codex_memory_records (
			project_id, scope, role_id, importance, created_by_role_id, source_turn_id, source_post_id
		) values ($1,$2,$3,$4,$5,nullif($6,0),$7)
		returning id
	`, input.ProjectID, input.Scope, roleID, input.Importance, input.CreatedByRoleID, input.SourceTurnID, input.SourcePostID).Scan(&recordID); err != nil {
		return entity.MemoryRecord{}, fmt.Errorf("create memory record: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into matter_codex_memory_record_versions (record_id, version, title, content, content_hash)
		values ($1,1,$2,$3,$4)
	`, recordID, input.Title, input.Content, hash); err != nil {
		return entity.MemoryRecord{}, fmt.Errorf("create memory version: %w", err)
	}
	item, err := scanMemoryRecord(tx.QueryRow(ctx, memoryRecordSQL+` where m.id = $1`, recordID))
	if err != nil {
		return entity.MemoryRecord{}, fmt.Errorf("scan remembered memory: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.MemoryRecord{}, fmt.Errorf("commit remember memory: %w", err)
	}
	return item, nil
}

func (repo *Repository) SearchMemory(ctx context.Context, input adminrepo.SearchMemoryInput) ([]entity.MemoryRecord, error) {
	if input.Limit <= 0 || input.Limit > 50 {
		input.Limit = 10
	}
	rows, err := repo.db.Query(ctx, memoryRecordSQL+`
		where m.project_id = $1 and m.status = 'active'
			and (m.scope = 'project' or (m.scope = 'role' and m.role_id = $2))
			and ($3 = '' or v.search_document @@ plainto_tsquery('simple', $3))
		order by case when $3 = '' then 0 else ts_rank(v.search_document, plainto_tsquery('simple', $3)) end desc,
			m.updated_at desc
		limit $4
	`, input.ProjectID, input.RoleID, input.Query, input.Limit)
	if err != nil {
		return nil, fmt.Errorf("search memory: %w", err)
	}
	defer rows.Close()
	items := make([]entity.MemoryRecord, 0)
	for rows.Next() {
		item, err := scanMemoryRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("scan memory search result: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (repo *Repository) CreateOwnerAttention(ctx context.Context, input adminrepo.CreateOwnerAttentionInput) (entity.OwnerAttentionRequest, bool, error) {
	row := repo.db.QueryRow(ctx, `
		insert into matter_codex_owner_attention_requests (
			request_kind, process_run_id, turn_id, severity, summary, options, recommendation,
			evidence_links, pause_scope, idempotency_key
		) values ('generic',$1,$2,$3,$4,$5::jsonb,$6,$7::jsonb,$8,$9)
		on conflict (process_run_id, idempotency_key) where request_kind = 'generic' do nothing
		returning id, process_run_id, turn_id, severity, summary, options, recommendation,
			evidence_links, pause_scope, idempotency_key, mattermost_post_id, status
	`, input.ProcessRunID, input.TurnID, input.Severity, input.Summary, marshalStrings(input.Options),
		input.Recommendation, marshalStrings(input.EvidenceLinks), input.PauseScope, input.IdempotencyKey)
	item, err := scanOwnerAttention(row)
	created := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		item, err = scanOwnerAttention(repo.db.QueryRow(ctx, `
			select id, process_run_id, turn_id, severity, summary, options, recommendation,
				evidence_links, pause_scope, idempotency_key, mattermost_post_id, status
			from matter_codex_owner_attention_requests
			where process_run_id = $1 and idempotency_key = $2 and request_kind = 'generic'
		`, input.ProcessRunID, input.IdempotencyKey))
	}
	if err != nil {
		return entity.OwnerAttentionRequest{}, false, fmt.Errorf("create owner attention: %w", err)
	}
	if _, err := repo.db.Exec(ctx, `
		update matter_codex_process_runs
		set status = 'waiting_owner', updated_at = now(), finished_at = null
		where id = $1
	`, input.ProcessRunID); err != nil {
		return entity.OwnerAttentionRequest{}, false, fmt.Errorf("mark process waiting for owner: %w", err)
	}
	return item, created, nil
}

func (repo *Repository) SetOwnerAttentionPost(ctx context.Context, id int64, postID string) (entity.OwnerAttentionRequest, error) {
	item, err := scanOwnerAttention(repo.db.QueryRow(ctx, `
		update matter_codex_owner_attention_requests set mattermost_post_id = $2, updated_at = now()
		where id = $1 and request_kind = 'generic'
		returning id, process_run_id, turn_id, severity, summary, options, recommendation,
			evidence_links, pause_scope, idempotency_key, mattermost_post_id, status
	`, id, postID))
	if err != nil {
		return entity.OwnerAttentionRequest{}, fmt.Errorf("set owner attention post: %w", err)
	}
	return item, nil
}

func (repo *Repository) ReconcileProcessRun(ctx context.Context, turnID int64) error {
	tag, err := repo.db.Exec(ctx, `
		with selected_process as (
			select process_run_id
			from matter_codex_process_turns
			where turn_id = $1
		), process_state as (
			select process.id,
				exists (
					select 1
					from matter_codex_owner_attention_requests attention
					where attention.process_run_id = process.id and attention.status = 'open'
				) as has_open_attention,
				exists (
					select 1
					from matter_codex_process_turns process_turn
					join matter_codex_agent_session_turns turn on turn.id = process_turn.turn_id
					where process_turn.process_run_id = process.id
						and turn.status in ('queued', 'running', 'capacity_retry')
				) as has_active_turn
			from matter_codex_process_runs process
			join selected_process selected on selected.process_run_id = process.id
		)
		update matter_codex_process_runs process
		set status = case
				when state.has_open_attention then 'waiting_owner'
				when state.has_active_turn then 'running'
				else 'completed'
			end,
			finished_at = case
				when state.has_open_attention or state.has_active_turn then null
				else coalesce(process.finished_at, now())
			end,
			updated_at = now()
		from process_state state
		where process.id = state.id
	`, turnID)
	if err != nil {
		return fmt.Errorf("reconcile process run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return adminrepo.ErrNotFound
	}
	return nil
}

const processContextSQL = `
	select p.id, p.public_id, p.project_id, p.policy_revision_id, p.root_role_id,
		p.root_initiator_user_id, p.root_initiator_user_name, p.root_trigger_post_id,
		p.root_channel_id, p.root_thread_post_id, p.status
	from matter_codex_process_turns t join matter_codex_process_runs p on p.id = t.process_run_id
	where t.turn_id = $1
`

const memoryRecordSQL = `
	select m.id, m.project_id, m.scope, coalesce(m.role_id, 0), m.status, m.importance,
		v.title, v.content, v.version, m.source_post_id, m.created_by_role_id, m.updated_at
	from matter_codex_memory_records m
	join lateral (
		select * from matter_codex_memory_record_versions mv
		where mv.record_id = m.id order by mv.version desc limit 1
	) v on true
`

type rowScanner interface{ Scan(...any) error }

func scanProcessContext(row rowScanner) (entity.ProcessContext, error) {
	var item entity.ProcessContext
	err := row.Scan(&item.ProcessRunID, &item.ProcessPublicID, &item.ProjectID, &item.PolicyRevisionID,
		&item.RootRoleID, &item.RootInitiatorUserID, &item.RootInitiatorUserName,
		&item.RootTriggerPostID, &item.RootChannelID, &item.RootThreadPostID, &item.Status)
	return item, err
}

func scanWorkClaim(row rowScanner, mode string) (entity.WorkClaim, error) {
	var item entity.WorkClaim
	var rawLinks []byte
	var err error
	if mode == "with-role" {
		err = row.Scan(&item.ID, &item.ProcessRunID, &item.TurnID, &item.RoleID, &item.RoleName,
			&item.Summary, &item.Domains, &item.ResourceKeys, &rawLinks, &item.Status, &item.UpdatedAt)
	} else {
		err = row.Scan(&item.ID, &item.ProcessRunID, &item.TurnID, &item.RoleID,
			&item.Summary, &item.Domains, &item.ResourceKeys, &rawLinks, &item.Status, &item.UpdatedAt)
	}
	if err == nil {
		err = json.Unmarshal(rawLinks, &item.Links)
	}
	return item, err
}

func scanMemoryRecord(row rowScanner) (entity.MemoryRecord, error) {
	var item entity.MemoryRecord
	err := row.Scan(&item.ID, &item.ProjectID, &item.Scope, &item.RoleID, &item.Status,
		&item.Importance, &item.Title, &item.Content, &item.Version, &item.SourcePostID,
		&item.CreatedByRoleID, &item.UpdatedAt)
	return item, err
}

func scanOwnerAttention(row rowScanner) (entity.OwnerAttentionRequest, error) {
	var item entity.OwnerAttentionRequest
	var options, evidence []byte
	err := row.Scan(&item.ID, &item.ProcessRunID, &item.TurnID, &item.Severity, &item.Summary,
		&options, &item.Recommendation, &evidence, &item.PauseScope, &item.IdempotencyKey,
		&item.MattermostPostID, &item.Status)
	if err == nil {
		err = json.Unmarshal(options, &item.Options)
	}
	if err == nil {
		err = json.Unmarshal(evidence, &item.EvidenceLinks)
	}
	return item, err
}

func marshalStrings(values []string) string {
	if values == nil {
		values = []string{}
	}
	body, _ := json.Marshal(values)
	return string(body)
}
