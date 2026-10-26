-- name: ssh_compose__context :one
SELECT c.public_configuration,p.ref,root.ref,
       revision.safe_snapshot->>'sessionRef',revision.safe_snapshot->>'turnRef',
       (revision.safe_snapshot->>'attempt')::integer,revision.id::text,model_lease.id::text,revision.generation
FROM control_plane.integration_connections c
JOIN control_plane.projects p ON p.organization_id=c.organization_id AND p.id=@project_id::uuid AND p.lifecycle='ACTIVE'
JOIN control_plane.run_nodes n ON n.organization_id=c.organization_id AND n.id=@node_id::uuid
JOIN control_plane.runs execution ON execution.id=n.run_id AND execution.project_id=p.id AND execution.organization_id=c.organization_id
JOIN control_plane.runs root ON root.id=n.root_run_id AND root.organization_id=c.organization_id
JOIN control_plane.runtime_revisions revision ON revision.organization_id=c.organization_id AND revision.node_id=n.id
  AND revision.run_id=n.run_id AND revision.root_run_id=root.id AND revision.agent_id=n.agent_id
  AND revision.turn_id=n.turn_id AND revision.attempt=n.attempt
JOIN control_plane.runtime_leases model_lease ON model_lease.runtime_revision_id=revision.id
  AND model_lease.organization_id=revision.organization_id AND model_lease.node_id=n.id AND model_lease.run_id=n.run_id
  AND model_lease.generation=revision.generation
WHERE c.organization_id=@organization_id::uuid AND c.id=@connection_id::uuid
  AND c.definition_key='ssh-compose' AND c.enabled AND c.state='CONNECTED'
  AND c.public_configuration->>'project_ref'=p.ref
  AND ((@runtime_revision_id='' AND n.state='RUNNING' AND model_lease.state='CLAIMED' AND model_lease.expires_at>clock_timestamp()
      AND NOT EXISTS(SELECT 1 FROM control_plane.runtime_revisions later WHERE later.node_id=n.id AND later.generation>revision.generation))
    OR (@runtime_revision_id<>'' AND revision.id=NULLIF(@runtime_revision_id,'')::uuid
      AND model_lease.id=NULLIF(@model_lease_id,'')::uuid AND revision.generation=@model_generation::bigint
      AND model_lease.state IN('CLAIMED','COMPLETED')));
