-- Только rollback-only negative probe собственного native fixture node.
UPDATE control_plane.run_nodes SET owner_rework_gate_id=NULL WHERE ref=@node_ref;
