-- name: configuration_insert_assistant_plan_receipt :one
INSERT INTO control_plane.assistant_plan_receipts(
    ref,organization_id,plan_id,plan_revision,outcome,operation_receipts,conflict_diff,
    audit_refs,created_resource_refs,applied_by
) VALUES($1,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10::uuid)
RETURNING created_at
