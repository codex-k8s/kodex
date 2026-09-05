-- name: interaction_insert_message_receipt :exec
WITH accepted AS (
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
    subject_id,
    external_team_ref,
    external_channel_ref,
    external_root_post_ref
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
    @subject_id::uuid,
    @external_team_ref,
    @external_channel_ref,
    @external_root_post_ref
)
RETURNING *
)
INSERT INTO control_plane.interaction_deliveries
    (ref,organization_id,project_id,connection_id,grant_id,root_run_id,capability_key,message_key,
     template_data,state,acceptance_receipt_id,external_team_ref,external_channel_ref,target_root_post_ref)
SELECT 'idlv_'||replace(gen_random_uuid()::text,'-',''),organization_id,project_id,connection_id,grant_id,root_run_id,
    'mattermost.acknowledgements',
    CASE outcome WHEN 'RUN_STARTED' THEN 'MATTERMOST_RUN_ACCEPTED' WHEN 'GATE_RESOLVED' THEN 'MATTERMOST_GATE_RESOLVED'
        WHEN 'STALE' THEN 'MATTERMOST_GATE_STALE' ELSE 'MATTERMOST_GATE_COMMAND_HELP' END,
    jsonb_build_object('receiptRef',ref),'DUE',id,external_team_ref,external_channel_ref,external_root_post_ref
FROM accepted WHERE project_id IS NOT NULL AND grant_id IS NOT NULL AND root_run_id IS NOT NULL;
