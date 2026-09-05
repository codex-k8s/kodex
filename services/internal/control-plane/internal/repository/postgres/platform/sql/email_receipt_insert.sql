-- name: email_receipt_insert :exec
INSERT INTO control_plane.email_effect_receipts(ref,organization_id,invocation_id,authorization_ref,
    external_receipt_ref,external_receipt_digest,semantic_input_digest,effect_key,mailbox_ref,configuration_revision)
SELECT $1,a.organization_id,a.invocation_id,a.ref,$3,$4,$5,$6,$7,$8
FROM control_plane.email_authorizations a WHERE a.ref=$2 AND a.invocation_id IS NOT NULL;
