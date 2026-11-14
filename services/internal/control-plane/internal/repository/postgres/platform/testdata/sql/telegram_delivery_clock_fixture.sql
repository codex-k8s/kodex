UPDATE control_plane.interaction_deliveries
SET available_at=clock_timestamp()-interval '1 second',
 lease_expires_at=CASE WHEN @expire_lease::boolean AND state='CLAIMED' THEN clock_timestamp()-interval '1 microsecond' ELSE lease_expires_at END
WHERE ref=@delivery_ref;
