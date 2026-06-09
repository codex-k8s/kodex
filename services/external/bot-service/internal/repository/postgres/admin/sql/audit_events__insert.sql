-- name: audit_events__insert :exec
insert into matter_codex_audit_events(event_type, actor_user_id, actor_user, resource_type, resource_name, summary)
values ($1, $2, $3, $4, $5, $6);
