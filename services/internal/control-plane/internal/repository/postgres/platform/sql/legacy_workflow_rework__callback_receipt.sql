-- name: legacy_workflow_rework__callback_receipt :exec
WITH origin AS MATERIALIZED (
    SELECT child.id,child.organization_id,child.project_id,child.ref,child.version,child.state,child.result_summary,
        witness.callback_edge_id AS edge_id,first_node.ref AS node_ref,witness.final_node_id AS node_id,
        witness.revision_ref,witness.runtime_attempt AS attempt,witness.generation
    FROM control_plane.legacy_rework_callback_origin(@organization_id::uuid,@child_run_id::uuid,@runtime_revision_id::uuid) witness
    JOIN control_plane.runs child ON child.id=@child_run_id::uuid AND child.organization_id=@organization_id::uuid
    JOIN control_plane.run_nodes first_node ON first_node.id=witness.original_node_id AND first_node.organization_id=child.organization_id
    WHERE witness.eligible AND witness.callback_edge_id=@callback_edge_id::uuid
), direct_artifacts AS (
    SELECT jsonb_build_object('ref',artifact.ref,'revision',artifact.revision,'version',artifact.version,
        'digest',artifact.digest,'sizeBytes',artifact.size_bytes,'fileName',artifact.file_name,'mediaType',artifact.media_type,
        'source',artifact.source,'runtimeRevisionRef',origin.revision_ref,'attempt',origin.attempt,'generation',origin.generation) AS pin
    FROM origin JOIN control_plane.artifacts artifact ON artifact.organization_id=origin.organization_id
        AND artifact.run_id=origin.id AND artifact.node_id=origin.node_id AND artifact.ref=ANY(@artifact_refs::text[])
        AND artifact.source='AGENT_RESULT'
), pins AS (
    SELECT pin FROM direct_artifacts UNION
    SELECT received FROM origin CROSS JOIN LATERAL control_plane.runtime_received_result_artifacts(origin.organization_id,origin.node_id) received
), snapshot AS (
    SELECT origin.id,origin.edge_id,jsonb_build_object('format',1,'childRunRef',origin.ref,'childVersion',origin.version,
        'nodeRef',origin.node_ref,'state',origin.state,'resultSummary',origin.result_summary,
        'artifacts',COALESCE((SELECT jsonb_agg(pin ORDER BY pin->>'ref') FROM pins),'[]'::jsonb)) AS value
    FROM origin WHERE cardinality(@artifact_refs::text[])<=16
        AND cardinality(@artifact_refs::text[])=(SELECT count(*) FROM direct_artifacts)
)
INSERT INTO control_plane.callback_receipts(child_run_id,callback_edge_id,result_snapshot)
SELECT id,edge_id,value FROM snapshot ON CONFLICT DO NOTHING;
