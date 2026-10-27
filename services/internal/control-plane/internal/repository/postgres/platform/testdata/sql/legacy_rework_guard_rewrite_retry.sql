-- Только rollback-only negative probe собственного native fixture node.
UPDATE control_plane.runs SET retry_of_run_id=NULL WHERE id=(SELECT run_id FROM control_plane.run_nodes WHERE ref=@node_ref);
