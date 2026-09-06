---
id: OPS-RUNTIME-ARTIFACT-TRANSFER-1046
title: Потоковое чтение exact runtime artifact
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Контракт MVP-UI-37/61

Refs #1046, #1025, #1026, Epic #1018. Продуктовый размер input остаётся
512MiB. Старый unary body ограничен 32MiB, поэтому он не завершает разрешённый
путь больших входных файлов. Новый additive transfer передаёт bounded chunks;
размер остальных unary envelopes не повышается.

| Переход | Actor и authority | Owner, version и readback | Consumer и результат |
| --- | --- | --- | --- |
| Открытие | Authenticated runner callback → controller; exact lease/fence/generation, input либо catalog membership | RuntimeWork.StreamExecutionArtifact; закрытый method profile71, signed canonical initial Proto SHA-256, workload mTLS/bearer и durable JTI acceptance | Initial request не отправляется до proof issuance; server не вызывает owner до verification actual decoded request |
| Source resolution | Actor/root/Agent/Project принадлежат CP lease | Existing authoritative ReadExecutionArtifact eligibility: current permissions/lifecycle/scan и immutable input/Skill/catalog revision; owner audit | Один safe metadata header, никаких object locators, потом chunks фиксированного максимума |
| Завершение | Та же lease/generation и source | Повторный owner read проверяет current eligibility; exact source size/digest сверяются до terminal frame | Controller сохраняет во временном private bounded spool, проверяет полный size/SHA-256 и EOF до выдачи bytes |
| Wrong request/replay | Изменение ref/lease/fence/generation не соответствует signed initial digest | Verifier отклоняет до domain read; повторное открытие требует нового proof/JTI | Файл не выдаётся, hidden retry отсутствует |
| Cancel/expiry/revoke/delete | Caller cancellation либо актуальный owner lifecycle | Stream прекращается; final eligibility не проходит; domain event отсутствует, authoritative state остаётся у CP | Закрыть reader/stream/spool и удалить временный файл; частичное тело не становится artifact |
| Повторное чтение | Новый подтверждённый read, тот же exact immutable source | Новый proof и owner audit, без новой task/attempt/write receipt | Права не расширяются; input snapshot не переписывается |

Новый server-stream использует существующий `UNARY_PROTO_SHA256` профиль для
своего единственного initial request. Shared interceptor включается только
для закрытого списка таких методов. Client откладывает открытие до SendMsg,
считает canonical digest, получает proof и подписанный context. Server
проверяет mTLS/context и actual decoded request до входа в обработчик owner.
Legacy `STREAM_SESSION` сохраняет свой контракт; произвольный stream не
получает initial-request mode из клиентских metadata.

Controller download не передаёт provider credentials, ticket или URL с token
в MCP metadata. Произвольный path, Project, object key и чужие revisions не
поддерживаются. Temporary spool не является authority/replay store: он
принадлежит одному bounded запросу и удаляется после success/error/cancel.
Runtime-controller deploy получает отдельный ограниченный writable volume;
provider не получает этот volume. Production/staging здесь не вызываются.

Context7 `/grpc/grpc-go`: server streaming Send/Recv, EOF, context cancellation,
flow control и единственный Send/Recv на stream. Официальные исходники:
https://github.com/grpc/grpc-go/blob/master/examples/gotutorial.md и
https://github.com/grpc/grpc-go/blob/master/Documentation/concurrency.md.

Producer и shared boundary реализованы на родителе
`a6e4c03e99cf155e2d15828ead04ad43d4eab37b`, дерево которого совпадает с
объединённым CP `89a8388cdf0dfee29eebc21a43da0ae162cf82f8`.

- PASS: targeted race transport/service/app; итоговый service1.229s, transport
  ранее1.697s и app1.067s без изменения их исходников. Fixture33MiB+7 проверяет
  chunk64KiB, checksum/size и отсутствие изменения уже отправленного slice.
  Negative paths: short/extra/digest/revoked/cancel/send/close/oversize.
- PASS: exact stream permission/workload после ResolvePrincipal; отсутствие
  owner read при неверных полномочиях, передача текущего owner rejection.
- PASS: полный shared authority race/vet, полный CP vet, Proto lint/build/
  canonical generation/clean replay, policy71 clean replay и authority ABI
  render. Первый Proto lint выявил reuse/name; исправлено отдельными request
  и response. Buf rate limit закрыт каноническими exact local plugins.
- PASS: adapter fixtures связывают proof с actual initial Proto; substitution,
  missing/duplicate auth, отсутствие mTLS peer и replay rejection не достигают
  owner. Это fixtures issuer/verifier, а не live cryptographic acceptance.
- NOT RUN: новый consumer/private spool, действующий protected CP↔controller
  stream, quota/non-root cleanup и оба итоговых render. Полный baseline общего
  SHA, staging и runtime/provider acceptance также NOT RUN.

Завершение owner или consumer отдельно не считается полной приёмкой этого пути.
