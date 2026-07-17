-- name: interaction_capabilities__state :one
select status, expires_at
from matter_codex_interaction_capabilities
where token_hash = $1
