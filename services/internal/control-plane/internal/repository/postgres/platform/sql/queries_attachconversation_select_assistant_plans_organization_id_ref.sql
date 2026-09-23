-- name: queries_attachconversation_select_assistant_plans_organization_id_ref :one
SELECT p.ref,p.summary,p.state,p.version,p.current_revision,p.validated_revision,
       p.content_digest,p.validation_problems,p.operations,p.created_at,p.validated_at,p.applied_at,
       COALESCE(r.ref,''),COALESCE(r.plan_revision,0),COALESCE(r.outcome,''),
       COALESCE(r.operation_receipts,'[]'::jsonb),COALESCE(r.conflict_diff,'[]'::jsonb),
       COALESCE(r.audit_refs,'{}'::text[]),COALESCE(r.created_resource_refs,'{}'::text[]),r.created_at
FROM control_plane.assistant_plans p
JOIN control_plane.assistant_conversations c ON c.latest_plan_id=p.id
LEFT JOIN LATERAL (
    SELECT receipt.ref,receipt.plan_revision,receipt.outcome,receipt.operation_receipts,
           receipt.conflict_diff,receipt.audit_refs,receipt.created_resource_refs,receipt.created_at
    FROM control_plane.assistant_plan_receipts receipt
    WHERE receipt.organization_id=c.organization_id AND receipt.plan_id=p.id
    ORDER BY receipt.created_at DESC,receipt.id DESC
    LIMIT 1
) r ON TRUE
WHERE c.organization_id=$1::uuid AND c.ref=$2
