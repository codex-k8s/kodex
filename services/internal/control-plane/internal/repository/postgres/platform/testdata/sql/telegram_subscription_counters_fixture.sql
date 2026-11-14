SELECT used_effects,rate_window_effects,version,enabled
FROM control_plane.telegram_subscriptions WHERE ref=$1;
