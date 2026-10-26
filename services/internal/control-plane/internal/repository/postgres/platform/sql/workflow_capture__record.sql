-- name: workflow_capture__record :exec
WITH producer AS (
    SELECT node.id,node.organization_id,node.run_id,node.turn_id,node.attempt,node.workflow_business_attempt,
           revision.id AS revision_id,lease.id AS lease_id,lease.generation
    FROM control_plane.run_nodes node
    JOIN control_plane.runtime_revisions revision ON revision.node_id=node.id
        AND revision.organization_id=node.organization_id AND revision.run_id=node.run_id
        AND revision.root_run_id=node.root_run_id AND revision.turn_id=node.turn_id
        AND revision.agent_id=node.agent_id AND revision.attempt=node.attempt
    JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
        AND lease.organization_id=revision.organization_id AND lease.node_id=node.id
        AND lease.run_id=node.run_id AND lease.generation=revision.generation AND lease.state='COMPLETED'
    WHERE node.organization_id=@organization_id::uuid AND node.id=@node_id::uuid
        AND node.run_id=@run_id::uuid AND node.root_run_id=@root_run_id::uuid
        AND revision.id=@runtime_revision_id::uuid AND lease.id=@lease_id::uuid
        AND node.workflow_business_attempt BETWEEN 1 AND 4 AND node.workflow_captured_artifacts IS NULL
), captured AS (
    SELECT COALESCE(jsonb_agg(jsonb_build_object(
               'ref',artifact.ref,'revisionRef',artifact.revision_ref,'revision',artifact.revision,
               'digest',artifact.digest,'sizeBytes',artifact.size_bytes,'fileName',artifact.file_name,'mediaType',artifact.media_type
           ) ORDER BY artifact.ref),'[]'::jsonb) AS entries,count(*) AS item_count
    FROM control_plane.artifact_history artifact
    JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
        AND content.digest=artifact.digest AND content.size_bytes=artifact.size_bytes
    JOIN producer ON producer.id=artifact.node_id AND producer.organization_id=artifact.organization_id
        AND producer.run_id=artifact.run_id
    WHERE artifact.id=ANY(@artifact_ids::uuid[]) AND artifact.source='AGENT_RESULT' AND artifact.lifecycle_state='ACTIVE'
)
UPDATE control_plane.run_nodes node
SET workflow_captured_artifacts=jsonb_build_object(
        'schema','kodex.workflow-capture.v1','runtimeRevisionId',producer.revision_id::text,
        'leaseId',producer.lease_id::text,'generation',producer.generation,'turnId',producer.turn_id::text,
        'runtimeAttempt',producer.attempt,'businessAttempt',producer.workflow_business_attempt,'artifacts',captured.entries
    ),version=node.version+1
FROM producer,captured
WHERE node.id=producer.id AND captured.item_count=cardinality(@artifact_ids::uuid[])
    AND captured.item_count<=16 AND node.workflow_captured_artifacts IS NULL;
