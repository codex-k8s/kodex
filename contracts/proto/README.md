---
id: CONTRACT-DOC-002
title: Внутренние Proto/gRPC-контракты
type: contract-guide
status: approved
owner: architect
version: 1.0.0
updated: 2026-07-28
---

# Внутренние Proto/gRPC-контракты

Исходные контракты располагаются по пути:

```text
contracts/proto/<service>/v<major>/*.proto
```

Proto является источником истины для внутреннего синхронного API. Generated Go
code размещается рядом с потребителем и вручную не редактируется.

## Package и версии

```proto
syntax = "proto3";

package example.v1;

option go_package =
  "<module>/services/internal/example/internal/generated/example/v1;examplev1";
```

- package содержит major version;
- несовместимое изменение создает новый `v<major>`;
- удаленные field numbers и names резервируются;
- field number не переиспользуется с другим смыслом;
- package owner зарегистрирован в `contracts/registry.yaml`.

## Service и методы

- Один service объединяет связную capability, а не все операции компонента.
- Method называется command/query намерением: `CreateRecord`, `GetRecord`,
  `ListRecords`, а не transport/SQL действием.
- Каждый method имеет собственные request/response messages.
- Generated `Empty` допустим только при фактическом отсутствии результата;
  успешная mutation обычно возвращает ID/version или безопасный snapshot.
- Batch/list имеют явные лимиты, стабильный порядок и pagination contract.

## Authority

Обычный request не содержит доверенные `actorId`, `tenantId`, `organizationId`,
`permission` или workload identity. Эти значения приходят из проверенной gRPC
metadata/signed context либо разрешаются владельцем данных.

Если идентификатор нужен как бизнесовый filter, контракт отдельно объясняет,
почему caller вправе его выбирать и как service проверяет ownership.

## Mutation

State-changing request включает:

- idempotency key;
- expected version для изменения существующего aggregate;
- только business input;
- immutable reference/version внешнего snapshot, если он утвержден сценарием.

Owner, server timestamp, aggregate version и event sequence назначаются
сервером. Idempotency и `expectedVersion` не заменяют authorization.

Unknown outcome после `Unavailable` повторяется только с тем же idempotency key
и тем же семантическим request.

## Presence и validation

- Optional field используется только когда нужно различить «не передано» и
  zero value.
- Невалидное нулевое значение enum не получает бизнесовый смысл по умолчанию.
- Закрытые enum отклоняют unknown на входной границе.
- Строки, collections и payload имеют max limits.
- Timestamp проверяется и нормализуется по утвержденной precision.
- UUID/ID/decimal/money получают устойчивое representation и domain caster.

Proto validation выполняется до domain handler. Validation schema не заменяет
domain constructors и cross-field invariants.

## Ошибки

Внутренний API использует типизированные domain errors, отображенные в
канонические gRPC codes по `GUIDE-DOC-005`:

- request validation -> `InvalidArgument`;
- отсутствующий ресурс -> `NotFound`;
- authority denial -> `PermissionDenied`;
- unauthenticated caller -> `Unauthenticated`;
- OCC/idempotency conflict -> `Aborted` или утвержденный conflict code;
- dependency outage -> `Unavailable`;
- unexpected defect -> `Internal`.

Текст status безопасен, стабилен и не содержит SQL, token, PII или provider
diagnostics.

## Реализация

Server path:

```text
interceptors -> handler -> request caster -> domain service
             -> response caster -> status mapping
```

Client path:

```text
domain client port -> service adapter -> generated client
                   -> deadline + mTLS + signed context
```

Generated request не передается в домен, а generated client не импортируется
domain service напрямую. Полный профиль задают `GO-DOC-001` и `GO-DOC-005`.
