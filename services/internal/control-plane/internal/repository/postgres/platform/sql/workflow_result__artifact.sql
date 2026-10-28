-- name: workflow_result__artifact :many
WITH RECURSIVE final AS MATERIALIZED (
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
        AND node.workflow_business_attempt BETWEEN 1 AND 4 AND node.state='SUCCEEDED'
        AND node.workflow_captured_artifacts->>'runtimeRevisionId'=revision.id::text
        AND node.workflow_captured_artifacts->>'leaseId'=lease.id::text
        AND node.workflow_captured_artifacts->'generation'=to_jsonb(lease.generation)
), lineage AS (
    SELECT final.id,final.turn_id,final.attempt,final.workflow_captured_artifacts,0 AS depth,ARRAY[final.id] AS path,false AS cycle
    FROM final
    UNION ALL
    SELECT previous.id,previous.turn_id,previous.attempt,previous.workflow_captured_artifacts,
           lineage.depth+1,lineage.path || previous.id,previous.id=ANY(lineage.path)
    FROM lineage CROSS JOIN final
    JOIN control_plane.run_edges edge ON edge.organization_id=final.organization_id AND edge.root_run_id=final.root_run_id
        AND edge.type='CONTINUES' AND edge.target_node_id=lineage.id
    JOIN control_plane.run_nodes previous ON previous.id=edge.source_node_id AND previous.organization_id=final.organization_id
        AND previous.root_run_id=final.root_run_id AND previous.run_id=final.run_id AND previous.agent_id=final.agent_id
        AND previous.state='SUCCEEDED' AND previous.attempt+1=lineage.attempt
        AND COALESCE(previous.workflow_step_origin_node_id,previous.id)=final.origin_id
        AND previous.workflow_business_attempt=final.workflow_business_attempt
    JOIN control_plane.session_turns turn ON turn.id=previous.turn_id AND turn.run_id=previous.run_id AND turn.session_id=final.session_id
    JOIN control_plane.runtime_revisions revision ON revision.id::text=previous.workflow_captured_artifacts->>'runtimeRevisionId'
        AND revision.organization_id=previous.organization_id AND revision.root_run_id=previous.root_run_id
        AND revision.node_id=previous.id AND revision.run_id=previous.run_id AND revision.turn_id=previous.turn_id
        AND revision.agent_id=previous.agent_id AND revision.attempt=previous.attempt
    JOIN control_plane.runtime_leases lease ON lease.id::text=previous.workflow_captured_artifacts->>'leaseId'
        AND lease.organization_id=revision.organization_id AND lease.runtime_revision_id=revision.id
        AND lease.node_id=previous.id AND lease.run_id=previous.run_id AND lease.generation=revision.generation
        AND to_jsonb(lease.generation)=previous.workflow_captured_artifacts->'generation' AND lease.state='COMPLETED'
    WHERE NOT lineage.cycle AND lineage.depth<65
        AND 1=(SELECT count(*) FROM control_plane.run_edges incoming
            WHERE incoming.organization_id=final.organization_id AND incoming.root_run_id=final.root_run_id
                AND incoming.type='CONTINUES' AND incoming.target_node_id=lineage.id)
), candidates AS (
    SELECT lineage.id AS producer_id,pin.value AS pin
    FROM lineage CROSS JOIN final
    CROSS JOIN LATERAL jsonb_array_elements(lineage.workflow_captured_artifacts->'artifacts') pin(value)
    WHERE (@source_kind='CURRENT_COMPLETION' AND lineage.id=final.id)
       OR (@source_kind='CAPTURED_ARTIFACT' AND lineage.id<>final.id
           AND pin.value->>'ref'=@artifact_ref AND pin.value->'revision'=to_jsonb(@artifact_revision::bigint)
           AND pin.value->>'digest'=@artifact_digest)
), pinned AS MATERIALIZED (
    SELECT history.ref,history.revision_ref,history.digest,history.revision,history.revision_id,
        head.id AS artifact_id,final.organization_id,final.actor_id,final.project_id,final.agent_id,final.run_id
    FROM candidates CROSS JOIN final
    JOIN control_plane.artifact_heads head ON head.organization_id=final.organization_id AND head.project_id=final.project_id
        AND head.ref=candidates.pin->>'ref' AND head.lifecycle_state='ACTIVE'
    JOIN control_plane.artifact_history history ON history.id=head.id AND history.organization_id=final.organization_id
        AND history.project_id=final.project_id AND history.run_id=final.run_id AND history.node_id=candidates.producer_id
        AND history.revision_ref=candidates.pin->>'revisionRef' AND to_jsonb(history.revision)=candidates.pin->'revision'
        AND history.digest=candidates.pin->>'digest' AND to_jsonb(history.size_bytes)=candidates.pin->'sizeBytes'
        AND history.file_name=candidates.pin->>'fileName' AND history.media_type=candidates.pin->>'mediaType'
        AND history.source='AGENT_RESULT' AND history.lifecycle_state='ACTIVE'
    JOIN control_plane.artifact_revision_content content ON content.revision_id=history.revision_id
        AND content.digest=history.digest AND content.size_bytes=history.size_bytes
    WHERE history.file_name=@file_name AND history.media_type=@media_type
        AND 1=(SELECT count(*) FROM candidates named WHERE named.pin->>'fileName'=@file_name)
        AND NOT EXISTS (SELECT 1 FROM lineage WHERE cycle OR depth>64)
        AND NOT EXISTS (
            SELECT 1 FROM lineage current
            WHERE (SELECT count(*) FROM control_plane.run_edges incoming
                WHERE incoming.organization_id=final.organization_id AND incoming.root_run_id=final.root_run_id
                    AND incoming.type='CONTINUES' AND incoming.target_node_id=current.id)>1
            OR EXISTS (
                SELECT 1 FROM control_plane.run_edges incoming
                WHERE incoming.organization_id=final.organization_id AND incoming.root_run_id=final.root_run_id
                    AND incoming.type='CONTINUES' AND incoming.target_node_id=current.id
                    AND NOT EXISTS (SELECT 1 FROM lineage previous
                        WHERE previous.id=incoming.source_node_id AND previous.depth=current.depth+1)
            )
        )
        AND (@source_kind='CURRENT_COMPLETION' OR EXISTS (
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
        ))
    FOR SHARE OF head NOWAIT
), clean AS MATERIALIZED (
    SELECT pinned.ref,pinned.revision_ref,pinned.digest,pinned.revision,pinned.artifact_id,
        pinned.organization_id,pinned.actor_id,pinned.project_id,pinned.agent_id,pinned.run_id
    FROM pinned
    WHERE control_plane.artifact_effective_scan(pinned.revision_id)='CLEAN'
), checked AS MATERIALIZED (
    SELECT clean.ref,clean.revision_ref,clean.digest,clean.revision,
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
SELECT ref,revision_ref,substring(digest FROM 8),revision
FROM checked WHERE eligible
ORDER BY ref,revision
LIMIT 2;
