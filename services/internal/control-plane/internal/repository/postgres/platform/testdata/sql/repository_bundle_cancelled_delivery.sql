-- name: repository_bundle_cancelled_delivery :one
SELECT root.state,root.id::text,
  (SELECT count(*)::integer FROM control_plane.runtime_leases lease JOIN control_plane.runs child ON child.id=lease.run_id WHERE child.root_run_id=root.id AND lease.state='CLAIMED'),
  (SELECT count(*)::integer FROM control_plane.repository_bundle_publications publication WHERE publication.root_run_id=root.id AND (publication.continuation_state='BLOCKED' OR publication.claimant<>'' OR publication.invocation_lease_ref<>'')),
  (SELECT count(*)::integer FROM control_plane.run_nodes node WHERE node.root_run_id=root.id AND node.state IN ('QUEUED','RUNNING','WAITING')),
  (SELECT count(*)::integer FROM control_plane.run_nodes node WHERE node.root_run_id=root.id AND node.type='AGENT_EXECUTION' AND node.state IN ('QUEUED','RUNNING','WAITING')),
  (SELECT count(*)::integer FROM control_plane.run_nodes node WHERE node.root_run_id=root.id AND node.type='ROOT_PROCESS' AND node.state='RUNNING')
FROM control_plane.runs root WHERE root.ref=$1;
