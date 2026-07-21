-- name: artifact_versions__set_state :one
update matter_codex_artifact_versions
set state = $3, error_code = $4, updated_at = now()
where id = $1 and state = $2
returning id;
