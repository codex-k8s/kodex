---
id: RUN-MC-012
title: Диагностика и восстановление integration-gateway
type: runbook
status: approved
owner: sre
version: 1.1.0
updated: 2026-08-03
---

# Диагностика и восстановление integration-gateway

## Назначение и запреты

Runbook применяется для startup/readiness, MCP admission, approval,
connection validation, continuation и execution workers. Он не разрешает
deploy, production change, ручное решение approval, повтор внешнего действия,
изменение RLS/high-watermark или просмотр secret values.

Не печатать bearer, credential files, DSN, context/payload encryption keys,
JWS/JWK, TLS private keys, provider request/response, preview до redaction или
Sentry DSN. Диагностика использует только закрытые status/reason codes,
timestamps, generation и opaque UUID/digest.

## Read-only preflight

1. Зафиксировать Git SHA и immutable image digest.
2. Проверить metadata Deployment, migration Job, Service, ServiceAccount,
   SecretProviderClass и NetworkPolicy без чтения Secret.
3. Проверить, что runtime Pod имеет только gateway credential CSI volume, а
   agent Pod не имеет provider credential, payload keyset или gateway DSN.
4. Проверить exact destinations: PostgreSQL, control-plane, internal authority,
   Vault, identity SSO, telemetry и `integration-egress-proxy`. Прямой provider
   egress отсутствует.
5. Проверить, что authority publisher target имеет workload
   `integration-gateway`, а issuer использует только собственные
   Vault/readback/restore paths. Не читать key material.
6. Проверить, что Pod не имеет прямого provider egress: рабочий и readiness
   path должны идти через exact `integration-egress-proxy` destination.

## Startup и readiness

Startup barrier должен пройти до обслуживания MCP/API:

- strict YAML packages прочитаны без duplicate/unknown/null и имеют неизменный
  canonical digest;
- PostgreSQL DSN использует exact SNI/CA, а `session_user` совпадает с
  `integration_gateway_runtime_g<generation>` и не имеет `BYPASSRLS`;
- context key и payload keyset доставлены файлами и имеют безопасные mode;
- local authority issuer, control-plane verifier/resolver и защищённый
  `CheckReadiness` проходят тот же mTLS/application grant/signed context path,
  что рабочий RPC;
- continuation worker может выполнить тот же специализированный control-plane
  RPC path, что `SUSPEND`, decision, `BEGIN` и terminal команды;
- OIDC discovery/JWKS доступны по pinned HTTPS identity destination;
- egress proxy CA и exact SNI совпадают.
- payload keyset повторно читается readiness path и содержит active key вместе
  со всеми ключами, нужными незавершённым invocation/continuation.

Не отключать issuer, OIDC, RLS, TLS verify, schema validation или dependency
readiness. Не подменять provider proxy прямым HTTPS egress.

## PostgreSQL и миграция

Migration Job использует отдельный migrator principal. Forward-only reconcile:

1. bootstrap допускает только `integration_gateway_runtime_g<generation>`;
2. `CURRENT|NEXT|PREVIOUS` имеют bounded lifetime и членство только в
   `integration_gateway_runtime`;
3. `RETIRED` получает `NOLOGIN`, revoke membership и termination;
4. retired principal/context key не может стать active снова;
5. runtime transaction устанавливает scope только подписанным одноразовым
   context, связанным с `session_user`, generation, backend PID и transaction.

При ошибке сохранить только SQLSTATE и safe error class. Не выводить query
arguments. Не выполнять `goose down`, ручной `SET SESSION AUTHORIZATION` или
caller-set GUC.

## MCP admission

- отсутствие/неверный bearer: `401`;
- чужой durable `Mcp-Session-Id`: `403`;
- закрытая/истёкшая transport session: `404`;
- body, deadline, global/session concurrency или request count: bounded
  `4xx/429/504`;
- неизвестные definition/tool/risk/permission/generation: fail-closed до
  provider call.

При повторе проверять только request hash и semantic receipt. Нельзя создавать
новый idempotency key, если неизвестно, был ли внешний effect.

## Approval и execution

`PENDING_APPROVAL`, decision, attempt и result находятся в PostgreSQL и
переживают restart. Approval применим только к сохранённому request hash.
Одновременные approve/reject/expire/cancel/execute разрешаются row lock и
single-winner fence.

Cancel допускается только до `EXECUTING`: owner API требует exact permission,
а MCP-команда — совпадающую durable transport session. После начала provider
call cancel закрыто возвращает conflict и не маскирует возможный внешний effect.

При `UNKNOWN` не повторять действие вручную. Provider-idempotent tool может
использовать только тот же сохранённый provider key; прочий исход остаётся
`UNKNOWN` до отдельного audited repair contract. Credential rotation закрывает
новые claims прежнего generation и не позволяет старому attempt воскресить
grant.

При graceful shutdown terminal receipt записывается независимым bounded
context до остановки PostgreSQL. После аварийной остановки lifecycle worker
переводит `EXECUTING`, который старше одной минуты, вместе с attempt/result/audit
в `UNKNOWN`; recovery не выполняет повторный provider call.

## Continuation и rejoin

Invocation, approval и `SUSPEND` effect создаются одной owner transaction.
Проверять разрешено только opaque ID, action, bounded state, version/fence,
attempt count и timestamps; encrypted application grant не читать.

Ожидаемые состояния после подтверждённой команды:

| Команда | Approval | Execution | Continuation |
| --- | --- | --- | --- |
| `SUSPEND` | `PENDING` | `NOT_STARTED` | `SUSPENDED` |
| `APPROVE` | `APPROVED` | `NOT_STARTED` | `SUSPENDED` |
| `BEGIN` | `APPROVED` | `EXECUTING` | `SUSPENDED` |
| `SUCCEED` | `APPROVED` | `SUCCEEDED` | `READY` |
| `FAIL` | `APPROVED` | `FAILED` | `READY` |
| `REJECT` | `REJECTED` | `NOT_APPLICABLE` | `READY` |
| `CANCEL` | `CANCELLED` | `NOT_APPLICABLE` | `READY` |
| `EXPIRE` | `EXPIRED` | `NOT_APPLICABLE` | `READY` |

Mismatch invocation/request digest, stale version/fence либо неизвестное
состояние — закрытая ошибка; вручную менять continuation row или пропускать
команду запрещено. Пока effect не доставлен или его application grant близок к
истечению, readiness должна быть отрицательной. Сначала восстановить
control-plane/issuer path в пределах исходного grant; не выдавать новый grant
и не перепривязывать invocation вручную.

Provider не должен вызываться до подтверждённого `BEGIN`. Если
`provider_dispatched_at` уже установлен, повторный dispatch запрещён даже при
отсутствии terminal result: lifecycle фиксирует `UNKNOWN`, затем отправляет
`FAIL` с безопасным reference/digest.

После terminal `READY` agent-runner читает continuation только через
`GetIntegrationContinuation` с exact server-owned binding и подтверждает ту же
version через `AcknowledgeIntegrationContinuation`. AsyncAPI/NATS replay для
этого пути отсутствует намеренно. При stale Ack повторно прочитать текущую
версию; не изменять inbox/cursor напрямую.

## Connection validation

Gateway отправляет credentials только exact mTLS egress proxy operation
`POST /validate`. Наружу допускаются только `OK`,
`CREDENTIAL_UNAVAILABLE`, `UNAUTHORIZED`, `FORBIDDEN`,
`ENDPOINT_UNAVAILABLE`, `TIMEOUT`, `PROTOCOL_ERROR` и timestamp. Raw body,
headers, target path и credential value не сохраняются и не логируются.

## Ротация payload keyset и TLS credential

Ротация выполняется только вперёд с overlap. Сначала доставить новый key ID или
сертификат вместе с прежним material, дождаться успешной readiness после
фактической загрузки и только затем переключить active generation. Старый
payload key удаляется лишь после завершения всех invocation/continuation,
зашифрованных этим key ID. TLS CA/certificate меняются с двухсторонним overlap
и exact SNI; plaintext fallback и `skipTLSVerify` запрещены.

Если keyset не читается либо active key отсутствует, readiness обязана стать
отрицательной. Не менять `EncryptedApplicationGrant` или result payload вручную
и не переиздавать grant для обхода старого key ID.

## Alerts

- `IntegrationGatewayUnavailable`: проверить доступность реплик и Service;
- `IntegrationGatewayNotReady`: проверить startup barrier в порядке выше;
- `IntegrationGatewayContinuationFailures`: проверить issuer/control-plane path,
  grant deadline и durable effect lease без чтения payload;
- `IntegrationGatewayProviderFailures`: проверить bounded validation code,
  credential generation и mTLS egress proxy без повторного provider effect;
- `IntegrationGatewayUnknownOutcomes`: внешний dispatch уже мог состояться;
  не повторять действие, сверить только safe receipt/digest и открыть отдельный
  audited repair flow.

Каждый alert обязан ссылаться на абсолютный URL этого runbook.

## Rollback

Rollback приложения — возврат предыдущего immutable image digest после
остановки новых claims. Миграция не откатывается. Pending approvals,
execution/continuation intents и receipts сохраняются; запрещено удалять их,
перепривязывать application grant или повторять provider effect вручную. При
несовместимой схеме исправление выполняется новой forward-only migration.
