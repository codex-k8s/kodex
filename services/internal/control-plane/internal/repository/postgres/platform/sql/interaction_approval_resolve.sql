-- name: interaction_approval_resolve :one
SELECT gate.id::text,gate.node_id::text,gate.root_run_id::text,gate.project_id::text,project.ref,
    gate.version,gate.state,gate.allowed_decisions,node.ref,run.state,delivery.id::text,delivery.state,
    connection.ref,delivery.connection_version,delivery.definition_version,delivery.definition_digest,
    grant_row.enabled AND grant_row.organization_id=connection.organization_id AND grant_row.connection_id=connection.id
        AND grant_row.definition_version=connection.definition_version AND grant_row.definition_digest=connection.definition_digest
FROM control_plane.owner_gates gate
JOIN control_plane.interaction_deliveries delivery ON delivery.approval_gate_id=gate.id
    AND delivery.organization_id=gate.organization_id
JOIN control_plane.projects project ON project.id=gate.project_id
JOIN control_plane.run_nodes node ON node.id=gate.node_id
JOIN control_plane.runs run ON run.id=gate.root_run_id
JOIN control_plane.integration_connections connection ON connection.id=delivery.connection_id
JOIN control_plane.integration_grants grant_row ON grant_row.id=delivery.grant_id
WHERE gate.organization_id=@organization_id::uuid AND gate.ref=@gate_ref
FOR UPDATE OF gate,delivery,connection,grant_row;
