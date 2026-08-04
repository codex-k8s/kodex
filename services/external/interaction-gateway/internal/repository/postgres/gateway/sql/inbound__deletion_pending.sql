SELECT EXISTS (
    SELECT 1 FROM interaction_gateway_inbound_events
    WHERE organization_id = $1::uuid AND project_id = $2::uuid AND state IN ('PENDING', 'PROCESSING', 'WAITING_CLEANUP')
      AND ((kind = 'CHANNEL_DELETE' AND $3 <> '' AND payload->>'chat_id' = $3)
        OR (kind = 'THREAD_DELETE' AND $4 <> '' AND session_id = $4::uuid))
);
