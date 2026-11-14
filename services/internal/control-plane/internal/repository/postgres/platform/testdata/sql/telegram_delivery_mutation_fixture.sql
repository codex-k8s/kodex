UPDATE control_plane.interaction_deliveries
SET telegram_input_digest=CASE WHEN @mutation='DIGEST' THEN repeat('0',64) ELSE telegram_input_digest END,
 telegram_input=CASE WHEN @mutation='SNAPSHOT' THEN jsonb_set(telegram_input,'{project_ref}','"prj_foreign01"'::jsonb) ELSE telegram_input END,
 state=CASE WHEN @mutation='REQUEUE' THEN 'DUE' ELSE state END,
 telegram_receipt=CASE WHEN @mutation='RECEIPT' THEN '{"messageId":999}'::jsonb ELSE telegram_receipt END
WHERE ref=@delivery_ref;
