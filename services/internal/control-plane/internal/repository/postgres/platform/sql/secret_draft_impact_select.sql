-- name: secret_draft_impact_select :exec
UPDATE control_plane.runtime_secret_draft_impact_items
SET outcome=CASE WHEN ref=ANY(@selected::text[]) THEN 'PENDING' ELSE 'NOT_SELECTED' END WHERE plan_id=@plan_id::uuid;
