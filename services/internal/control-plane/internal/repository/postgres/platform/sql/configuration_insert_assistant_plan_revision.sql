-- name: configuration_insert_assistant_plan_revision :one
INSERT INTO control_plane.assistant_plan_revisions(
    ref,organization_id,plan_id,revision,summary,operations,content_digest,created_by_kind,created_by_ref
) VALUES($1,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9)
RETURNING created_at
