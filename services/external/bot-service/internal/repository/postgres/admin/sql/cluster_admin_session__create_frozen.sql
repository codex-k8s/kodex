-- name: cluster_admin_session__create_frozen :one
select matter_codex_create_frozen_cluster_admin_session(
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17::jsonb
);
