-- name: native_session__unresolved_effects :one
-- Native history is never evidence that an accepted external effect completed.
-- Include delegated runs in every root graph originating from this session.
WITH roots AS (
 SELECT DISTINCT run.root_run_id
 FROM control_plane.sessions session
 JOIN control_plane.runs run ON run.session_id=session.id AND run.organization_id=session.organization_id
 WHERE session.organization_id=@organization_id::uuid AND session.ref=@session_ref
), graph AS (
 SELECT run.id FROM control_plane.runs run
 JOIN roots ON roots.root_run_id=run.root_run_id
 WHERE run.organization_id=@organization_id::uuid
)
SELECT EXISTS (
 SELECT 1 FROM control_plane.integration_invocations invocation
 JOIN graph ON graph.id=invocation.run_id
 WHERE invocation.organization_id=@organization_id::uuid AND invocation.risk<>'READ'
   AND invocation.state IN('WAITING_APPROVAL','READY','RUNNING','UNKNOWN_OUTCOME')
) OR EXISTS (
 SELECT 1 FROM control_plane.repository_bundle_publications publication
 JOIN graph ON graph.id=publication.origin_run_id
 WHERE publication.organization_id=@organization_id::uuid
   AND publication.state IN('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')
);
