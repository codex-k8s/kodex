-- name: historical_secret_recovery_snapshot :one
SELECT jsonb_build_object(
    'secret',jsonb_build_array(secret.ref,secret.state,secret.version,secret.current_revision),
    'revisions',(SELECT jsonb_agg(jsonb_build_array(revision.revision,revision.state,revision.secret_uid,revision.secret_resource_version,revision.content_sha256) ORDER BY revision.revision)
                 FROM control_plane.runtime_secret_revisions revision WHERE revision.secret_id=secret.id),
    'legacy',(SELECT jsonb_build_array(operation.state,operation.claimant_id,operation.claim_generation,operation.target_revision)
              FROM control_plane.runtime_secret_operations operation WHERE operation.ref=$2 AND operation.secret_id=secret.id),
    'draft',(SELECT jsonb_build_array(operation.state,operation.claimant_id,operation.claim_generation,operation.target_revision,operation.cleanup_completed,operation.updated_at,draft.state,draft.published_revision)
             FROM control_plane.runtime_secret_draft_operations operation JOIN control_plane.runtime_secret_drafts draft ON draft.id=operation.draft_id
             WHERE operation.ref=$2 AND draft.secret_id=secret.id),
    'receipts',(SELECT count(*) FROM control_plane.idempotency_receipts),
    'audit',(SELECT count(*) FROM control_plane.audit_events),
    'outbox',(SELECT count(*) FROM control_plane.outbox_events)
)::text FROM control_plane.runtime_secrets secret WHERE secret.ref=$1;
