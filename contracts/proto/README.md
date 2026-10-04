---
id: CONTRACT-DOC-002
title: Внутренние Proto/gRPC-контракты
type: contract-guide
status: approved
owner: architect
version: 1.0.1
updated: 2026-10-04
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

## Ограниченный каталог для подтверждаемой настройки

`RuntimeWorkService.SearchAssistantResources` обслуживает три взаимоисключающих
режима: обычный поиск, каталог определений интеграций и типизированный
`assistant_configuration_catalog`. Новый режим использует тот же зарегистрированный
метод, mTLS, signed context и проверку точных lease/fence/generation. Ссылка
`assistant_ref` выбирает ресурс, но не выдаёт полномочий: сервер разрешает
инициатора, исходный диалог и сохранённую область целевого помощника до чтения.
PROJECT может настраивать только свой профиль, SYSTEM — себя либо доступный
проектный профиль. Обычный сотрудник не становится помощником из-за пустого
project ref.

Каталог возвращает максимум десять записей закрытого вида с ограниченным
поиском и стабильным порядком; offset ограничен, нулевой next offset означает
конец. Аккаунты и модели читаются только для совместимого активного runtime
профиля. Ответ не содержит credential, auth refs, значения секретов, Dockerfile
или произвольные исполняемые параметры. Пустой каталог не означает разрешение
угадывать ссылки или объединять области организации и проекта.

Специализированный discovery не заменяет доменную eligibility обычного чтения:
`PROVIDER_ACCOUNTS` проверяет `provider.account.view` для каждого аккаунта,
включая отдельный instance ALLOW; `MODELS` проверяет `organization.view`, как
штатный ListModelCapabilities. Право управления помощником не назначает права
чтения другого вида ресурса. Совместимость аккаунта с runtime profile и
проверка current credential являются дополнительными условиями, не заменой
access policy. Ограничения схемы БД обязаны поддерживать тот же закрытый
набор scope/resource kind, что canonical registry, без фиктивного project.

Только записи `ASSISTANTS` могут содержать `runtime_environment_ref` текущего
опубликованного окружения. Это locator, не grant: изменение требует повторного
разрешения полного assistant/profile/project/environment tuple. SYSTEM может
подготовить окружение, инструкции или перепривязку проектного помощника через
`projectAssistantRef` независимо от открытого экрана; PROJECT — только себе.
Обычная операция сотрудника по-прежнему требует точного screen context.
Сохранённые owner/version pins неизменяемы при редактировании; изменение
состояния после подготовки не устраняется скрытым перепривязыванием.

Значения reasoning принадлежат exact каталогу модели и не ограничиваются
перечнем заранее известных имён. Producer и consumer используют canonical
структурную проверку, ограничение коллекции и проверяют уникальность и
принадлежность default к этому набору.

Чтение не меняет состояние и не создаёт событие. Последующая специализированная
операция заново разрешает actor/owner, снимки каталога, версии конфигурации и
ручного черновика. Подтверждение плана не заменяет эти проверки; прежний
каталожный ответ не является grant. Публикация модели и reasoning выполняется
одной транзакцией владельца и влияет только на следующие immutable runtime
ревизии, не переписывая уже материализованный запуск.

В подтверждённом плане из нескольких операций перенос версии допустим только
для точного эффекта предыдущей выбранной canonical операции над тем же
aggregate в той же транзакции владельца. Версия и зависимости берутся из
фактически возвращённого результата, не из caller payload или произвольного
свежего read. Исходная immutable revision, её digest и snapshots не меняются.
Внешний drift, отзыв прав, другой target и неизвестный эффект по-прежнему
закрыто отклоняются; последующая ошибка откатывает все эффекты плана.

Проект целевого помощника не заменяет область исходного диалога. Ответ,
окончательное событие плана и persisted conversation используют область
источника; audit и события изменённых объектов — область своего target.
Перепривязка диалога допустима только отдельным утверждённым переходом создания
проекта, а не как побочный эффект изменения проектного помощника.
