SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM control_plane.worker_retry_fault_seq
