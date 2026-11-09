---
id: OPS-INTEGRATION-RESOURCE-POLICIES-2073
title: Области ресурсов и настраиваемое подтверждение инструментов
type: operations
status: approved
owner: architect
version: 1.0.0
updated: 2026-11-09
---

# Области ресурсов и подтверждение инструментов

Источник требований: [#2073](https://github.com/codex-k8s/kodex/issues/2073),
эпик [#1830](https://github.com/codex-k8s/kodex/issues/1830). Этот контракт
задаёт полный unit; наличие документа и library-тестов не подтверждает
готовность API/PWA/gateway либо установку функции.

## Продуктовые правила

Владелец Connection выбирает внешнюю область. Владелец Grant отдельно выбирает
инструмент, получателя и одну из политик `NONE`, `HUMAN_EACH_EFFECT`,
`HUMAN_SCOPED`. Риск операции остаётся отдельным свойством: `NONE` не превращает
запись в READ и не меняет idempotency/recovery. Прежние defaults и exact
published revisions сохраняются, новые права старым grants не наследуются.

`NONE` пропускает только integration Gate. Workflow owner gates, mailbox/SSH
permits, provider branch protection и обязательный допуск файлов/образов
проверяются независимо. Администратор явно включает автономную политику;
модель не может выдать её себе. Для READ default — `NONE`, при явной настройке
доступно подтверждение каждого вызова.

### GitLab

`PROJECT` сохраняет один exact repository. `GROUP` ограничен одной выбранной
группой; подгруппы включаются явно. `INSTANCE` означает все проекты, доступные
конкретной credential на одном настроенном origin, а не административный доступ.
Группа `team/services`, проект `team/services/alpha` и origin
`https://example.invalid` — синтетические примеры.

Список проектов пагинируется; group listing исключает shared foreign projects.
Выбор проекта типизирован. До вызова проверяются формат и lexical boundary,
затем gateway получает свежие project/group metadata на том же доверенном
origin и проверяет canonical namespace и exact identity. Похожий prefix,
encoded slash, traversal, другой origin, чужой/переехавший проект и неподключённая
подгруппа не расширяют область. Успех API чтения не доказывает право записи.

Код не содержит конкретных instance, group или project IDs. Server search
используется лишь при поддержке API/лицензии. Ограниченный поиск в проектах
не объявляется полным глобальным поиском по содержимому.

## Предметная область временного согласования

Connection/Grant/definition pins и инициатор проверяются всегда. Таблица
задаёт минимальную identity; оператор может добавить ограничивающие поля,
но не убрать обязательную identity. Для создания объекта областью является
его существующий родитель и, когда нужно, branch/ref.

GitLab project selector содержит canonical path и numeric provider ID. После
свежего namespace lookup рабочий запрос использует этот проверенный ID,
поэтому повторное использование прежнего пути другим repository не переносит
на него согласование. Число ограничено точным диапазоном текущего JSON/Struct
транспорта; округление большего ID не используется как fallback.

| Семейство инструментов              | Минимальная предметная область                                              |
| ----------------------------------- | --------------------------------------------------------------------------- |
| MR/PR comment или discussion create | repository + exact MR/PR                                                    |
| Comment/discussion update/delete    | repository + exact comment/discussion                                       |
| Issue comment create                | project/repository + exact issue                                            |
| Issue read/update/transition        | exact issue; transition дополнительно exact transition                      |
| MR/PR read/update/merge/ready       | repository + exact MR/PR; commit/branch guards сохраняются                  |
| Pipeline retry/cancel/read          | repository + exact pipeline                                                 |
| Job retry/cancel/read/trace         | repository + exact job                                                      |
| Repository file read/write          | repository + ref/branch + exact file path                                   |
| Branch create/delete/read           | repository + exact branch/ref                                               |
| Commit create                       | repository + branch; manifest и expected parent сохраняются                 |
| Issue create                        | configured project/repository                                               |
| MR/PR create                        | repository + source/target branches                                         |
| Confluence page update/read         | exact space + page                                                          |
| Confluence page create              | exact space + parent page                                                   |
| Space/project metadata или catalog  | точная разрешённая область Connection                                       |
| Custom OpenAPI tool                 | объявленные owner metadata typed identity paths + pinned server/method/path |

Неизвестная или неполная identity не становится разрешением на все ресурсы.
Body комментария не заменяет номер MR. Разрешение комментировать один MR не
разрешает другой MR, merge, push или pipeline. Отдельный effect получает
собственные intent, key, receipt и audit, даже если consent переиспользуется.

## Срок, получатель и отзыв

Срок и лимит использований настраиваются типизированной policy. Существующие
24 часа и100 reserved effects сохраняются как legacy default, а не остаются
единственным SQL-литералом. Неявное бессрочное разрешение запрещено. Сервер
проверяет bounds до сужения типов и считает expiry по часам PostgreSQL.

Run-scoped legacy consent остаётся в своём root. Для явно выбранного
межзапускового consent требуется та же проверенная identity инициатора,
получатель/Grant, Connection, tenant и предметный объект. Каждый новый root
всё равно получает собственные lease/RuntimeRevision и свежую авторизацию;
consent не является worker grant и не заменяет delegation lineage.

Существующий `ChangeIntegrationGrant` задаёт `NONE`, `HUMAN_EACH_EFFECT` либо
`HUMAN_SCOPED` отдельно для инструмента и получателя. Optional versioned
`approvalScope` metadata содержит обязательные input paths, верхние bounds и
разрешённые `ROOT_RUN`/`INITIATOR_RESOURCE`; managed revision может только
сужать их. Это не скрытая per-instance проверка конкретного tool ID в PWA.

`PlatformCommandService.ResolveOwnerGate` принимает optional typed
`integrationApproval` только для `APPROVE` действующего `HUMAN_SCOPED` intent:
`durationSeconds`, `maxEffects`, `lifetime`. Отсутствие выбора сохраняет
24 часа/100 reservations/`ROOT_RUN`, ограниченные более строгим tool policy.
Явный неполный объект либо выбор для EACH/REJECT/обычного workflow gate
отклоняется до owner effects. Первый invocation входит в лимит.

Межзапусковый вариант явно выбирается человеком. Сервер получает initiator
из заблокированного canonical root и требует `USER`; ID инициатора нельзя
передать в решении. Новый root получает собственные invocation/lease/revision.
Grant/Connection/recipient/object pins сохраняются, старый worker grant не
переносится. Удаление исходного root вправе отозвать связанное согласование;
оно не воскрешает после удаления либо повторной публикации конфигурации.

Специализированные `PlatformQueryService.ListIntegrationApprovalConsents` и
`PlatformCommandService.RevokeIntegrationApprovalConsent` обслуживают
`/api/v1/me/integration-approval-consents` и
`/api/v1/me/integration-approval-consents/{ref}/revoke`. Каталог — только
согласования текущего root initiator, с актуальной видимостью подключения и
исходного Run; вывод не создаёт authority. Cursor/page ограничены, скрытые
scope/tenant не становятся oracle. Revoke использует current ownership,
OCC/idempotency и DB clock. Обычный project admin не подменяет initiator;
владелец конфигурации по-прежнему может отозвать сам Grant и все его scopes.

Самостоятельный revoke сохраняет один owner audit и idempotency receipt без нового
platform event: authoritative read path — self catalog и ответ специализированной
команды. Gateway/worker читают durable row при admission, event-кэш не выдаёт
полномочий. Первая выдача сохраняет существующее Gate/Run event; expiry queued
invocation сохраняет существующий integration result event и continuation.

Revoke и reservation блокируют одну durable scope row. Reservation сама по себе
не означает начатого внешнего действия: перед авторитетным call-start сервер
снова проверяет scope. Уже принятый call-start до отзыва остаётся отдельным
effect с собственным receipt/reconciliation; отзыв не обещает его отмену.
Состояния ACTIVE/EXPIRED/EXHAUSTED/REVOKED различимы в каталоге. Сервер проверяет
DB clock и счётчик при каждом вызове. Повтор revoke читает прежний receipt;
новый consent появляется только через новый реальный Gate.

Истечение либо отзыв до call-start не оставляют READY invocation навсегда
невыбираемой. Существующий owner claim/reconcile path ограниченно фиксирует
известное отсутствие внешнего эффекта и передаёт причину continuation; новая
попытка требует нового действительного согласования. RUNNING/UNKNOWN не
переводятся в «эффекта не было» только из-за отзыва. Разрешённый read-only
recovery прежнего внешнего действия сохраняет собственные lineage, credential
и reconciliation проверки; новый write этим путём невозможен.

UI показывает первый конкретный effect, область будущих операций, получателя,
срок, лимит и возможность отзыва. `HUMAN_EACH_EFFECT` нельзя незаметно превратить
во временный допуск кнопкой решения. При `HUMAN_SCOPED` человек может ограничить
согласование одним вызовом либо выбрать допустимые policy bounds.

Отзыв, expiry, изменение Grant/Connection/definition и credential readiness
блокируют новые вызовы. Уже начатый внешний effect остаётся в собственном
lifecycle; отзыв не обещает отменить подтверждённую запись задним числом.
Ни scope, ни решение Gate не меняют root initiator и не выбирают credential
последнего автора/approver. Shared fallback отсутствует.

## Сквозной маршрут и владелец

Actor из проверенного transport → typed API/PWA → control-plane owner command
с OCC/idempotency → Grant/Connection/definition → immutable RuntimeRevision →
managed tool → owner admission/claim → integration-gateway → scoped provider API
→ bounded result/receipt → owner completion/audit/event → continuation/PWA.

Control-plane владеет policy, consent, counters и expiry. Integration-gateway
владеет provider projection и проверкой текущего внешнего объекта. SQL не
выполняется в gateway, PWA не принимает решение об авторизации. Policy
переключается только owner-командой; caller input и prompt не являются authority.

| Переход                   | Проверка и результат                                                                         |
| ------------------------- | -------------------------------------------------------------------------------------------- |
| Create/update Grant       | owner/tenant/recipient, declared policy/identity, bounds, OCC; один receipt/audit/event      |
| Launch/turn/delegation    | fresh grants/credentials и exact root lineage; immutable policy snapshot                     |
| Invoke NONE               | обычная owner admission; нет integration Gate; effect lifecycle неизменен                    |
| Invoke EACH               | exact immutable intent; OPEN Gate до одного разрешённого вызова                              |
| Invoke SCOPED без consent | exact preview/owner Gate; самовыдача исключена                                               |
| Approve scoped            | owner/OCC + live invocation/version; атомарные consent, expiry, первая reservation и binding |
| Reuse                     | exact identity/actor/pins, DB clock, атомарный bounded counter; новый effect key             |
| Expiry/revoke             | новые reservation запрещены; история/receipt и uncertain effects сохраняются                 |
| Claim/authorize           | свежая owner authority и personal binding; provider object проверяется до HTTP effect        |
| Cancel/terminal           | прежний граф закрывается атомарно; RUNNING write может остаться UNKNOWN_OUTCOME              |
| Retry/unknown response    | readback/reconciliation; нельзя повторить известный успешный create/write                    |
| Restart/resume            | persisted counters/expiry + fresh RR/credential; RAM не источник полномочий                  |
| Definition upgrade        | exact legacy contract остаётся исполнимым; новая policy только через новый owner pin         |

## Проверки и ручная приёмка

Обязательны synthetic library/gateway tests, disposable PostgreSQL concurrency
и native migration, OpenAPI/Proto/schema/codegen, frontend policy/scope forms и
bounded Human Gate preview. Browser contour запускается только на разрешённой
disposable установке. `NOT RUN` не считается `PASS`.

Проверяются три GitLab режима, pagination, shared projects, subgroup/prefix
confusion, 401/403/404/429, timeout и redirects. Для approval — один MR с разными
body, другой MR/repository/actor/tenant, expiry/revoke, budget race, delegation,
resume, unknown COMMIT и отсутствие повторного внешнего effect. Прежний
PROJECT/default policy и исторические snapshots проходят регресс отдельно.

Live deploy, promotion и настройка реальных credentials не являются локальной
проверкой. Для новых schema inputs нужна отдельная forward-only qualification;
старый image сам по себе не является доказанным rollback.

### Установка и восстановление037

Forward-only миграция `20261109000100` применяется после036. Normal и backup
имеют отдельные полные каталоги; явный SELECT-only restore037 сохраняет
прежние backup permissions и добавляет только две ранее не читаемые таблицы
согласований и grants. Запись backup reader, неизвестная схема и смешанные
профили закрываются. Старые migration/proof assets не меняются.

Новый custody verifier037 учитывает только два подтверждённых эффекта
`pg_dump/pg_restore`: три точные эквивалентные формы группировки `AND` и
перенумерацию колонок после исторического удаления колонки. Триггер связывается
с именем колонки; изменение его события, функции, аргументов, состояния,
условия или permissions по-прежнему отклоняется. Другие формы ограничений
не нормализуются. Проверка выполняется и на свежей схеме, и на реально
восстановленной копии, включая сохранение счётчика и запрет старого allocator.
Это проверка каталога и watermark на disposable PostgreSQL18.3; она не
объявляет выполненным восстановление действующей распределённой установки.

### Ручная проверка владельца

1. Создать отдельное подключение GitLab2.0.0 с выбранным `PROJECT`, `GROUP`
   либо `INSTANCE`. Для группы отдельно выбрать включение подгрупп; смешанные
   selectors должны отклоняться. Указать собственную credential.
2. Выдать сотруднику нужные READ-инструменты с `NONE`: список проектов,
   repository tree/file, MR и pipeline. Убедиться, что результаты приходят
   через gateway в выбранной области и соседний namespace недоступен.
3. Переключить выбранный READ на `HUMAN_EACH_EFFECT`: до решения Gate запрос
   во внешний API не выполняется. Другие grants сохраняют свою политику.
4. Для комментария в synthetic MR выбрать `HUMAN_SCOPED`. В Gate проверить
   repository и exact MR, задать срок и лимит; отдельно явно выбрать
   межзапусковое действие для того же инициатора и сотрудника. Другой MR,
   пользователь, получатель либо изменённый grant требуют нового решения.
5. В «Мои подключения» отозвать согласование. Новые вызовы блокируются;
   счётчик не сбрасывается, уже начатый внешний эффект сохраняет receipt и
   штатную reconciliation. Повторный отзыв с тем же ключом идемпотентен.
6. Повторить запуск со старым exact published definition pin: его defaults,
   область и прежняя lifetime не должны наследовать новые разрешения.

## Миграция и сопровождение

037 использует обычную forward-only Goose-миграцию. Проверки установщика
читают конкретные условия перехода036→037 и объекты текущего изменения;
полные snapshots прошлых каталогов и генератор их переписывания не нужны.
Backup restore возвращает ограниченный SELECT атомарно, без временного отзыва
чтения ради сравнения с manifest. Его SQL встраивается в control-plane CLI
и доставляется новым immutable image вместе с остальной конфигурацией.

Границы readback и команды native-проверок описаны в
[runbook установки](../runbooks/portable-install.md#переход036037-и-backup-reader).
Эти проверки не подменяют функциональные тесты двух пользователей,
делегирования, expiry/revoke и продолжения неизвестного внешнего эффекта.
