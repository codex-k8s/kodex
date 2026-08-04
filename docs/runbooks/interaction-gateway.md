---
id: RUNBOOK-DOC-INTERACTION-GATEWAY
title: Interaction gateway
type: runbook
status: approved
owner: manager
version: 1.1.0
updated: 2026-08-04
---

# Interaction gateway

## Сигналы

- `InteractionGatewayUnavailable` — нет доступной реплики;
- `InteractionGatewayNotReady` — exact dependency path не готов;
- `InteractionGatewayDeliveryFailures` — delivery worker не получает durable
  provider receipt либо domain acknowledgement;
- `InteractionGatewayInboundFailures` — inbound не проходит mapping, artifact
  scan или control-plane command;
- `InteractionGatewayPostgresqlUnavailable` — недостаточно готовых CNPG Pod.

## Диагностика

1. Проверить `/readyz` и метрики gateway/authority issuer без чтения значений
   Secret.
2. Проверить готовность CNPG, control-plane, Mattermost REST/WebSocket и S3
   bucket по тем же TLS identities, которые использует рабочий путь.
3. Для delivery прочитать защищённый
   `/internal/v1/deliveries/{deliveryId}`: сверить state, payload digest,
   attempts и provider receipt. Помимо mTLS обязателен producer-specific bearer
   credential с exact organization/project/delivery. Claim token в readback не
   возвращается.
4. При ambiguous Mattermost POST проверять exact `PendingPostId`, client-owned
   `matter_codex_*` props, bot/channel/root, текст и digest каждого файла;
   ручной повтор публикации запрещён.
5. При owner decision сверить Gate version, claim fence/expiry, recipient,
   Mattermost post/channel и process/session/turn/attempt/input lineage.
6. `DEAD_LETTER` не объявлять успешным и не изменять SQL вручную; recovery
   оформляется отдельным owner-approved repair Issue.
7. При зависшем inbound проверить DB-time lease/fence/token: expired
   `PROCESSING` reclaim-ится worker, а `WAITING_SCAN` poll не уменьшает terminal
   retry budget. Запись stale worker должна иметь нулевую cardinality.
8. При `DELETION_PENDING` проверить исходный delete event, Chat/Session version,
   24-часовой cleanup и отсутствие новых Turn/callback. Restore обязан отменить
   тот же cleanup receipt; SQL-переход вручную запрещён.
9. Для result больше Mattermost upload limit сверить `CLEAN`, exact S3 metadata,
   project prefix и срок presigned HTTPS link 15 минут.

## Ротация ключей и PostgreSQL identity

- Mattermost JWK публикуется как `NEXT`, затем новый generation становится
  `CURRENT`, старый ограниченно обслуживается как `PREVIOUS`, после overlap —
  только `RETIRED`. Control-plane admission readback должен показать тот же
  high-watermark; откат и повторный ввод retired generation запрещены.
- PostgreSQL migration job сверяет context-key digest и served high-watermark.
  Следующее поколение вводится отдельным code-first изменением с overlap;
  `POSTGRES_EXPECTED_SESSION_USER` при этом обязан быть ровно
  `interaction_gateway_runtime_g<N>` для выбранного generation. До следующей
  ротации единственный `PREVIOUS` principal должен быть retired;
  retire выполняется `interaction_gateway_retire_runtime_identity` минимальной
  controller identity, делает `NOLOGIN`, revoke membership, завершает backend
  и не имеет rollback.

## Rollback

1. Остановить rollout и вернуть предыдущие immutable image digests и
   environment overlay.
2. Не выполнять `goose down`: миграция forward-only и совместима с отсутствием
   новой версии Pod.
3. Не удалять gateway PostgreSQL, S3 objects, provider receipts, cursors и
   delivery rows: они нужны для dedup/recovery.
4. Если изменился mapping, вернуть его предыдущую Git revision вместе с
   соответствующими bot identities; не создавать dual write в legacy bot.
5. Восстановить readiness и только затем возобновить входящие callbacks.

Production-действия требуют отдельного подтверждения владельца.
