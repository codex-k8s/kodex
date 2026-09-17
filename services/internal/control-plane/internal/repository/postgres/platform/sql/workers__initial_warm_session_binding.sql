-- name: workers__initial_warm_session_binding :one
SELECT agent.ref, subject.id::text
FROM control_plane.assistant_runtime runtime
JOIN control_plane.agents agent
  ON agent.id = runtime.agent_id AND agent.organization_id = runtime.organization_id
JOIN control_plane.subjects subject
  ON subject.organization_id = runtime.organization_id
 AND subject.ref = 'sys_platform' AND subject.issuer = 'kodex-system'
 AND subject.kind = 'SERVICE'
WHERE runtime.organization_id = @organization_id::uuid
  AND runtime.stable_key = 'system-assistant'
  AND runtime.system_session_ref IS NULL
FOR UPDATE OF runtime, agent;
