UPDATE control_plane.kubernetes_identity_setups
SET state=@state,version=version+1 WHERE id=@setup_id::uuid;
