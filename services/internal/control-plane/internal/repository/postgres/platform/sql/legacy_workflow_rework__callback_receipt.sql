-- name: legacy_workflow_rework__callback_receipt :exec
WITH RECURSIVE origin AS MATERIALIZED (
    SELECT child.id,child.organization_id,child.project_id,child.ref,child.version,child.state,child.result_summary,
        witness.callback_edge_id AS edge_id,first_node.ref AS node_ref,witness.final_node_id AS node_id,
        witness.revision_ref,witness.runtime_attempt AS attempt,witness.generation,first_node.id AS first_id,
        first_node.agent_id,first_node.owner_rework_gate_id,first_node.root_run_id
    FROM control_plane.legacy_rework_callback_origin(@organization_id::uuid,@child_run_id::uuid,@runtime_revision_id::uuid) witness
    JOIN control_plane.runs child ON child.id=@child_run_id::uuid AND child.organization_id=@organization_id::uuid
    JOIN control_plane.run_nodes first_node ON first_node.id=witness.original_node_id AND first_node.organization_id=child.organization_id
    WHERE witness.eligible AND witness.callback_edge_id=@callback_edge_id::uuid
), budget AS MATERIALIZED (
    SELECT count(*) AS nodes FROM control_plane.run_nodes node JOIN origin ON node.organization_id=origin.organization_id
        AND node.root_run_id=origin.root_run_id
), lineage AS (
    SELECT node.id,node.attempt,node.turn_id,ARRAY[node.id] AS path,0 AS depth
    FROM origin JOIN control_plane.run_nodes node ON node.id=origin.node_id
    UNION ALL
    SELECT prior.id,prior.attempt,prior.turn_id,lineage.path||prior.id,lineage.depth+1
    FROM lineage JOIN origin ON true
    JOIN control_plane.run_edges edge ON edge.target_node_id=lineage.id AND edge.organization_id=origin.organization_id
        AND edge.root_run_id=origin.root_run_id AND edge.type='CONTINUES'
    JOIN control_plane.run_nodes prior ON prior.id=edge.source_node_id AND prior.organization_id=origin.organization_id
        AND prior.root_run_id=origin.root_run_id AND prior.run_id=origin.id AND prior.agent_id=origin.agent_id
        AND prior.owner_rework_gate_id=origin.owner_rework_gate_id
        AND COALESCE(prior.owner_rework_origin_node_id,prior.id)=origin.first_id
        AND prior.attempt+1=lineage.attempt AND prior.state='SUCCEEDED'
    CROSS JOIN budget WHERE lineage.depth<budget.nodes AND NOT prior.id=ANY(lineage.path)
), producers AS MATERIALIZED (
    SELECT lineage.id,revision.ref,revision.attempt,revision.generation
    FROM lineage JOIN origin ON true
    JOIN control_plane.runs physical ON physical.id=origin.id AND physical.organization_id=origin.organization_id
    JOIN control_plane.runtime_revisions revision ON revision.node_id=lineage.id AND revision.organization_id=origin.organization_id
        AND revision.run_id=origin.id AND revision.root_run_id=origin.root_run_id AND revision.project_id=origin.project_id
        AND revision.agent_id=origin.agent_id AND revision.session_id=physical.session_id
        AND revision.turn_id=lineage.turn_id AND revision.attempt=lineage.attempt
    JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id AND lease.node_id=lineage.id
        AND lease.organization_id=origin.organization_id AND lease.run_id=origin.id AND lease.generation=revision.generation
        AND lease.input_digest=revision.input_digest AND lease.state='COMPLETED'
), expected_artifacts AS MATERIALIZED (
    SELECT head.id,head.node_id FROM origin JOIN lineage ON true
    JOIN control_plane.artifact_heads head ON head.organization_id=origin.organization_id AND head.run_id=origin.id AND head.node_id=lineage.id
), direct_artifacts AS (
    SELECT artifact.ref,artifact.node_id,jsonb_build_object('ref',artifact.ref,'revision',artifact.revision,'version',artifact.version,
        'digest',artifact.digest,'sizeBytes',artifact.size_bytes,'fileName',artifact.file_name,'mediaType',artifact.media_type,
        'source',artifact.source,'runtimeRevisionRef',producers.ref,'attempt',producers.attempt,'generation',producers.generation) AS pin
    FROM expected_artifacts expected JOIN origin ON true JOIN producers ON producers.id=expected.node_id
    JOIN control_plane.artifact_history artifact ON artifact.id=expected.id AND artifact.organization_id=origin.organization_id
        AND artifact.run_id=origin.id AND artifact.node_id=expected.node_id AND artifact.revision=1 AND artifact.version=1
        AND artifact.source='AGENT_RESULT' AND artifact.lifecycle_state='ACTIVE' AND artifact.scan_state='CLEAN'
    JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
        AND content.digest=artifact.digest AND content.size_bytes=artifact.size_bytes
), pins AS (
    SELECT pin FROM direct_artifacts UNION
    SELECT received FROM origin CROSS JOIN LATERAL control_plane.runtime_received_result_artifacts(origin.organization_id,origin.node_id) received
), snapshot AS (
    SELECT origin.id,origin.edge_id,jsonb_build_object('format',1,'childRunRef',origin.ref,'childVersion',origin.version,
        'nodeRef',origin.node_ref,'state',origin.state,'resultSummary',origin.result_summary,
        'artifacts',COALESCE((SELECT jsonb_agg(pin ORDER BY pin->>'ref') FROM pins),'[]'::jsonb)) AS value
    FROM origin WHERE cardinality(@artifact_refs::text[])<=16
        AND cardinality(@artifact_refs::text[])=(SELECT count(*) FROM direct_artifacts artifact WHERE artifact.node_id=origin.node_id AND artifact.ref=ANY(@artifact_refs::text[]))
        AND (SELECT count(*) FROM lineage)=(SELECT count(*) FROM producers)
        AND (SELECT count(*) FROM expected_artifacts)=(SELECT count(*) FROM direct_artifacts)
)
INSERT INTO control_plane.callback_receipts(child_run_id,callback_edge_id,result_snapshot)
SELECT id,edge_id,value FROM snapshot ON CONFLICT DO NOTHING;
