-- name: workflow_pdlc__read_order :one
SELECT control_plane.workflow_pdlc_read_order(@organization_id::uuid,@write_receipt_ref,@head_receipt_ref,@content_receipt_ref);
