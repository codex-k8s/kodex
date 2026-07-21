select coalesce(revision.id, 0), coalesce(revision.digest, ''), coalesce(revision.manifest::text, '{}'),
	coalesce(revision.account_alias, ''), coalesce(revision.authorization_revision, ''),
	coalesce(revision.created_at, 'epoch'::timestamptz)
from matter_codex_agent_session_turns turn_row
left join matter_codex_runtime_revisions revision on revision.id = turn_row.runtime_revision_id
where turn_row.session_id = $1
	and turn_row.status = 'queued'
order by turn_row.created_at, turn_row.id
limit 1;
