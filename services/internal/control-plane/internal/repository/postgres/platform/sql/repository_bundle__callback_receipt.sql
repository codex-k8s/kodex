-- name: repository_bundle__callback_receipt :exec
WITH origin AS (
    SELECT child.id,child.organization_id,child.project_id,child.ref,child.version,child.state,
        child.result_summary,witness.callback_edge_id AS edge_id,source.ref AS node_ref,witness.final_node_id AS node_id,
        @runtime_revision_id::uuid AS revision_id,witness.revision_ref,witness.attempt,witness.generation
    FROM control_plane.repository_bundle_callback_origin(@organization_id::uuid,@child_run_id::uuid,@runtime_revision_id::uuid) witness
    JOIN control_plane.runs child ON child.id=@child_run_id::uuid AND child.organization_id=@organization_id::uuid
    JOIN control_plane.run_nodes source ON source.id=witness.original_node_id AND source.organization_id=child.organization_id
    WHERE witness.eligible AND witness.callback_edge_id=@callback_edge_id::uuid
), direct_artifacts AS (
    SELECT jsonb_build_object('ref',artifact.ref,'revision',artifact.revision,'version',artifact.version,
        'digest',artifact.digest,'sizeBytes',artifact.size_bytes,'fileName',artifact.file_name,
        'mediaType',artifact.media_type,'source',artifact.source,'runtimeRevisionRef',origin.revision_ref,
        'attempt',origin.attempt,'generation',origin.generation) AS pin
    FROM origin
    JOIN control_plane.artifacts artifact ON artifact.organization_id=origin.organization_id
      AND artifact.run_id=origin.id AND artifact.node_id=origin.node_id
      AND artifact.ref=ANY(@artifact_refs::text[]) AND artifact.source='AGENT_RESULT'
), received_artifacts AS (
    SELECT pin AS pin
    FROM origin CROSS JOIN LATERAL control_plane.runtime_received_result_artifacts(origin.organization_id,origin.node_id) pin
    WHERE origin.revision_id IS NOT NULL
    UNION
    -- Вложенный Workflow передаёт доставленные квитанции, не общий набор файлов root.
    SELECT pin.value AS pin
    FROM origin
    JOIN control_plane.run_edges callback ON callback.organization_id=origin.organization_id
      AND callback.root_run_id=origin.id AND callback.type='CALLBACK_TO'
    JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id=callback.id
      AND receipt.result_snapshot IS NOT NULL
    CROSS JOIN LATERAL jsonb_array_elements(receipt.result_snapshot->'artifacts') pin(value)
    WHERE origin.revision_id IS NULL
), snapshot AS (
    SELECT origin.id,origin.edge_id,
        jsonb_build_object('format',1,'childRunRef',origin.ref,'childVersion',origin.version,
            'nodeRef',origin.node_ref,'state',origin.state,'resultSummary',origin.result_summary,
            'artifacts',COALESCE((
                SELECT jsonb_agg(result.pin ORDER BY result.pin->>'ref')
                FROM (SELECT pin FROM direct_artifacts UNION SELECT pin FROM received_artifacts) result
            ),'[]'::jsonb)) AS result
    FROM origin
    WHERE cardinality(@artifact_refs::text[])=(SELECT count(*) FROM direct_artifacts)
      AND cardinality(@artifact_refs::text[])<=16
      AND NOT EXISTS (
          SELECT 1 FROM control_plane.run_edges callback
          JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id=callback.id
          WHERE origin.revision_id IS NULL AND callback.organization_id=origin.organization_id
            AND callback.root_run_id=origin.id AND callback.type='CALLBACK_TO'
            AND receipt.result_snapshot IS NULL
      )
)
INSERT INTO control_plane.callback_receipts(child_run_id,callback_edge_id,result_snapshot)
SELECT id,edge_id,result FROM snapshot
ON CONFLICT DO NOTHING;
