-- name: workflow_launch_proof :one
SELECT launch.state,parent.ref,child.ref,
 origin.root_run_id=launch.origin_root_run_id AND origin.run_id=launch.origin_run_id
 AND origin.node_id=launch.origin_node_id AND origin.session_id=launch.origin_session_id
 AND origin.turn_id=launch.origin_turn_id AND origin.generation=launch.origin_generation
 AND origin.attempt=launch.origin_attempt AND origin.input_digest=launch.origin_input_digest
 AND origin.revision_digest=launch.origin_revision_digest AND child.root_run_id=child.id
 AND child.workflow_version_id=launch.workflow_version_id AND child.initiated_by=launch.root_actor_id,
 (SELECT count(*) FROM control_plane.runtime_leases lease JOIN control_plane.runs run ON run.id=lease.run_id WHERE run.root_run_id=child.id AND lease.state='CLAIMED'),
 (SELECT count(*) FROM control_plane.session_turns turn JOIN control_plane.runs run ON run.id=turn.run_id WHERE run.root_run_id=child.id AND turn.state IN ('QUEUED','RUNNING'))
FROM control_plane.required_workflow_launches launch
JOIN control_plane.runs parent ON parent.id=launch.origin_run_id
JOIN control_plane.runs child ON child.id=launch.child_root_run_id
JOIN control_plane.runtime_revisions origin ON origin.id=launch.origin_runtime_revision_id
WHERE child.ref=@child_ref;
