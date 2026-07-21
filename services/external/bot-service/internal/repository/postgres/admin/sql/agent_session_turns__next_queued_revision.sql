select revision.id, revision.digest, revision.manifest::text,
	revision.account_alias, revision.authorization_revision, revision.created_at
from matter_codex_agent_session_turns turn_row
join matter_codex_runtime_revisions revision on revision.id = turn_row.runtime_revision_id
where turn_row.session_id = $1
	and turn_row.status = 'queued'
order by turn_row.created_at, turn_row.id
limit 1;
