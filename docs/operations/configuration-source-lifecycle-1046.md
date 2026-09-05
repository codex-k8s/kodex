---
id: OPS-DOC-1046-CONFIGURATION-SOURCE
title: Источники исполняемых RoleImage и IntegrationDefinition
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Границы источника конфигурации

Источник требований: Issue #1046, Epic #1018, исходный CFG и MVP-UI-42/47.
Матрица задаёт реализацию оставшейся части unit; наличие документа не означает
готовность runtime. До owner/consumer/deploy проверок этот путь имеет статус NOT RUN.

RoleImage и IntegrationDefinition используют отдельные специализированные
команды настройки и обновления Git source. Существующий UI draft сначала
разрешается внутри owner boundary. Source не назначает organization, project,
root actor, managed revision, credential locator или номер поколения из payload.
Запрос выбирает доступную integration connection, repository, ref и path.
Owner проверяет собственное управление агрегатом и connection, разрешённый
repository scope и доступный read operation. URL с credentials недопустим.

Источник сохраняет точный root actor, connection/version, credential revision,
repository/ref/path и собственное монотонное поколение. Периодический refresh
повторно проверяет действующие полномочия root actor и connection. Секрет
остаётся в credential store, CP хранит только утверждённый private descriptor.

| Переход | Authority, OCC и idempotency | Устойчивый результат и потребитель |
| --- | --- | --- |
| Configure UI → GIT/QUEUED | Специализированная команда, exact set version и connection version; receipt внутри owner TX | Source generation, immutable work snapshot, audit и один PLATFORM_CHANGED; PWA читает safe source state |
| Повтор Configure | Сначала повторная owner eligibility, затем тот же intent receipt | Никакого второго source/work |
| Refresh READY/BLOCKED → QUEUED | Owner command либо CP timer; актуальный root actor/credential и source generation | Новая attempt и grant, audit и PLATFORM_CHANGED; integration-gateway получает только owner-assigned work |
| Claim QUEUED → CLAIMED | Exact integration-gateway workload; bounded lease и монотонное claim generation | Immutable snapshot + fence; private descriptor не попадает в публичный DTO |
| Renew CLAIMED | Полный work/source/attempt/claimant/generation/fence; lease ещё действует | Продление в пределах общего work expiry; read path — Claim/renew receipt |
| Complete same commit | Exact claim и текущие source/credential/actor; digest совпадает с принятым | READY без новой published revision/build; завершённый receipt, audit и PLATFORM_CHANGED |
| Complete forward commit | Worker читает exact commit/path и подтверждает fast-forward от server-pinned predecessor; CP проверяет bytes/digest и typed validator | Новая immutable revision; RoleImage owner атомарно создаёт recipe generation/build, IntegrationDefinition — executable package projection; receipt/audit/PLATFORM_CHANGED и профильный эффект в той же TX |
| Invalid document | Exact claim; diagnostic не содержит value/credential | INVALID candidate и SYNC_BLOCKED, прежняя published revision остаётся; receipt/audit/PLATFORM_CHANGED |
| Credential/root permission revoked | Повторная owner eligibility перед claim и complete | SYNC_BLOCKED, прежние grants/claims закрыты; published revision сохраняется; audit/PLATFORM_CHANGED |
| Lease expiry/crash | Reclaim закрывает старый claim, назначает новую attempt/generation/fence | Старый complete отклонён; повторный provider read безопасен, прежний effect receipt не повторяется |
| Provider failure | Закрытый error registry, bounded retry budget | Retry либо SYNC_BLOCKED; partial effect не маскируется успехом; authoritative source read |
| Ref/path reconfiguration | Явная специализированная owner команда с OCC | Новое source generation, прежняя work lineage отозвана атомарно; отдельный baseline ancestry нового source |
| DETACH | Existing specialized detach, owner/set OCC | Source/work/grants terminal в одной TX, новая UI draft с parent revision, published не заменяется; audit/PLATFORM_CHANGED |
| COPY | Existing specialized copy, owner/set OCC | Новый UI set/draft без активного Git source; исходная синхронизация не меняется; audit/PLATFORM_CHANGED |

Неизвестный commit, force-push/divergence, одинаковый commit с другим digest,
несовпадение path, content SHA, source generation или credential pins закрыто
отклоняются. Provider response не является произвольным разрешением читать другой
repository/path: запрос целиком следует неизменяемому owner snapshot.

## Исполнение и доставка

Публичные RPC: `ConfigureRoleImageGitSource`,
`ConfigureIntegrationDefinitionGitSource`, `RefreshRoleImageGitSource`,
`RefreshIntegrationDefinitionGitSource`. Их `MutationContext` относится к
configuration set; source fields не подменяют authority. Ответ содержит set и
safe `git_source`, также доступный через существующий managed read path.

Worker RPC принадлежат `ManagedConfigurationSourceWorkService`:
`ClaimManagedConfigurationSourceWork`, `RenewManagedConfigurationSourceWork`,
`CompleteManagedConfigurationSourceWork`, `FailManagedConfigurationSourceWork`.
Operation IDs: `platform.configuration-sources.work.claim|renew|complete|fail`.
Все используют exact `UNARY_PROTO_SHA256`, caller `integration-gateway`, без
resource/version/attempt/idempotency/project metadata hints. Полная work lease
передаётся в protobuf и проверяется owner по durable состоянию.

Claim ограничен 16 работами; consumer запрашивает столько, сколько успевает
обработать в lease. Lease — 60 секунд, renewal не выходит за общий deadline
15 минут. Максимальный документ — 256 KiB, UTF-8, точный configured format.
Complete всегда передаёт content, в том числе для UNCHANGED: CP повторяет
digest/typed validation и не доверяет одному признаку совпадения commit.
INITIAL допустим лишь без predecessor, UNCHANGED — при совпадении commit,
FAST_FORWARD — при отличающемся commit и подтверждённой provider ancestry.

Worker failure — закрытый enum `UNAVAILABLE`, `CREDENTIAL_REJECTED`,
`ACCESS_DENIED`, `NOT_FOUND`, `DIVERGED`, `CONTENT_INVALID`, `RESPONSE_INVALID`.
UNSPECIFIED не принимается. Повтор exact completed tuple возвращает receipt,
другой payload после complete — конфликт. Ошибки CP: InvalidArgument для
невалидного запроса, NotFound для скрытого owner resource, Aborted для stale
version/state/fence, AlreadyExists для повторного ключа с другим intent,
Unavailable для временного сбоя владельца. Транспортная ошибка не выдаётся за
provider outcome.

Existing integration-gateway worker получает отдельный configuration source
work kind через CP Proto/gRPC и existing protected operation profile. Runtime
Run/Turn/integration grant для такого чтения не создаётся. Новый listener,
deployable и прямое чтение чужой БД не требуются. Startup barrier, cancel/join,
bounded retry и readiness принадлежат существующему consumer.

RoleImage managed revision связывается с реальной recipe generation/build.
Direct UI recipe create/update также создаёт server-owned managed lineage;
Git-owned recipe изменяется только через source sync либо detach/copy. Build,
scan, promotion и RuntimeEnvironment используют immutable pins. Новый runtime
v7 требует role runtime contract revision 2; старый образ не подставляется при
недоступности новой сборки.

IntegrationDefinition использует типизированный canonical package и точные
adapter/operation/schema pins. UI/GIT origin не расширяет registry поддерживаемых
adapter effects и не выдаёт дополнительных permissions. Consumers читают
нормализованную published projection с exact version/digest.

Private `IntegrationConnectionTestClaim.definition_package`,
`IntegrationInvocationClaim.definition_package` и
`ManagedConfigurationSourceWork.definition_package` передают canonical package
в пределах существующего лимита 256 KiB. Consumer строго разбирает документ,
сверяет key/version/digest и поддерживаемые adapter/operations. Несовпадение либо
отсутствие обязательного package закрыто отклоняется; fallback на shipped digest
не подменяет выбранную UI/GIT revision. Public DTO не содержит эти bytes.

Полная проверка включает creator → HTTP → specialized CP command → durable
work → protected integration-gateway claim → provider exact commit read → CP
atomic accept/effect → published readback, а также все строки матрицы, consumer
readiness и оба итоговых environment renders. Live Git provider smoke отдельно
остаётся NOT RUN без безопасной среды.
