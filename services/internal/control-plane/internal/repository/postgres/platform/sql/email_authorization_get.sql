SELECT ref,query_projection,decision_projection
FROM control_plane.email_authorizations
WHERE organization_id=$1::uuid AND source_ref=$2 AND lease_ref=$3 AND generation=$4 AND fence_digest=$5
FOR SHARE;
