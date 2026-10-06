SELECT organization_id::text,node_id::text,generation
FROM control_plane.runtime_revisions WHERE ref=$1;
