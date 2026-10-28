-- Только rollback-only negative probe собственного native fixture node.
DELETE FROM control_plane.run_nodes WHERE ref=@node_ref;
