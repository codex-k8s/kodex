-- name: workflow_result__artifact_review :many
WITH final AS MATERIALIZED (
    SELECT node.id,node.organization_id,node.root_run_id,node.run_id,node.agent_id,node.turn_id,node.attempt,
           COALESCE(node.workflow_step_origin_node_id,node.id) AS origin_id,node.workflow_business_attempt,
           node.workflow_captured_artifacts,root.project_id,root.initiated_by AS actor_id,
           run.session_id,revision.id AS revision_id,revision.ref AS revision_ref,revision.generation,revision.safe_snapshot
    FROM control_plane.run_nodes node
    JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
    JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=node.organization_id
        AND run.root_run_id=root.id AND run.project_id=root.project_id
    JOIN control_plane.runtime_revisions revision ON revision.node_id=node.id AND revision.run_id=node.run_id
        AND revision.organization_id=node.organization_id AND revision.root_run_id=node.root_run_id
        AND revision.turn_id=node.turn_id AND revision.agent_id=node.agent_id AND revision.attempt=node.attempt
    JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
        AND lease.organization_id=revision.organization_id AND lease.run_id=revision.run_id
        AND lease.node_id=revision.node_id AND lease.generation=revision.generation AND lease.state='COMPLETED'
    JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
    WHERE node.organization_id=@organization_id::uuid AND node.id=@node_id::uuid
        AND node.root_run_id=@root_run_id::uuid AND revision.id=@runtime_revision_id::uuid AND lease.id=@lease_id::uuid
        AND node.workflow_business_attempt BETWEEN 1 AND 4 AND node.state='SUCCEEDED' AND root.state IN ('RUNNING','WAITING_HUMAN')
        AND node.workflow_captured_artifacts->>'runtimeRevisionId'=revision.id::text
        AND node.workflow_captured_artifacts->>'leaseId'=lease.id::text
        AND node.workflow_captured_artifacts->'generation'=to_jsonb(lease.generation)

), subject AS MATERIALIZED (
    SELECT final.*,history.ref,history.revision_ref AS artifact_revision_ref,history.digest,history.revision,history.revision_id AS artifact_revision_id,
        head.id AS artifact_id
    FROM final
    JOIN control_plane.run_nodes node ON node.id=final.id
    CROSS JOIN LATERAL jsonb_array_elements(node.workflow_inputs) input(value)
    CROSS JOIN LATERAL jsonb_array_elements(input.value->'receipt'->'results') result(value)
    JOIN control_plane.artifact_heads head ON head.organization_id=final.organization_id
        AND head.project_id=final.project_id AND head.ref=result.value->>'artifactRef' AND head.lifecycle_state='ACTIVE'
    JOIN control_plane.artifact_history history ON history.id=head.id AND history.organization_id=final.organization_id
        AND history.project_id=final.project_id AND history.revision_ref=result.value->>'artifactRevision'
        AND substring(history.digest FROM 8)=result.value->>'digest' AND history.lifecycle_state='ACTIVE'
        AND history.source='AGENT_RESULT'
    JOIN control_plane.artifact_revision_content content ON content.revision_id=history.revision_id
        AND content.digest=history.digest AND content.size_bytes=history.size_bytes
    WHERE input.value->>'stepKey'=@subject_step_key AND input.value->'receipt'->>'accepted'='true'
        AND result.value->>'key'=@subject_result_key AND result.value->>'kind'='ARTIFACT'
        AND history.ref=@artifact_ref AND history.revision_ref=@artifact_revision_ref
        AND history.revision=@artifact_revision::bigint AND history.digest=@artifact_digest
        AND EXISTS (
            SELECT 1 FROM control_plane.runtime_file_catalogs catalog
            JOIN control_plane.runtime_file_catalog_entries entry ON entry.catalog_id=catalog.id
            WHERE catalog.runtime_revision_ref=final.revision_ref AND catalog.frozen
                AND catalog.organization_id=final.organization_id AND catalog.project_id=final.project_id
                AND catalog.actor_id=final.actor_id AND catalog.agent_id=final.agent_id
                AND catalog.node_id=final.id AND catalog.run_id=final.run_id AND catalog.session_id=final.session_id
                AND catalog.turn_id=final.turn_id AND catalog.generation=final.generation
                AND catalog.ref=final.safe_snapshot #>> '{fileCatalog,ref}'
                AND catalog.digest=final.safe_snapshot #>> '{fileCatalog,digest}'
                AND to_jsonb(catalog.total)=final.safe_snapshot #> '{fileCatalog,total}'
                AND to_jsonb(catalog.purposes)=final.safe_snapshot #> '{fileCatalog,purposes}'
                AND 'RUN_RESULT'=ANY(catalog.purposes) AND entry.purpose='RUN_RESULT'
                AND entry.artifact_id=history.id AND entry.artifact_ref=history.ref AND entry.artifact_revision=history.revision
                AND entry.artifact_digest=history.digest AND entry.file_name=history.file_name
                AND entry.media_type=history.media_type AND entry.size_bytes=history.size_bytes AND entry.source=history.source
                AND entry.entry_digest=control_plane.runtime_file_entry_digest(entry)
        )
    FOR SHARE OF head NOWAIT
), clean AS MATERIALIZED (
    SELECT subject.* FROM subject WHERE control_plane.artifact_effective_scan(subject.artifact_revision_id)='CLEAN'
), checked AS MATERIALIZED (
    SELECT
        (SELECT control_plane.catalog_resource_visible(clean.organization_id,clean.actor_id,'project.view','PROJECT',
            target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false)
            FROM control_plane.catalog_access_targets target
            WHERE target.organization_id=clean.organization_id AND target.kind='PROJECT' AND target.id=clean.project_id)
        AND (SELECT control_plane.catalog_resource_visible(clean.organization_id,clean.actor_id,'agent.view','AGENT',
            target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false)
            FROM control_plane.catalog_access_targets target
            WHERE target.organization_id=clean.organization_id AND target.kind='AGENT' AND target.id=clean.agent_id AND target.project_id=clean.project_id)
        AND (SELECT control_plane.catalog_resource_visible(clean.organization_id,clean.actor_id,'run.view','RUN',
            target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false)
            FROM control_plane.catalog_access_targets target
            WHERE target.organization_id=clean.organization_id AND target.kind='RUN' AND target.id=clean.run_id AND target.project_id=clean.project_id)
        AND (SELECT control_plane.catalog_resource_visible(clean.organization_id,clean.actor_id,'artifact.view','ARTIFACT',
            target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false)
            AND control_plane.catalog_resource_visible(clean.organization_id,clean.actor_id,'artifact.download','ARTIFACT',
            target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false)
            FROM control_plane.catalog_access_targets target
            WHERE target.organization_id=clean.organization_id AND target.kind='ARTIFACT' AND target.id=clean.artifact_id) AS eligible
    FROM clean
)
SELECT true FROM checked WHERE eligible LIMIT 2;
