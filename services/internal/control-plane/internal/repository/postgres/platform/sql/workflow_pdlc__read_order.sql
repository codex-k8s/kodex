-- name: workflow_pdlc__read_order :one
SELECT EXISTS (
    SELECT 1
    FROM control_plane.integration_effect_receipts written
    JOIN control_plane.integration_invocations writer ON writer.id=written.invocation_id
        AND writer.organization_id=written.organization_id AND writer.effect_receipt_id=written.id
        AND writer.state='SUCCEEDED'
    JOIN control_plane.integration_effect_receipts head ON head.organization_id=written.organization_id
        AND head.ref=@head_receipt_ref
    JOIN control_plane.integration_invocations head_read ON head_read.id=head.invocation_id
        AND head_read.organization_id=head.organization_id AND head_read.effect_receipt_id=head.id
        AND head_read.state='SUCCEEDED' AND head_read.operation='github.pull_request.read'
    JOIN control_plane.integration_effect_receipts content ON content.organization_id=written.organization_id
        AND content.ref=@content_receipt_ref
    JOIN control_plane.integration_invocations content_read ON content_read.id=content.invocation_id
        AND content_read.organization_id=content.organization_id AND content_read.effect_receipt_id=content.id
        AND content_read.state='SUCCEEDED' AND content_read.operation='github.repository.content.read'
    WHERE written.organization_id=@organization_id::uuid AND written.ref=@write_receipt_ref
        AND writer.operation IN('github.repository.content.create','github.repository.content.update')
        AND head_read.created_at>=written.created_at AND head.created_at>=head_read.created_at
        AND content_read.created_at>=head.created_at AND content.created_at>=content_read.created_at
);
