---
id: OPS-MAILBOX-CONFIGURATION-CHECKPOINT-1046
title: Передача mailbox configuration и остатка D5/D6
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-05
---

# Граница checkpoint

Issue #1046, основа `67aa98d77`. Этот checkpoint не завершает D5 и не
добавляет публичные RPC. D2/D4 не изменены. HTTP dependency:
`cfb18a17e2048f5056ddd46c8fccbd3f1e18a3d6`, draft PR #1066.

## Реализовано

- `entity.EmailMailboxSpecification`: typed редактируемые параметры без tenant,
  connection, revision, credential generation, managed_by и source. Credential
  descriptors не содержат значений секретов.
- `emailpolicy.BoundSpecification`: неполный черновик допустим, размер <=256 KiB;
  folders <=100, recipients <=1000, policies ограничены закрытым каталогом,
  folders одной policy <=100.
- `emailpolicy.MaterializeMailbox`: отдельный server-owned binding, EnvelopeFrom
  равен Sender; копии endpoint и вложенных коллекций не разделяют память с входом.
  Полная проверка использует существующий typed EMAIL validator. Helper ещё не
  подключён к UI owner-команде: происхождение binding обязан проверить CP owner.
- `emailpolicy.DecodeConfiguration`: исполняемый trusted import также отклоняет
  protocol/port/TLS вне матрицы #1029: SMTP 465 implicit / 587 STARTTLS,
  IMAP 993 implicit / 143 STARTTLS, POP3 995 implicit / 110 STARTTLS.
- Explicit all-allow mailbox допустима; безусловный Human Gate не возвращён.

Новые SQL, policy, Proto, события и workers не добавлены. Startup import
сохраняет прежние authority/watermark semantics. Network shape не доказывает
DNS pins, применённую NetworkPolicy или готовность endpoint.

## Проверки

- PASS: `go test -race ./internal/domain/service/emailpolicy`: binding,
  отсутствие owner-полей и aliasing, draft bounds, TLS/port mismatch, all-allow,
  отказ trusted import на неподдерживаемом SMTP port.
- PASS: `go vet ./internal/domain/service/emailpolicy ./internal/domain/types/entity`.
- PASS: targeted disposable PG `^TestBootstrapComponent$/email_configuration`,
  включая `authorization_and_report_owner_lifecycle`.
  Журнал: `/tmp/kodex-1046-email-config-network-pg.log`.
- PASS: `git diff --check`.
- NOT RUN: общий PG suite, полный protected HTTP→CP→EMAIL, live/deploy.
  Proto/codegen/render не затронуты и не перезапускались для этого checkpoint.

## Остаток D5

1. Typed owner CRUD и UI/YAML import, OCC/idempotency/audit, server lineage,
   Git copy/detach/writeback; неполный DRAFT отдельно от VALID digest.
2. Durable publication PENDING и immutable snapshot; READY только после
   точного Secret и mail-network generation apply/readback.
3. Исполняемый owner путь применения результата #1029 `f8da405af`, включая
   ConfigMap/NetworkPolicy/Deployment pins, минимальные RBAC, rollout/readiness
   и restart recovery. CLI render без apply не завершает UI lifecycle.
4. Связать credentials из `67aa98d77` с mailbox draft; сохранить immutable keys
   и определить уборку не привязанных ключей после OCC race.
5. Подключить актуальный root EMAIL consumer reload checkpoint и проверить
   публикацию вместе с ним. Root сообщил PASS своего reload/release suite;
   это не локальная проверка данного дерева.

Номера `20260904000625`–`20260904000629` объявлены резервом D5;
файлы миграций здесь не создавались. Wildcard/direct fallback запрещены.

## Остаток D6

D6 не начинался. Нужен encrypted staged Runtime Secret lifecycle:
save/validate/publish/discard, owner-resolved scope, exact version/idempotency,
fresh authorization на применимых действиях, отсутствие plaintext в DB,
ответах, audit и событиях. Требуются доставка и ротация encryption key,
публикация через существующий secret owner path, terminal cleanup/recovery,
Proto/generated/policy/client/HTTP handoff и targeted contract/PG tests.
Не переиспользовать purpose EMAIL_EFFECT_RECONCILIATION для Secret.

По поручению владельца после checkpoint и интеграции D2 работа останавливается.
D4 не реализован. Итоговый unit остаётся одним PR #1046.
