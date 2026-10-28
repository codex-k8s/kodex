-- name: workflow_evidence__artifact :many
WITH execution AS MATERIALIZED (
    SELECT node.id,node.organization_id,node.root_run_id,node.run_id,node.agent_id,node.turn_id,
        root.project_id,root.initiated_by,revision.ref AS revision_ref,revision.safe_snapshot,lease.generation
    FROM control_plane.run_nodes node
    JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    JOIN control_plane.runtime_revisions revision ON revision.id=@runtime_revision_id::uuid
        AND revision.organization_id=node.organization_id AND revision.node_id=node.id AND revision.run_id=node.run_id
        AND revision.root_run_id=root.id AND revision.agent_id=node.agent_id AND revision.turn_id=node.turn_id AND revision.attempt=node.attempt
    JOIN control_plane.runtime_leases lease ON lease.id=@lease_id::uuid AND lease.runtime_revision_id=revision.id
        AND lease.organization_id=node.organization_id AND lease.node_id=node.id AND lease.run_id=node.run_id AND lease.generation=revision.generation
    WHERE node.organization_id=@organization_id::uuid AND node.id=@node_id::uuid AND node.root_run_id=@root_run_id::uuid
        AND version.spec->>'ResultEvidenceProfile'='GITHUB_REVIEW_QA_V1'
        AND node.workflow_business_attempt BETWEEN 1 AND 4 AND root.state IN ('RUNNING','WAITING_HUMAN')
        AND ((node.state='RUNNING' AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp()
            AND control_plane.runtime_execution_before_deadline(node.organization_id,node.run_id))
          OR (node.state='SUCCEEDED' AND lease.state='COMPLETED'
            AND node.workflow_captured_artifacts->>'runtimeRevisionId'=revision.id::text
            AND node.workflow_captured_artifacts->>'leaseId'=lease.id::text
            AND node.workflow_captured_artifacts->'generation'=to_jsonb(lease.generation)))
), candidates AS MATERIALIZED (
    SELECT history.*
    FROM execution
    CROSS JOIN LATERAL control_plane.workflow_captured_artifact_pins(execution.organization_id,execution.id) pin
    JOIN control_plane.artifact_history history ON history.id=pin.artifact_id AND history.revision_id=pin.revision_id
        AND history.organization_id=execution.organization_id AND history.project_id=execution.project_id
        AND history.run_id=execution.run_id AND history.node_id<>execution.id
    WHERE (@file_name='' OR history.file_name=@file_name)
), pinned AS MATERIALIZED (
    SELECT candidates.* FROM candidates
    JOIN control_plane.artifact_heads head ON head.id=candidates.id AND head.organization_id=candidates.organization_id
        AND head.project_id=candidates.project_id AND head.lifecycle_state='ACTIVE'
    WHERE (@file_name='' OR 1=(SELECT count(*) FROM candidates))
        AND (@artifact_ref='' OR (candidates.ref=@artifact_ref AND candidates.revision=@artifact_revision::bigint AND candidates.digest=@artifact_digest))
    FOR SHARE OF head NOWAIT
)
SELECT pinned.ref,pinned.revision_ref,pinned.revision,pinned.digest,pinned.file_name,pinned.media_type,pinned.size_bytes,
    content.object_key,content.object_version,content.object_etag
FROM pinned CROSS JOIN execution
JOIN control_plane.runtime_file_catalogs catalog ON catalog.organization_id=execution.organization_id
    AND catalog.node_id=execution.id AND catalog.run_id=execution.run_id AND catalog.project_id=execution.project_id
    AND catalog.agent_id=execution.agent_id AND catalog.actor_id=execution.initiated_by
    AND catalog.runtime_revision_ref=execution.revision_ref AND catalog.generation=execution.generation AND catalog.frozen
    AND catalog.turn_id=execution.turn_id
    AND catalog.ref=execution.safe_snapshot #>> '{fileCatalog,ref}'
    AND catalog.digest=execution.safe_snapshot #>> '{fileCatalog,digest}'
    AND to_jsonb(catalog.total)=execution.safe_snapshot #> '{fileCatalog,total}'
    AND to_jsonb(catalog.purposes)=execution.safe_snapshot #> '{fileCatalog,purposes}'
JOIN control_plane.runtime_file_visible_entries entry ON entry.catalog_id=catalog.id AND entry.purpose='RUN_RESULT'
    AND entry.artifact_id=pinned.id AND entry.artifact_ref=pinned.ref AND entry.artifact_revision=pinned.revision
    AND entry.artifact_digest=pinned.digest AND entry.file_name=pinned.file_name AND entry.media_type=pinned.media_type AND entry.size_bytes=pinned.size_bytes
JOIN control_plane.artifact_revision_content content ON content.revision_id=pinned.revision_id
    AND content.digest=pinned.digest AND content.size_bytes=pinned.size_bytes
WHERE pinned.lifecycle_state='ACTIVE' AND control_plane.artifact_effective_scan(pinned.revision_id)='CLEAN'
LIMIT 2;
