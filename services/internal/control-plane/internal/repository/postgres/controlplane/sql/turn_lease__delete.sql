DELETE FROM control_plane.turn_leases
WHERE turn_id = @turn_id::uuid
  AND fence = @fence
