-- name: workspace_unbound__prepare :one
WITH source AS MATERIALIZED (
 SELECT workspace.id,workspace.organization_id,workspace.session_id,workspace.generation,
        CASE WHEN workspace.source_archive_id IS NULL THEN 'EMPTY' ELSE 'RESTORED' END AS previous_state,
        lease.id AS lease_id,lease.ref AS lease_ref,revision.id AS revision_id
 FROM control_plane.session_workspace_storage workspace
 JOIN control_plane.runtime_leases lease ON lease.id=@lease_id::uuid AND lease.organization_id=workspace.organization_id
 JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
   AND revision.organization_id=workspace.organization_id AND revision.session_id=workspace.session_id
   AND revision.project_id=workspace.project_id AND revision.agent_id=workspace.agent_id
 JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.organization_id=workspace.organization_id
   AND node.state IN ('FAILED','CANCELLED','DENIED','SUCCEEDED')
 JOIN control_plane.session_storage native ON native.session_id=workspace.session_id AND native.organization_id=workspace.organization_id
   AND native.native_writer_lease_ref=lease.ref AND native.native_writer_revision_id=revision.id
 WHERE workspace.id=@workspace_id::uuid AND workspace.organization_id=@organization_id::uuid
   AND workspace.generation=@workspace_generation AND workspace.version=@version
   AND workspace.state='WRITER_PENDING' AND workspace.writer_lease_id=lease.id
   AND workspace.writer_acknowledgement IS NULL AND workspace.quiescence_record IS NULL
   AND workspace.block_reason='' AND NOT workspace.writer_released AND workspace.destructive_task_id IS NULL
   AND lease.state IN ('COMPLETED','CANCELLED','EXPIRED')
   AND ((native.state='INITIALIZING' AND native.native_session_id IS NULL AND native.native_checkpoint IS NULL
         AND native.content_generation=0 AND native.current_archive_id IS NULL)
        OR (native.state='LIVE' AND native.native_checkpoint IS NOT NULL AND native.content_generation>0))
   AND NOT EXISTS(SELECT 1 FROM control_plane.integration_invocations effect
     WHERE effect.organization_id=workspace.organization_id AND effect.node_id=node.id AND effect.risk<>'READ')
   AND NOT EXISTS(SELECT 1 FROM control_plane.repository_bundle_publications effect
     WHERE effect.organization_id=workspace.organization_id AND effect.origin_node_id=node.id)
)
INSERT INTO control_plane.workspace_unbound_stops(organization_id,workspace_id,workspace_generation,lease_id,previous_state,record)
SELECT organization_id,id,generation,lease_id,previous_state,@record FROM source
RETURNING previous_state;
