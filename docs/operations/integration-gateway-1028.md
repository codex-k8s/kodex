---
id: OPS-INT-1028
title: Сквозная проверка integration-gateway #1028
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-04
---

# Integration gateway #1028

Источники: #1018, #1028, `GUIDE-DOC-003`, `GUIDE-DOC-006`,
`contracts/integrations/v1`. Владельцем состояния является control-plane.
Финальные проверки относятся к локальному SHA в отчёте исполнителя, не к CI.

## Передача root: EMAIL consumer, 2026-09-05

Код и тесты: `7c517b77aa73256a4dc8148798c3d09e37b01b52`.
Зависимости потреблены без merge: rebase на CP `88f4331ff` (уже содержит
main #1057 и исходный typed EMAIL consumer), cherry-pick EMAIL `07cd3ee69`
как `370967468` и `a67f200623` как `712618521`. Повторный catalog commit
`c5d02f280` автоматически исключён rebase как уже применённый через #1057.
Vendor contracts/adapters, CP Proto/generated и EMAIL API после потребления
зависимостей не изменены. Handwritten HTTP/PWA не менялись.

| Acceptance | Локальный результат |
| --- | --- |
| Все advertised операции, включая 21 EMAIL | PASS: `timeout 180s go test -race ./... -count=1 -timeout=120s` в gateway на code SHA; `TestEveryAdvertisedOperation` использует реальный HTTP fixture и generated command, проверяет exact endpoint/header/body |
| Unknown response без повторного эффекта | PASS: все 12 EMAIL mutations × unknown/malformed/oversize/503/403/failed; ровно один HTTP call, UNKNOWN не превращается в success; отдельный claimed status read сохранён |
| Authority/lease/fence/TLS | PASS: claim mapping, запрет mutation через test binding, expiry/cancel HTTP, exact IMAP/thread readback, completion с исходным fence; actual mTLS, wrong CA/SNI/client и redirect negatives |
| Остальные vendors | PASS: полный gateway race-suite, включая catalog/pagination/rate-limit/scope/UNKNOWN; vendor implementations не изменялись |
| Статика/сборка | PASS: `timeout 120s go vet ./...`, `timeout 120s go build ./...`, `git diff --check` |
| Generated clients/catalog | PASS: `timeout 120s make check-email-bridge-codegen check-integration-package-codegen test-integration-gateway-render`; EMAIL source и generated совпадают |
| Оба deploy profiles | PASS: web-only/web-with-mattermost, exact gateway → EMAIL 8443, Service443 → workload8443; внешние mail ports не добавлялись |
| SQL authority/lifecycle | PASS: targeted disposable PostgreSQL `(integration|email)`, migration up/status/up до 20260904000620; gate/cardinality, workload/replay, immutable projection, Report/reconciliation и watermark |
| Runtime Docker | PASS: `timeout 300s docker build --target runtime --build-arg VERSION=7c517b77a -f services/external/integration-gateway/Dockerfile -t kodex-integration-gateway:1028-7c517b77a .` |

Локальный OCI manifest list:
`sha256:46f48c50c87165d49bf2723d1a1a37c0de61850cd302df91f39912d366e7a25e`.
SQL/codegen/render выполнялись на том же содержимом соответствующих файлов
до commit; полный gateway race и Docker выполнены после commit на code SHA.
Последующий документальный commit не меняет проверенный runtime/test tree.

Первый новый TLS-тест: FAIL из-за fixture mode0600; исправлен только fixture
на read-only0400, затем targeted TLS и полный gateway race PASS. Production
securefile boundary не ослаблялась. Незакрытых локальных FAIL нет.

NOT RUN: live SMTP/IMAP/POP, staging/production, общий browser UI, общий
репозиторный baseline и отдельные review. Их не подменяют fake HTTP или
disposable PG. Push/PR/merge/deploy не выполнялись. Listener8082/config остаются
у #1029/root; отсутствие разрешённого live mail не выдаётся за готовность
производственного почтового контура. Вопрос POP UID ниже передан через этот
файл, поскольку инструмент прямой межагентной отправки недоступен.

## Карта сценариев

| Сценарий | Инициатор и полномочия | Контракт и владелец | Результат и потребитель |
| --- | --- | --- | --- |
| Connection/configuration/grant | Проверенный browser actor и `integration.manage` | control-api → специализированные control-plane команды; OCC/idempotency | PostgreSQL configuration, grant, audit; UI authoritative read |
| UI definition | Управляющий actor; JSON/YAML `IntegrationPackage` | create → validate → publish → impact/rebind; exact registered digest и READY | Versioned managed binding; не разрешает raw provider операции |
| MCP read | Runtime execution и последняя immutable revision, active grant, root actor | `ResolveIntegrationInvocation` → `ClaimIntegrationInvocations` | Typed adapter GET → complete/receipt → `GetIntegrationInvocation` → MCP |
| MCP mutation | Те же ограничения плюс Human Gate для exact effect/input/scope | WAITING_APPROVAL → APPROVED → READY → RUNNING | Один provider call, receipt и событие TURN_PROGRESS в owner transaction |
| Rejection/cancel gate | Проверенный owner gate actor и OCC | REJECTED/CANCELLED без claim | Авторитетный invocation read; внешний эффект отсутствует |
| Отзыв grant/connection | Текущий enabled/state и exact scope на claim | Claim скрывает отозванный grant до выдачи credential revision | Нет provider-вызова |
| Неизвестный outcome | Точный lease/fence/generation | Completion UNKNOWN_OUTCOME или expiry RUNNING mutation | Durable read; новый claim запрещён; MCP сообщает необходимость решения владельца |
| Read lease expiry | Точная просроченная read lease | READY с новым fence при следующем claim | Повтор только чтения |
| Mattermost | Catalog metadata | Owner interaction-gateway #1030 | Generic MCP/credential route отклоняет |
| Email | Exact CP claim, invocation либо connection test, lease/ref/fence/generation; mailbox/sender из public configuration claim | Generated `emailbridgeapi`, POST `/v1/mailbox-operations`, mTLS и `X-Kodex-Email-Execution`; CP повторно разрешает authority | Все 21 операции; typed result, bounded receipt; SMTP/IMAP/POP принадлежат #1037 |

## EMAIL lifecycle #1037 → #1028

| Переход | Gateway boundary | Авторитетный результат / read path |
| --- | --- | --- |
| Connection test | Только test binding из CP claim и HEALTH, без fabricated invocation | CP complete test с исходным fence; readiness не обещает live mail |
| Claim read | Exact definition/input/scope digest; binding только из claim | EMAIL повторно проверяет CP authorization и mailbox policy |
| Claim mutation | Тот же binding; effect key из claim, не input | EMAIL durable UNKNOWN и CP Report до provider effect |
| Lease expiry / cancel | Контекст HTTP ограничен lease и caller; истёкший binding не отправляется | CP закрыто отклоняет stale fence; gateway не продлевает authority самостоятельно |
| Complete | Typed bounded response; исходные lease/ref/fence/generation | CP complete сохраняет результат и receipt; authoritative invocation read |
| Unknown / truncated / timeout | Ни повторного POST, ни скрытого status lookup | CP UNKNOWN_OUTCOME; EMAIL immutable receipt и report journal |
| Receipt read | Отдельный claimed RECEIPT с ровно одним receipt ID либо effect key | Статус unknown остаётся unknown; чтение не разрешает новый эффект |
| Reconciliation | Gateway не вызывает owner decision от имени пользователя | CP owner decision → EMAIL bounded reconciler; исходный UNKNOWN неизменен |

Зависимости: CP `88f4331ff`, EMAIL `07cd3ee69` и metadata `a67f200623`.
Каталог остальных vendors сохранён из #1057 без расширения операций.

Контрактное замечание для root #1037: POP fetch/download/attachment list пока
возвращают parsed payload без `uid`. Текущий consumer допускает отсутствие UID
только при отсутствии запрошенного UIDVALIDITY; присутствующий UID сверяет,
для IMAP требует exact UID/UIDVALIDITY/folder. Это не новый authority источник:
CP authorization и серверный source binding остаются обязательными.
Новых CP authority fields для 21 операций не требуется. Listener 8082 и live
mail принадлежат #1029/root и здесь не меняются. Внутренний egress gateway →
EMAIL открыт только по exact namespace/pod selector на workload port 8443.

Context7 resolve для стандартного `net/http` вернул нерелевантные библиотеки;
проверена [официальная документация Transport](https://pkg.go.dev/net/http#Transport).
POST не имеет `GetBody` или idempotency HTTP header; effect key находится в
typed command, поэтому не включает автоматический replay транспорта.

Неизвестный исход не подтверждает отсутствие эффекта. Нельзя менять ему state
через SQL, повторять POST или выпускать новый effect key автоматически.
Владелец сверяет provider history и receipt, останавливает исходный run либо
отдельно разрешает новое намерение после сверки. Отсутствие результата GET
само по себе не разрешает повтор. Старый UNKNOWN_OUTCOME остаётся историей
неподтверждённого эффекта, а не поддельной успешной receipt.

## Критерии и доказательства

| Критерий #1028 | Конкретная проверка |
| --- | --- |
| Каждая advertised operation исполняется | `TestEveryAdvertisedOperation`: перебор shipped registry, отдельный input каждого operation, реальный HTTP fixture, typed output и receipt |
| Pagination | GitHub Link, GitLab page, Jira nextPageToken, Confluence cursor в той же матрице |
| Rate limits | `TestReadOperationsHandleRateLimits`; bounded повтор 429; Retry-After длиннее бюджета не сокращается |
| Неизвестная mutation не повторяется | `TestEveryMutationPreservesUnknownOutcome`; `testIntegrationEffectLifecycle` проверяет explicit unknown, lease expiry и replacement worker |
| Scope до credential | `TestScopeDeniedBeforeCredentialRead`; resource digest проверяется с недоступным credential store |
| Confluence space | `TestConfluenceForeignSpaceCannotMutate` для update, parent create и upload |
| Human Gate/receipt/revocation | PostgreSQL `testIntegrationEffectLifecycle`: READ, REJECT, APPROVE, exact duplicate, digest mismatch, revoked queued grant |
| UI registry | `TestValidateTypedIntegrationRegistry`; один package parser, exact зарегистрированный ready digest |
| Email fail closed | `TestEmailNotReadyCannotSend`, `TestEmailEveryMutationHTTPFailureIsNotRetried`; generated `emailbridgeapi`, все 21 typed POST, без legacy sender query |
| Email authority и TLS | `TestEmailExecutionClaimMapping`, `TestEmailTestBindingCannotAuthorizeMutation`, `TestEmailClaimExpiryBoundsHTTP`, `TestEmailExactTLSAndRedirectBoundary` |
| Email readback и completion | `TestEmailReadbackExactIdentity`, `TestEmailCompletionPreservesFenceAndUnknown`; exact UID/UIDVALIDITY/folder/thread и исходный CP fence |
| Synthetic | `make test-integration-synthetic`: race, HTTP CRUD/OCC, replay и local-only render |
| Contracts | `make check-integration-package-codegen check-email-bridge-codegen check-proto-codegen`; OpenAPI re-generation comparison |
| Migration и targeted component | `timeout 240s bash scripts/tests/control-plane-postgres-test.sh '^TestBootstrapComponent$/(integration|email)'`: fresh PostgreSQL, up/status/up до 20260904000620, integration и EMAIL subtests |
| Targeted render | `make test-integration-gateway-render`: оба profiles, exact egress, probes, ServiceAccount, lease budget и unknown alert |

## Эксплуатация

Gateway не хранит domain state и не получает DB/RBAC права на изменение
invocations. Secret projection и mTLS/application proof остаются в существующем
deploy ownership. Egress разрешён только через exact platform egress gateway,
внутренние control-plane/telemetry endpoints и local Synthetic fixture.
`/readyz` проверяет local authority; доступность provider проверяется рабочей
typed health operation, без упрощённого обхода auth.

Alert `IntegrationGatewayUnknownOutcome` использует исполняемую метрику
`kodex_integration_gateway_operations_total{outcome="unknown"}`.
Сервисный dashboard показывает те же фактические series.

Full baseline, review итогового интегрированного SHA, staging/production и
реальная доставка почты выполняются отдельно. Они не обозначаются как PASS
по локальным fixture-тестам. Откат application не откатывает PostgreSQL schema;
неизвестные outcomes нельзя переводить в READY старым бинарём.

Первый локальный `make test-web-only-release` завершился `FAIL` на проверке
`provider credential publisher delivery targets are incomplete`. После
интеграции актуального `main` и #1048 полный release suite повторно выполнен
на содержимом `5fe379f24685a03245496dbe3129638d47c42216`: `PASS`.
Также повторно прошли targeted PostgreSQL, оба integration render-профиля и
package/email/proto codegen. Это локальные проверки, не CI и не staging.
Deployed browser replay fixture прежнего профиля не запускался.

После rebase на runtime #1047 общий PostgreSQL suite остановился на
`session_provider_affinity_survives_policy_mutation_and_fails_closed_on_revoke`:
ожидание workspace policy не соответствует новой модели. Это `FAIL` общего
suite; targeted integration subtests запускаются отдельной точкой входа выше.

## Проверенные API

GitHub REST проверен через Context7 `/websites/github_en_rest`:
issues/comments, Link pagination, 403/429 и Retry-After.
Официальные источники остальных adapter profiles:
[GitLab issues](https://docs.gitlab.com/api/issues/),
[Jira search](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-search/),
[Confluence pages](https://developer.atlassian.com/cloud/confluence/rest/v2/api-group-page/),
[Confluence multipart attachments](https://developer.atlassian.com/cloud/confluence/rest/v1/api-group-content---attachments/).
