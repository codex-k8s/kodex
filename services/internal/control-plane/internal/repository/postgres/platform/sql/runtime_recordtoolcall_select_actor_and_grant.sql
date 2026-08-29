-- name: runtime_recordtoolcall_select_actor_and_grant :one
SELECT agent.ref,agent.name,agent.system_key='system-assistant',
       CASE
         WHEN @grant_ref='' THEN true
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
