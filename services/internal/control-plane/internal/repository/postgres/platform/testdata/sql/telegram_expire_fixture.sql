UPDATE control_plane.telegram_subscriptions SET expires_at=created_at+interval '1 microsecond' WHERE ref=$1;
