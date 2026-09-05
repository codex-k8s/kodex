---
id: OPS-DOC-1046-CONFIGURATION-WRITEBACK
title: Подтверждённый Git write-back через отдельную ветку и PR
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Граница и источники

Issue #1046, Epic #1018, исходный CFG-01: изменение Git-managed RoleImage либо
IntegrationDefinition создаёт видимый план, отдельную ветку и PR/MR. Source
branch не изменяется. Последняя опубликованная runtime revision сохраняется
до внешнего merge и успешной обычной синхронизации `ManagedConfigurationSourceWork`.
Этот документ фиксирует согласованный контракт реализации; до завершения
owner, consumer и deploy проверки write-back имеет статус NOT RUN.

CP владеет proposal, root actor, exact configuration/source/connection versions,
accepted base commit, immutable content/digest, server-assigned branch и
approval digest. Gateway переносит проверенный actor/organization; payload
не назначает authority. Для RoleImage нужны существующие права управления
конфигурацией, чтения/изменения source и `image.build`; для глобального пакета
— `organization.manage`. Отдельно проверяются `integration.manage`, текущая
connection/credential и actual bound package с разрешёнными write operations.
SHIPPED system-base не изменяется. UI/Git configuration bytes не содержат
credential values и не возвращаются в summary/list.

Human Gate — отдельный подтверждаемый пользователем proposal, без фиктивных
Run, node или system assistant. Approve является отдельной командой после
просмотра плана, а не побочным эффектом Prepare. Подтверждение связывает exact
proposal version/digest, repository, base/path, content SHA256 и оба внешних
эффекта. Перед каждым эффектом CP повторяет текущие полномочия root actor и
approver, exact connection/credential/package и непротиворечивость source.

## Карта переходов

| Инициатор и операция | Fence и authority | Устойчивый переход, событие и read path |
| --- | --- | --- |
| UI PrepareRoleImageGitWriteBack / PrepareIntegrationDefinitionGitWriteBack | Exact configuration If-Match, source version, текущий owner/connection; idempotency key | WAITING_APPROVAL, immutable plan, audit и receipt в одной TX; события нет, protected Get/List |
| Повтор Prepare | Сначала eligibility, затем тот же intent digest | Прежний proposal, без новой ветки/PR; receipt/Get |
| UI Approve | Exact proposal If-Match и approval digest, отдельное явное действие | QUEUED и approver lineage; audit/receipt, события нет, Get/List и worker claim |
| UI Reject | Exact proposal If-Match, текущий owner; только WAITING_APPROVAL | REJECTED tombstone; audit/receipt, события нет, Get/List |
| UI Cancel до начала эффекта | Exact owner/proposal If-Match | CANCELLED, claim/grant отозваны в той же TX; audit/receipt, события нет, Get/List |
| Approval expiry / отзыв полномочий до эффекта | Owner проверяет deadline и текущие actor/approver/connection | EXPIRED либо FAILED/AUTHORITY_CHANGED, старый grant закрыт; audit, события нет, Get/List |
| Worker claim | Exact integration-gateway workload, owner-assigned attempt/generation/fence/lease | CLAIMED, immutable private snapshot; audit, события нет, claim/Get |
| Renew до effect | Полный tuple и действующий lease, общий deadline | Новый bounded expiry; audit/receipt, события нет, lease read |
| Crash/lease expiry до effect | Предыдущий claim закрывается; не более 3 попыток подготовки | QUEUED с новым attempt либо FAILED; audit, события нет, claim/Get |
| Begin branch effect | Exact live claim; candidate commit имеет один parent=base, exact approved path/content; актуальная authority | Durable BRANCH effect intent до Git push, невозобновляемый effect grant; audit/receipt, события нет, Get/worker read |
| Branch effect/readback | Server-owned proposal branch; expected absent либо exact owner-recorded old OID; source branch не трогается | Exact candidate branch receipt, затем подготовка PR effect; audit, события нет, Get/worker read |
| Begin PR/MR effect | Точный branch receipt, candidate/base и marker proposal; повторная authority | Durable PULL_REQUEST effect intent до provider mutation; audit/receipt, события нет, Get/worker read |
| PR/MR readback | Точные repository/head/base/marker и candidate; provider ref принадлежит результату этого intent | SUCCEEDED с безопасной ссылкой на PR/MR, оба effect receipts; source/runtime не изменяются, audit, события нет, Get/List |
| Lost ACK / timeout после начала любого эффекта | CP сохраняет intent, candidate и предыдущие receipts | UNKNOWN_OUTCOME; прежний effect не отправляется повторно. Audit, события нет, Get/List и read-only recovery |
| Cancel/expiry/revoke после начала эффекта | Прежний effect нельзя считать отменённым без доказательства | UNKNOWN_OUTCOME, запрет следующего нового эффекта; только read-only recovery; audit, события нет, Get/List |
| Recovery branch | Exact stored candidate/ref/base и readonly provider read | Подтверждённый receipt либо UNKNOWN_OUTCOME; отсутствие ветки не доказывает отсутствие исторического эффекта, resend запрещён |
| Recovery PR/MR | Exact durable marker, repository/head/base/candidate и все страницы bounded lookup | Exact единственный receipt либо UNKNOWN_OUTCOME; неоднозначность не создаёт второй PR |
| Duplicate Complete | Exact completed claim/effect/input tuple | Прежний durable receipt; другой payload конфликтует, audit не дублируется |
| Terminal reload | Текущая owner eligibility до чтения | Tombstone/receipt сохраняется; событие отсутствует, авторитетен Get/List с actor/filter-bound cursor |
| Внешний merge и обычный SourceWork | Существующий source root actor/connection и fast-forward/readback protocol | Только здесь новая опубликованная revision/build; write-back receipt сам runtime не меняет |

Новая версия конфигурации, перенастройка source, detach, connection rebind или
отзыв credential атомарно закрывают ещё не начатую работу. Начатый внешний
эффект сохраняет UNKNOWN и свой recovery fence; его нельзя забыть удалением
агрегата. COPY создаёт независимый UI set, не переписывая исходный proposal.
Отсутствие события для каждой строки компенсируется authoritative protected
read, command refetch и bounded polling незавершённого proposal в PWA.

## Исполнение и поставка

Public mapping: `POST /api/v1/role-image-configurations/{configurationRef}/git-write-backs`
и `POST /api/v1/integration-definition-configurations/{configurationRef}/git-write-backs`
вызывают соответствующие Prepare RPC. Input содержит `expectedSourceVersion`
и `content`; формат назначается из текущего Git source (JSON либо YAML).
`If-Match` относится к configuration version, idempotency key обязателен.
Read: `GET /api/v1/managed-configuration-git-write-backs/{proposalRef}` возвращает
exact summary и оба документа для плана; список по
`GET /api/v1/managed-configurations/{configurationRef}/git-write-backs` возвращает
только summaries, cursor и server total. Документы ограничены 256 KiB каждый;
Get повторяет source-read authority, List не выдаёт bytes.

`POST /api/v1/managed-configuration-git-write-backs/{proposalRef}/approve|reject|cancel`
вызывает три специализированных RPC. `If-Match` относится к proposal version,
Approve/Reject также требуют exact `approvalDigest`, Cancel не меняет
подтверждённый план. Все POST требуют CSRF и idempotency key. Три `next_actions`
содержат APPROVE/REJECT/CANCEL ровно по одному; enabled только с причиной NONE.
Unknown enums закрыто отклоняются, UNSPECIFIED failure означает отсутствие
ошибки. UNKNOWN_OUTCOME сохраняет OUTCOME_UNCONFIRMED либо конкретную причину
потери подтверждения; наличие branch receipt не означает наличие PR receipt.

Ошибки CP: InvalidArgument — неприемлемое представление/typed content;
NotFound — отсутствующий или скрытый aggregate; PermissionDenied — недостаточное
отдельное действие на уже доступном объекте; Aborted — stale OCC/source/state;
AlreadyExists — тот же idempotency key с другим intent; Unavailable — сбой owner.
Сначала owner/tenant eligibility, затем OCC и receipt. SQL назначает timestamps,
proposal version/branch/marker; caller не управляет generation, сроками и refspec.

Policy65 сохраняет все операции policy64. Public Prepare profiles используют
resource=configuration_ref, version=mutation.expected_version; решения —
resource=proposal_ref, version=mutation.expected_version; idempotency REQUIRED,
attempt/project FORBIDDEN. Read profiles используют соответствующий ref,
version/attempt/idempotency/project FORBIDDEN. Полный protobuf входит в
UNARY_PROTO_SHA256. Worker profile не использует metadata resource/version/
attempt/idempotency/project hints: tuple находится только в exact protobuf.

Lease — 60 секунд, подготовка не выходит за owner deadline 15 минут после
одобрения, само предложение истекает через 24 часа без решения. Три попытки
разрешены лишь до первого внешнего эффекта. Read-only recovery имеет отдельные
bounded claims и продолжает хранить terminal intent дольше пользовательского
deadline. Claim limit не выше 16, consumer запрашивает одну работу за раз.
Повтор Begin с `already_started=true` разрешает только readback, не resend.
Для branch из первого успешного Begin разрешён ровно один push; для PR —
ровно один provider create. При утрате ответа Begin также требуется readback.

Исполнитель — существующий integration-gateway, не новый deployable. Private
worker snapshot несёт credential descriptor и actual definition package только
через защищённый Proto/gRPC. Все work методы используют exact unary digest,
полный durable lease tuple и workload authority; browser не получает grant.
Worker входит в существующий startup barrier и cancel/join. Readiness рабочего
пути отдельно учитывает свежий owner cycle; локальный kube probe не подменяет
успешную owner обработку.

GitHub/GitLab используют HTTPS Git transport с exact CA/hostname и разрешённым
destination. GitHub требует отдельный `github.com:443` рядом с `api.github.com:443`.
Disposable bare repository, credential file/environment и bounded process
deadline не передают token в URL/argv/log. Commit имеет ровно согласованный base
parent; push ограничен proposal ref и explicit exact lease. Source ref и другие
refs не входят в refspec. Подтверждение lease не разрешает переписывать историю.
PR/MR создаётся отдельным write operation с теми же package/network bounds.

Документация Git `push --force-with-lease=<ref>:<expect>` проверена через Context7
`/git/htmldocs`; GitLab Commit API — через `/websites/gitlab_19_3` и официальную
документацию. `last_commit_id` файла не подменяет branch HEAD fence.

## Обязательные проверки

Targeted PostgreSQL: actor/tenant/approval before OCC, exact replay, stale
configuration/source/connection, expiry/revoke, claim lease, два effects,
lost ACK в каждом окне, terminal tombstone и запрет resend. Consumer fixtures:
GitHub/GitLab exact branch lease, no source-ref mutation, one-parent commit,
single PR/MR marker, ambiguity/timeout read-only recovery, credential redaction,
bounded process cleanup и exact environment render. Contract/codegen/policy65,
Go race/vet/build, Docker и ручной browser scenario фиксируются по фактическому
SHA как PASS/FAIL/NOT RUN; synthetic не заменяет provider/live приёмку.
