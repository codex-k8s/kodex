-- name: runtime_recordtoolcall_select_actor_and_grant :one
SELECT agent.ref,agent.name,COALESCE(agent.system_key='system-assistant',false),
       CASE
         WHEN @grant_ref='' THEN true
         WHEN @capability_ref='' AND @tool IN ('search_files','get_file_metadata','preview_file','get_file_manifest') THEN EXISTS (
           SELECT 1 FROM control_plane.runtime_file_catalogs catalog
           WHERE catalog.runtime_revision_ref=revision.ref AND catalog.organization_id=revision.organization_id
             AND catalog.ref=@grant_ref AND catalog.generation=revision.generation AND catalog.frozen
             AND @purpose=ANY(catalog.purposes)
         )
         ELSE EXISTS (
           SELECT 1
           FROM jsonb_array_elements(COALESCE(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) integration_grant(value)
           WHERE integration_grant.value->>'ref'=@grant_ref AND integration_grant.value->>'capabilityKey'=@capability_ref
         )
       END
FROM control_plane.runtime_revisions revision
JOIN control_plane.agents agent ON agent.id=revision.agent_id
WHERE revision.organization_id=@organization_id::uuid AND revision.node_id=@node_id::uuid
  AND revision.generation=@generation
