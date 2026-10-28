-- name: workflow_attempt__append :one
WITH completed AS (
    SELECT physical.id,physical.organization_id,physical.root_run_id,physical.turn_id,physical.attempt,
           COALESCE(physical.workflow_step_origin_node_id,physical.id) AS origin_id,
           physical.workflow_business_attempt,run.ref AS run_ref,revision.id AS revision_id,
           lease.id AS lease_id,lease.generation
    FROM control_plane.run_nodes physical
    JOIN control_plane.runs run ON run.id=physical.run_id AND run.organization_id=physical.organization_id
        AND run.root_run_id=physical.root_run_id
    JOIN control_plane.runtime_revisions revision ON revision.node_id=physical.id
        AND revision.organization_id=physical.organization_id AND revision.run_id=physical.run_id
        AND revision.root_run_id=physical.root_run_id AND revision.turn_id=physical.turn_id
        AND revision.agent_id=physical.agent_id AND revision.attempt=physical.attempt
    JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
        AND lease.organization_id=revision.organization_id AND lease.run_id=revision.run_id
        AND lease.node_id=revision.node_id AND lease.generation=revision.generation AND lease.state='COMPLETED'
    WHERE physical.organization_id=@organization_id::uuid AND physical.id=@node_id::uuid
        AND physical.root_run_id=@root_run_id::uuid AND physical.state IN ('SUCCEEDED','FAILED')
        AND revision.id=@runtime_revision_id::uuid AND lease.id=@lease_id::uuid
        AND physical.workflow_business_attempt BETWEEN 1 AND 4
        AND physical.workflow_captured_artifacts->>'runtimeRevisionId'=revision.id::text
        AND physical.workflow_captured_artifacts->>'leaseId'=lease.id::text
), outcome AS (
    SELECT completed.origin_id,completed.organization_id,completed.workflow_business_attempt,
           jsonb_build_object(
               'attempt',completed.workflow_business_attempt,'runRef',completed.run_ref,
               'runtimeRevisionId',completed.revision_id::text,'accepted',@accepted::boolean,
               'reason',@reason::text,'results',@results::jsonb,
               'provenance',jsonb_build_object(
                   'rootRunId',completed.root_run_id::text,'originNodeId',completed.origin_id::text,
                   'nodeId',completed.id::text,'leaseId',completed.lease_id::text,
                   'generation',completed.generation,'turnId',completed.turn_id::text,
                   'runtimeAttempt',completed.attempt
               )
           ) || CASE WHEN jsonb_array_length(@rework_subjects::jsonb)>0
               THEN jsonb_build_object('reworkSubjects',@rework_subjects::jsonb) ELSE '{}'::jsonb END AS entry
    FROM completed
)
UPDATE control_plane.run_nodes anchor
SET workflow_attempts=anchor.workflow_attempts || jsonb_build_array(outcome.entry),version=anchor.version+1
FROM outcome
WHERE anchor.id=outcome.origin_id AND anchor.organization_id=outcome.organization_id
    AND anchor.workflow_step_origin_node_id IS NULL AND anchor.workflow_business_attempt=1
    AND jsonb_array_length(anchor.workflow_attempts)=outcome.workflow_business_attempt-1
RETURNING outcome.entry;
