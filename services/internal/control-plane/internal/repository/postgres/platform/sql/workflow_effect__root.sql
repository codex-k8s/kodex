-- name: workflow_effect__root :one
SELECT root.id::text
FROM control_plane.runs root
JOIN control_plane.runs physical ON physical.root_run_id=root.id AND physical.organization_id=root.organization_id
JOIN control_plane.run_nodes node ON node.run_id=physical.id AND node.root_run_id=root.id AND node.organization_id=root.organization_id
JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
WHERE root.organization_id=@organization_id::uuid
    AND ((@lease_ref='' AND physical.ref=@run_ref AND node.ref=@node_ref)
        OR (@lease_ref<>'' AND EXISTS (SELECT 1 FROM control_plane.runtime_leases lease
            WHERE lease.organization_id=root.organization_id AND lease.node_id=node.id AND lease.run_id=physical.id AND lease.ref=@lease_ref)))
    AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
FOR UPDATE OF root;
