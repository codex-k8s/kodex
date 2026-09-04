-- name: interaction_insert_message_receipt :exec
INSERT INTO control_plane.interaction_message_receipts (
    ref,
    organization_id,
    project_id,
    connection_id,
    grant_id,
    root_run_id,
    gate_id,
    external_event_digest,
    external_user_digest,
    outcome,
    decision,
    identity_id,
    subject_id
)
VALUES (
    @receipt_ref,
    @organization_id::uuid,
    NULLIF(@project_id, '')::uuid,
    @connection_id::uuid,
    NULLIF(@grant_id, '')::uuid,
    (SELECT run.id FROM control_plane.runs run WHERE run.organization_id = @organization_id::uuid AND run.ref = NULLIF(@root_run_ref, '')),
    NULLIF(@gate_id, '')::uuid,
    @external_event_digest,
    @external_user_digest,
    @outcome,
    NULLIF(@decision, ''),
    @identity_id::uuid,
    @subject_id::uuid
)
