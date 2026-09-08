---
id: OPS-DOC-1242
title: Интеграции MVP-UI-39–42, локальные доказательства и живая приёмка
type: acceptance-proof
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-08
---

# Интеграции MVP-UI-39–42

Задача [#1242](https://github.com/codex-k8s/kodex/issues/1242), эпик #1018,
приёмка [MVP-1031](mvp-1031-acceptance.md). Исходная ревизия
`6061aedc558330d65215e7512df6fd457cfb9e78`; точный итоговый SHA и результаты
публикуются в связанном PR. Проверенная рабочая версия содержит описанное ниже
исправление PWA; backend, контракты, pinned packages и deploy не менялись.
Документ фиксирует реализованные маршруты и локальные доказательства,
а не факт живой приёмки.

## Исправление и пользовательская матрица

При повторном выборе проекта или получателя тот же ref мог получить новую
revision, а старые downstream candidates оставались до изменения controlled
props. Теперь выбор синхронно очищает зависимые selections, блокирует submit
и инвалидирует поздние страницы. Ключи получателя/возможности включают версию
connection и pins родительского candidate, поэтому старый dropdown не сохраняет
страницы прежнего снимка. API команды остаётся авторитетным.

| Требование | Реализованный путь | Локальное доказательство | Живая приёмка |
| --- | --- | --- | --- |
| MVP-UI-39 | Общий AsyncEntityPicker, server search/cursor, connection projection с provider/credential/readiness/scope | IntegrationConnectionsPanel, grant-candidates и профильные PWA tests | NOT RUN: браузер, длинные badges, keyboard/infinite scroll, смена Project |
| MVP-UI-40 | connections → projects → recipients → capabilities; pins/version; admission повторно проверяет authority | IntegrationGrantsPanel.clear: очистка, поздняя страница, same-ref новая revision; candidate/API tests | NOT RUN: реальный actor, delegation ceiling, параллельный revoke |
| MVP-UI-41 | Mailbox form/YAML preview, credential references, publish/bind, SMTP/IMAP/POP3, per-operation policy и receipt | PWA mailbox/editor/credential tests; email-bridge protocol components; gateway typed mapping | NOT RUN: настоящие серверы и полный CP/egress/secret-publisher маршрут |
| MVP-UI-42 | Семь pinned packages, details modal, exact scopes, typed schema и executor ownership | Полный operation catalog в gateway и Mattermost; IntegrationCatalogPanel и package isolation tests | NOT RUN: UI/MCP → deployed provider → durable owner receipt |

## Полная карта authority и эффекта

| Шаг | Инициатор, endpoint и mapping | Авторитетное состояние, проверка и результат |
| --- | --- | --- |
| Поиск кандидатов | Проверенный UI actor; GET /api/v1/integration-grant-candidates/{connections,projects,recipients,capabilities}; generated SDK → control-api-gateway → CP ListIntegrationGrant*Candidates | CP integration_grant_candidate_queries.go и общий admission: actor, organization, connection, Project, recipient kind/ref, capability; context digest, immutable definition и versions; bounded cursor. Projection не выдаёт полномочия |
| Выдача/отзыв | POST /api/v1/integration-connections/{connectionRef}/grants → ChangeIntegrationGrant | CP configuration.go: connection lock, authorizeIntegrationGrant до OCC, точный recipient owner, definition/resource scope; receipt/audit и INTEGRATION_GRANT_CHANGED в owner transaction; revoke не отменяет уже принятый провайдером эффект |
| MCP | RuntimeRevision grants → закрытый tools catalog → ResolveIntegrationInvocation | CP workers.go/runtime.go и SQL: действующий run/node, initiator authority, connection/grant, package version/digest, scope digest, последний runtime generation; immutable input. Idempotency scope связывает node/key/connection/capability/input/package/scope; для Email добавлен mailbox source digest |
| Human Gate | ResolveIntegrationInvocation → owner gate graph | Package HUMAN_EACH_EFFECT либо усиленная mailbox policy дают WAITING_APPROVAL; OWNER_GATE_OPENED атомарен с gate/node/edge и WAITING_HUMAN; разрешение относится к конкретному effect/input |
| Claim | Generated RuntimeWorkService.ClaimIntegrationInvocations | CP фильтрует workload/route, grant enabled, connection CONNECTED, pinned package/scope и runtime generation; выдаёт lease/ref/fence/generation/expiresAt. Gateway не назначает себе actor или scope |
| Provider | integration-gateway для GitHub/GitLab/Jira/Confluence/Synthetic/Email; interaction-gateway для Mattermost | Adapter повторно сверяет schema, risk/Gate, config/scope/input digest до credential. Exact HTTPS origin через egress; redirect запрещён. Email использует mTLS POST /v1/mailbox-operations и повторный ResolveEmailAuthorization у CP |
| Completion | CompleteIntegrationInvocation с exact lease и effect receipt | CP проверяет fence/generation/expiry, effect/input/response digests; immutable receipt и TURN_PROGRESS фиксируются в owner transaction. Результат читается через GetIntegrationInvocation; audit принадлежит CP |
| Email effect | ReserveEffect → ReportEmailEffect → SMTP/IMAP/POP3 → CompleteEffect → ReportEmailEffect | Bridge сохраняет UNKNOWN до provider, exact owner binding и report journal; CP владеет решением reconciliation, bridge durable consumer подтверждает receipt. Повтор не отправляет письмо заново; потеря report восстанавливается из PostgreSQL journal |

Ссылки на реализацию: `services/internal/control-plane/internal/repository/postgres/platform/{configuration,workers,runtime,integration_grant_admission}.go`,
`services/external/integration-gateway/internal/integration/`,
`services/external/interaction-gateway/internal/mattermost/`,
`services/internal/email-bridge/internal/domain/service/mail/service.go`.
События доставляет существующая общая цепочка CP outbox → broker → inbox/cursor
потребителей; в #1242 новые события и consumers не вводятся. Email report journal
является отдельным подтверждаемым каналом owner receipt, а не прямой записью
в чужую БД. Live outbox/inbox/realtime delivery здесь NOT RUN.

## Lifecycle и отказы

| Вид и переход | Обязательный итог | Локальное доказательство и предел |
| --- | --- | --- |
| Grant create/update/revoke | Exact actor/recipient/resource; OCC/idempotency после authority; pinned revisions не расширяются автоматически | CP admission/runtime unit; PostgreSQL admission приёмка NOT RUN |
| Connection test claim/complete/expiry | Тот же credential и health operation, exact lease; истёкший test возвращается DUE | Gateway app/completion, Mattermost connection component; SQL recovery NOT RUN |
| Invocation create/approval/claim | READY либо WAITING_APPROVAL; package minimum Gate нельзя ослабить; чужой workload не claim-ит operation | CatalogPinnedAuthorityBeforeCredentials; Mattermost changed-contract/system subscription negatives |
| Read retry/lease expiry | Bounded retry; истёкший READ возвращается READY; новые grant/package/runtime pins перепроверяются | ReadOperationsHandleRateLimits, pagination/limits tests; durable SQL expiry NOT RUN |
| Mutation success/duplicate | Один внешний mutation attempt, exact receipt; повтор отдаёт прежний effect | EveryAdvertisedOperation, EveryCallableMattermostOperationHasTypedExecution, TypedIMAPCatalogHTTPS |
| Mutation timeout/disconnect/5xx/malformed success | UNKNOWN_OUTCOME, не READY и не автоматический повтор | EveryMutationPreservesUnknownOutcome, CatalogMutationProviderFailuresNeverReplay, EmailEveryMutationHTTPFailureIsNotRetried |
| Cancel/delete/revoke | Новые claims не выдаются; уже принятый внешний эффект не объявляется отменённым | Claim SQL checks и email/Mattermost authority negatives; реальный concurrent cancel/revoke NOT RUN |
| Email restart/неполное completion | Устойчивый UNKNOWN/report journal, exact replay либо owner reconciliation; новый send без решения запрещён | Reserve-before-provider и report/reconciliation unit; PostgreSQL restart/recovery tests NOT RUN |
| Email owner reconciliation | Свежая интерактивная authority, exact receipt/version; только UNKNOWN → EFFECT_CONFIRMED или NO_EFFECT_CONFIRMED | emailpolicy unit и reconciliation service unit; durable report consumer NOT RUN |

## Протоколы и scope всех семи пакетов

| Пакет | Реальные методы/протокол и exact scope | Ограничения и readiness |
| --- | --- | --- |
| GitHub | REST /repos/{owner}/{repo}: contents, branches/git refs, commits, issues/comments, pulls/reviews/files, check-runs, actions/workflows/runs/jobs | owner/repository; content update/delete SHA, merge/review commit pin; metadata health тем же credential. 64 KiB включая JSON/base64; Actions logs/artifacts/CDN не объявлены |
| GitLab | REST /api/v4/projects/{escaped project_path}: repository/files/tree/branches/commits, issues/notes, merge_requests/discussions/diffs, pipelines/jobs | exact HTTPS origin/project; native cursor/page; bounded trace; metadata health. retry/cancel отдельные типизированные команды, не общий proxy |
| Jira | REST /rest/api/3: project, user/assignable/search, search/jql, issue/transitions/comment/issueLink/attachment | exact origin/project_key; assignable users только проекта, JQL project condition не снимается; child/link/attachment проверяется через issue; project health |
| Confluence | REST /wiki/api/v2: spaces/pages/descendants/footer-comments/attachments; upload через /wiki/rest/api/content/{page}/child/attachment | exact origin/space_id; OCC page/comment version; parent page/comment проверяется до write/download; health читает exact space; inline comments и cross-origin CDN не объявлены |
| Email | Gateway mTLS HTTPS → email-bridge → SMTP submission, IMAP UID operations, optional POP3 compatibility через egress CONNECT | exact mailbox/sender/recipients/folders и credential generation; отдельные SMTP/IMAP/POP3 статусы; проверка CA/hostname/SNI и TLS/STARTTLS без downgrade |
| Mattermost | /api/v4: team/channel resolution, posts/threads/search/files/reactions и exact send/update; системные inbound/gate subscriptions отдельно | exact base_url/team_name/channel_name; post/file/root revalidation, собственный author при update, gate post защищён; health тем же credential/team/channel. Не исполняется generic integration-gateway |
| Synthetic HTTP | Только закреплённый local integration-synthetic и journal.read/write; typed schemas/effect key | Не внешний vendor и не доказательство vendor acceptance. Нет arbitrary URL/method/path; journal.read health. В staging/production provider не подставляется автоматически |

SMTP поддерживает implicit TLS и STARTTLS, password/OAUTHBEARER, bounded MIME,
from/envelope-from/reply-to, To/Cc/Bcc, вложения и send/reply/reply_all/forward.
Подтверждение DATA означает приём SMTP-сервером, а не доставку адресату.
IMAP использует UID/UIDVALIDITY, BODY.PEEK, native search, exact allowed folders;
thread связывает Message-ID/References/In-Reply-To. Flags/move/archive/delete
и draft create/update/delete имеют receipt; UID EXPUNGE не удаляет чужие
помеченные сообщения. COPY fallback подтверждается перед удалением источника.

POP3 имеет один maildrop, отображённый compatibility-слоем как INBOX. List/read/
attachments работают через UIDL/LIST/TOP/RETR; `message.search` является
ограниченной локальной фильтрацией заголовков, а не server-side search.
Курсор закреплён за UIDL snapshot и filter. POP3 не предоставляет IMAP folders,
threads, flags, move/archive и drafts. DELE подтверждается QUIT. SMTP-send
остаётся отдельным протоколом. Это существующая граница
[OPS-EMAIL-1037](email-bridge-1037.md), не новый provider API.

Mail policy ALLOW означает отсутствие дополнительного mailbox Gate;
минимальная package policy сохраняется. HUMAN_GATE может усилить чтение или
отправку, DENY запрещает operation. Все значения secret остаются отдельно от
формы/YAML; public representation содержит только secret references.

## Полный реестр операций

Следующие таблицы извлечены из текущих shipped YAML: всего 157 capabilities,
139 принадлежат integration-gateway, 18 Mattermost. Из Mattermost inbound и
gate_decisions являются подписками, остальные 16 callable; подписка не может
быть вызвана агентом как команда. Ссылка каждой operation ведёт к её typed
input/output schema, exact resource kind, ограничениям и execution policy.
Обозначение времени/попыток отражает максимум package. Gateway дополнительно
ограничивает operation общим lease budget; внешний HTTP mutation выполняется
один раз даже при большем declared maxAttempts. Email HTTP не повторяется
автоматически. Synthetic имеет собственную durable дедупликацию effect key.

### GitHub (github 2.3.0)

Connection fields: `owner`, `repository`. Health: `github.repository.metadata.read`.

| Operation / typed schema | Risk / minimum Gate | Idempotency | Timeout, s / attempts |
| --- | --- | --- | --- |
| [github.repository.metadata.read](../../contracts/integrations/v1/definitions/github.yaml#L41) | READ / NONE | READ_ONLY | 20 / 3 |
| [github.issue.create](../../contracts/integrations/v1/definitions/github.yaml#L69) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 2 |
| [github.issue.list](../../contracts/integrations/v1/definitions/github.yaml#L90) | READ / NONE | READ_ONLY | 20 / 3 |
| [github.issue.read](../../contracts/integrations/v1/definitions/github.yaml#L112) | READ / NONE | READ_ONLY | 20 / 3 |
| [github.issue.comment.create](../../contracts/integrations/v1/definitions/github.yaml#L133) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.issue.update](../../contracts/integrations/v1/definitions/github.yaml#L153) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.repository.content.list](../../contracts/integrations/v1/definitions/github.yaml#L176) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.repository.content.read](../../contracts/integrations/v1/definitions/github.yaml#L197) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.repository.content.create](../../contracts/integrations/v1/definitions/github.yaml#L220) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.repository.content.update](../../contracts/integrations/v1/definitions/github.yaml#L242) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.repository.content.delete](../../contracts/integrations/v1/definitions/github.yaml#L265) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.branch.list](../../contracts/integrations/v1/definitions/github.yaml#L287) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.branch.read](../../contracts/integrations/v1/definitions/github.yaml#L308) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.branch.create](../../contracts/integrations/v1/definitions/github.yaml#L328) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.branch.delete](../../contracts/integrations/v1/definitions/github.yaml#L349) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.commit.list](../../contracts/integrations/v1/definitions/github.yaml#L367) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.commit.read](../../contracts/integrations/v1/definitions/github.yaml#L390) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.pull_request.list](../../contracts/integrations/v1/definitions/github.yaml#L415) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.pull_request.read](../../contracts/integrations/v1/definitions/github.yaml#L439) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.pull_request.create](../../contracts/integrations/v1/definitions/github.yaml#L465) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.pull_request.update](../../contracts/integrations/v1/definitions/github.yaml#L494) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.pull_request.merge](../../contracts/integrations/v1/definitions/github.yaml#L524) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.pull_request.review.list](../../contracts/integrations/v1/definitions/github.yaml#L546) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.pull_request.review.read](../../contracts/integrations/v1/definitions/github.yaml#L568) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.pull_request.review.create](../../contracts/integrations/v1/definitions/github.yaml#L590) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.issue.comment.list](../../contracts/integrations/v1/definitions/github.yaml#L614) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.issue.comment.read](../../contracts/integrations/v1/definitions/github.yaml#L636) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.issue.comment.update](../../contracts/integrations/v1/definitions/github.yaml#L656) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.issue.comment.delete](../../contracts/integrations/v1/definitions/github.yaml#L677) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.check_run.list](../../contracts/integrations/v1/definitions/github.yaml#L696) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.check_run.read](../../contracts/integrations/v1/definitions/github.yaml#L718) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.actions.workflow.list](../../contracts/integrations/v1/definitions/github.yaml#L742) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.actions.workflow.read](../../contracts/integrations/v1/definitions/github.yaml#L763) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.actions.workflow.dispatch](../../contracts/integrations/v1/definitions/github.yaml#L784) | SENSITIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.actions.run.list](../../contracts/integrations/v1/definitions/github.yaml#L804) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.actions.run.read](../../contracts/integrations/v1/definitions/github.yaml#L827) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.actions.run.rerun](../../contracts/integrations/v1/definitions/github.yaml#L852) | SENSITIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.actions.run.cancel](../../contracts/integrations/v1/definitions/github.yaml#L870) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [github.actions.job.list](../../contracts/integrations/v1/definitions/github.yaml#L888) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.actions.job.read](../../contracts/integrations/v1/definitions/github.yaml#L910) | READ / NONE | READ_ONLY | 30 / 3 |
| [github.pull_request.file.list](../../contracts/integrations/v1/definitions/github.yaml#L934) | READ / NONE | READ_ONLY | 30 / 3 |

### GitLab (gitlab 1.2.0)

Connection fields: `base_url`, `project_path`. Health: `gitlab.project.metadata.read`.

| Operation / typed schema | Risk / minimum Gate | Idempotency | Timeout, s / attempts |
| --- | --- | --- | --- |
| [gitlab.project.metadata.read](../../contracts/integrations/v1/definitions/gitlab.yaml#L26) | READ / NONE | READ_ONLY | 20 / 3 |
| [gitlab.repository.file.read](../../contracts/integrations/v1/definitions/gitlab.yaml#L41) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.issue.read](../../contracts/integrations/v1/definitions/gitlab.yaml#L58) | READ / NONE | READ_ONLY | 20 / 3 |
| [gitlab.issue.list](../../contracts/integrations/v1/definitions/gitlab.yaml#L74) | READ / NONE | READ_ONLY | 20 / 3 |
| [gitlab.issue.create](../../contracts/integrations/v1/definitions/gitlab.yaml#L91) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 2 |
| [gitlab.issue.update](../../contracts/integrations/v1/definitions/gitlab.yaml#L107) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [gitlab.merge_request.read](../../contracts/integrations/v1/definitions/gitlab.yaml#L125) | READ / NONE | READ_ONLY | 20 / 3 |
| [gitlab.merge_request.discussion.create](../../contracts/integrations/v1/definitions/gitlab.yaml#L142) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 2 |
| [gitlab.branch.create](../../contracts/integrations/v1/definitions/gitlab.yaml#L156) | SENSITIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 2 |
| [gitlab.commit.create](../../contracts/integrations/v1/definitions/gitlab.yaml#L170) | SENSITIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 45 / 2 |
| [gitlab.merge_request.create](../../contracts/integrations/v1/definitions/gitlab.yaml#L188) | SENSITIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 2 |
| [gitlab.pipeline.read](../../contracts/integrations/v1/definitions/gitlab.yaml#L206) | READ / NONE | READ_ONLY | 20 / 3 |
| [gitlab.pipeline.retry](../../contracts/integrations/v1/definitions/gitlab.yaml#L222) | SENSITIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [gitlab.branch.list](../../contracts/integrations/v1/definitions/gitlab.yaml#L236) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.branch.read](../../contracts/integrations/v1/definitions/gitlab.yaml#L251) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.branch.delete](../../contracts/integrations/v1/definitions/gitlab.yaml#L266) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [gitlab.commit.list](../../contracts/integrations/v1/definitions/gitlab.yaml#L278) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.commit.read](../../contracts/integrations/v1/definitions/gitlab.yaml#L295) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.commit.diff](../../contracts/integrations/v1/definitions/gitlab.yaml#L311) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.repository.tree.list](../../contracts/integrations/v1/definitions/gitlab.yaml#L327) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.issue.note.list](../../contracts/integrations/v1/definitions/gitlab.yaml#L344) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.issue.note.read](../../contracts/integrations/v1/definitions/gitlab.yaml#L360) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.issue.note.create](../../contracts/integrations/v1/definitions/gitlab.yaml#L377) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [gitlab.issue.note.update](../../contracts/integrations/v1/definitions/gitlab.yaml#L394) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [gitlab.issue.note.delete](../../contracts/integrations/v1/definitions/gitlab.yaml#L412) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [gitlab.merge_request.list](../../contracts/integrations/v1/definitions/gitlab.yaml#L425) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.merge_request.update](../../contracts/integrations/v1/definitions/gitlab.yaml#L441) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [gitlab.merge_request.merge](../../contracts/integrations/v1/definitions/gitlab.yaml#L463) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [gitlab.merge_request.discussion.list](../../contracts/integrations/v1/definitions/gitlab.yaml#L483) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.pipeline.list](../../contracts/integrations/v1/definitions/gitlab.yaml#L499) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.pipeline.cancel](../../contracts/integrations/v1/definitions/gitlab.yaml#L516) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [gitlab.job.list](../../contracts/integrations/v1/definitions/gitlab.yaml#L532) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.job.read](../../contracts/integrations/v1/definitions/gitlab.yaml#L548) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.job.retry](../../contracts/integrations/v1/definitions/gitlab.yaml#L566) | SENSITIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [gitlab.job.cancel](../../contracts/integrations/v1/definitions/gitlab.yaml#L584) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [gitlab.job.trace.read](../../contracts/integrations/v1/definitions/gitlab.yaml#L602) | READ / NONE | READ_ONLY | 30 / 3 |
| [gitlab.merge_request.diff.list](../../contracts/integrations/v1/definitions/gitlab.yaml#L615) | READ / NONE | READ_ONLY | 30 / 3 |

### Jira (jira 1.2.0)

Connection fields: `base_url`, `auth_scheme`, `username` (optional), `project_key`, `issue_type`. Health: `jira.project.read`.

| Operation / typed schema | Risk / minimum Gate | Idempotency | Timeout, s / attempts |
| --- | --- | --- | --- |
| [jira.project.read](../../contracts/integrations/v1/definitions/jira.yaml#L29) | READ / NONE | READ_ONLY | 20 / 3 |
| [jira.issue.search](../../contracts/integrations/v1/definitions/jira.yaml#L42) | READ / NONE | READ_ONLY | 30 / 3 |
| [jira.issue.read](../../contracts/integrations/v1/definitions/jira.yaml#L58) | READ / NONE | READ_ONLY | 20 / 3 |
| [jira.issue.create](../../contracts/integrations/v1/definitions/jira.yaml#L73) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 2 |
| [jira.issue.comment.write](../../contracts/integrations/v1/definitions/jira.yaml#L87) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 2 |
| [jira.issue.update_limited](../../contracts/integrations/v1/definitions/jira.yaml#L101) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [jira.issue.link.write](../../contracts/integrations/v1/definitions/jira.yaml#L116) | SENSITIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [jira.project.user.search](../../contracts/integrations/v1/definitions/jira.yaml#L132) | READ / NONE | READ_ONLY | 30 / 3 |
| [jira.project.user.read](../../contracts/integrations/v1/definitions/jira.yaml#L148) | READ / NONE | READ_ONLY | 30 / 3 |
| [jira.issue.transition.list](../../contracts/integrations/v1/definitions/jira.yaml#L163) | READ / NONE | READ_ONLY | 30 / 3 |
| [jira.issue.transition.apply](../../contracts/integrations/v1/definitions/jira.yaml#L177) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [jira.issue.comment.list](../../contracts/integrations/v1/definitions/jira.yaml#L190) | READ / NONE | READ_ONLY | 30 / 3 |
| [jira.issue.comment.read](../../contracts/integrations/v1/definitions/jira.yaml#L206) | READ / NONE | READ_ONLY | 30 / 3 |
| [jira.issue.comment.update](../../contracts/integrations/v1/definitions/jira.yaml#L220) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [jira.issue.comment.delete](../../contracts/integrations/v1/definitions/jira.yaml#L235) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [jira.issue.link.list](../../contracts/integrations/v1/definitions/jira.yaml#L248) | READ / NONE | READ_ONLY | 30 / 3 |
| [jira.issue.link.read](../../contracts/integrations/v1/definitions/jira.yaml#L262) | READ / NONE | READ_ONLY | 30 / 3 |
| [jira.issue.link.delete](../../contracts/integrations/v1/definitions/jira.yaml#L278) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [jira.attachment.list](../../contracts/integrations/v1/definitions/jira.yaml#L291) | READ / NONE | READ_ONLY | 30 / 3 |
| [jira.attachment.read](../../contracts/integrations/v1/definitions/jira.yaml#L305) | READ / NONE | READ_ONLY | 30 / 3 |
| [jira.attachment.upload](../../contracts/integrations/v1/definitions/jira.yaml#L322) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [jira.attachment.delete](../../contracts/integrations/v1/definitions/jira.yaml#L341) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |

### Confluence (confluence 1.2.0)

Connection fields: `base_url`, `auth_scheme`, `username` (optional), `space_id`. Health: `confluence.space.read`.

| Operation / typed schema | Risk / minimum Gate | Idempotency | Timeout, s / attempts |
| --- | --- | --- | --- |
| [confluence.space.read](../../contracts/integrations/v1/definitions/confluence.yaml#L28) | READ / NONE | READ_ONLY | 20 / 3 |
| [confluence.page.search](../../contracts/integrations/v1/definitions/confluence.yaml#L41) | READ / NONE | READ_ONLY | 30 / 3 |
| [confluence.page.read](../../contracts/integrations/v1/definitions/confluence.yaml#L57) | READ / NONE | READ_ONLY | 30 / 3 |
| [confluence.page.create](../../contracts/integrations/v1/definitions/confluence.yaml#L73) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 2 |
| [confluence.page.update](../../contracts/integrations/v1/definitions/confluence.yaml#L90) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [confluence.attachment.upload](../../contracts/integrations/v1/definitions/confluence.yaml#L108) | SENSITIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 45 / 1 |
| [confluence.space.list](../../contracts/integrations/v1/definitions/confluence.yaml#L125) | READ / NONE | READ_ONLY | 30 / 3 |
| [confluence.page.descendant.list](../../contracts/integrations/v1/definitions/confluence.yaml#L138) | READ / NONE | READ_ONLY | 30 / 3 |
| [confluence.page.comment.list](../../contracts/integrations/v1/definitions/confluence.yaml#L154) | READ / NONE | READ_ONLY | 30 / 3 |
| [confluence.page.comment.read](../../contracts/integrations/v1/definitions/confluence.yaml#L171) | READ / NONE | READ_ONLY | 30 / 3 |
| [confluence.page.comment.create](../../contracts/integrations/v1/definitions/confluence.yaml#L189) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [confluence.page.comment.update](../../contracts/integrations/v1/definitions/confluence.yaml#L208) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [confluence.page.comment.delete](../../contracts/integrations/v1/definitions/confluence.yaml#L228) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [confluence.attachment.list](../../contracts/integrations/v1/definitions/confluence.yaml#L242) | READ / NONE | READ_ONLY | 30 / 3 |
| [confluence.attachment.read](../../contracts/integrations/v1/definitions/confluence.yaml#L258) | READ / NONE | READ_ONLY | 30 / 3 |
| [confluence.attachment.delete](../../contracts/integrations/v1/definitions/confluence.yaml#L276) | DESTRUCTIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |

### Электронная почта (email 1.4.0)

Connection fields: `base_url`, `from_address`, `mailbox_id`. Health: `email.delivery.health.read`.

| Operation / typed schema | Risk / minimum Gate | Idempotency | Timeout, s / attempts |
| --- | --- | --- | --- |
| [email.delivery.health.read](../../contracts/integrations/v1/definitions/email.yaml#L0) | READ / NONE | READ_ONLY | 20 / 1 |
| [email.mailbox.list](../../contracts/integrations/v1/definitions/email.yaml#L0) | READ / NONE | READ_ONLY | 20 / 1 |
| [email.message.list](../../contracts/integrations/v1/definitions/email.yaml#L0) | READ / NONE | READ_ONLY | 20 / 1 |
| [email.message.search](../../contracts/integrations/v1/definitions/email.yaml#L0) | READ / NONE | READ_ONLY | 20 / 1 |
| [email.message.read](../../contracts/integrations/v1/definitions/email.yaml#L0) | READ / NONE | READ_ONLY | 20 / 1 |
| [email.attachment.read](../../contracts/integrations/v1/definitions/email.yaml#L0) | READ / NONE | READ_ONLY | 20 / 1 |
| [email.message.send](../../contracts/integrations/v1/definitions/email.yaml#L0) | SENSITIVE / NONE | EFFECT_KEY | 20 / 1 |
| [email.message.reply](../../contracts/integrations/v1/definitions/email.yaml#L0) | SENSITIVE / NONE | EFFECT_KEY | 20 / 1 |
| [email.message.reply_all](../../contracts/integrations/v1/definitions/email.yaml#L0) | SENSITIVE / NONE | EFFECT_KEY | 20 / 1 |
| [email.message.forward](../../contracts/integrations/v1/definitions/email.yaml#L0) | SENSITIVE / NONE | EFFECT_KEY | 20 / 1 |
| [email.message.delete](../../contracts/integrations/v1/definitions/email.yaml#L0) | DESTRUCTIVE / NONE | EFFECT_KEY | 20 / 1 |
| [email.message.status.read](../../contracts/integrations/v1/definitions/email.yaml#L0) | READ / NONE | READ_ONLY | 20 / 1 |
| [email.thread.read](../../contracts/integrations/v1/definitions/email.yaml#L0) | READ / NONE | READ_ONLY | 20 / 1 |
| [email.attachment.list](../../contracts/integrations/v1/definitions/email.yaml#L0) | READ / NONE | READ_ONLY | 20 / 1 |
| [email.message.mark_read](../../contracts/integrations/v1/definitions/email.yaml#L0) | SENSITIVE / NONE | EFFECT_KEY | 20 / 1 |
| [email.message.mark_unread](../../contracts/integrations/v1/definitions/email.yaml#L0) | SENSITIVE / NONE | EFFECT_KEY | 20 / 1 |
| [email.message.move](../../contracts/integrations/v1/definitions/email.yaml#L0) | SENSITIVE / NONE | EFFECT_KEY | 20 / 1 |
| [email.message.archive](../../contracts/integrations/v1/definitions/email.yaml#L0) | SENSITIVE / NONE | EFFECT_KEY | 20 / 1 |
| [email.draft.create](../../contracts/integrations/v1/definitions/email.yaml#L0) | SENSITIVE / NONE | EFFECT_KEY | 20 / 1 |
| [email.draft.update](../../contracts/integrations/v1/definitions/email.yaml#L0) | SENSITIVE / NONE | EFFECT_KEY | 20 / 1 |
| [email.draft.delete](../../contracts/integrations/v1/definitions/email.yaml#L0) | DESTRUCTIVE / NONE | EFFECT_KEY | 20 / 1 |

### Mattermost (mattermost 2.2.0)

Connection fields: `base_url`, `team_name`, `channel_name`. Health: `mattermost.team.read`.

| Operation / typed schema | Risk / minimum Gate | Idempotency | Timeout, s / attempts |
| --- | --- | --- | --- |
| [mattermost.inbound](../../contracts/integrations/v1/definitions/mattermost.yaml#L36) | READ / NONE | READ_ONLY | 15 / 2 |
| [mattermost.notifications](../../contracts/integrations/v1/definitions/mattermost.yaml#L47) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [mattermost.result_mirror](../../contracts/integrations/v1/definitions/mattermost.yaml#L59) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [mattermost.gate_decisions](../../contracts/integrations/v1/definitions/mattermost.yaml#L71) | SENSITIVE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [mattermost.team.read](../../contracts/integrations/v1/definitions/mattermost.yaml#L29) | READ / NONE | READ_ONLY | 15 / 2 |
| [mattermost.channel.read](../../contracts/integrations/v1/definitions/mattermost.yaml#L97) | READ / NONE | READ_ONLY | 15 / 2 |
| [mattermost.channel.members.list](../../contracts/integrations/v1/definitions/mattermost.yaml#L112) | READ / NONE | READ_ONLY | 15 / 2 |
| [mattermost.post.list](../../contracts/integrations/v1/definitions/mattermost.yaml#L127) | READ / NONE | READ_ONLY | 20 / 2 |
| [mattermost.post.read](../../contracts/integrations/v1/definitions/mattermost.yaml#L142) | READ / NONE | READ_ONLY | 15 / 2 |
| [mattermost.thread.read](../../contracts/integrations/v1/definitions/mattermost.yaml#L159) | READ / NONE | READ_ONLY | 20 / 2 |
| [mattermost.post.search](../../contracts/integrations/v1/definitions/mattermost.yaml#L175) | READ / NONE | READ_ONLY | 20 / 2 |
| [mattermost.file.list](../../contracts/integrations/v1/definitions/mattermost.yaml#L191) | READ / NONE | READ_ONLY | 20 / 2 |
| [mattermost.file.read](../../contracts/integrations/v1/definitions/mattermost.yaml#L204) | READ / NONE | READ_ONLY | 30 / 2 |
| [mattermost.reaction.list](../../contracts/integrations/v1/definitions/mattermost.yaml#L225) | READ / NONE | READ_ONLY | 20 / 2 |
| [mattermost.post.send](../../contracts/integrations/v1/definitions/mattermost.yaml#L238) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [mattermost.post.update](../../contracts/integrations/v1/definitions/mattermost.yaml#L251) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [mattermost.reaction.add](../../contracts/integrations/v1/definitions/mattermost.yaml#L264) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |
| [mattermost.reaction.remove](../../contracts/integrations/v1/definitions/mattermost.yaml#L277) | WRITE / HUMAN_EACH_EFFECT | EFFECT_KEY | 30 / 1 |

### Synthetic HTTP (synthetic 3.1.0)

Connection fields: `journal`. Health: `synthetic.journal.read`.

| Operation / typed schema | Risk / minimum Gate | Idempotency | Timeout, s / attempts |
| --- | --- | --- | --- |
| [synthetic.journal.read](../../contracts/integrations/v1/definitions/synthetic.yaml#L28) | READ / NONE | READ_ONLY | 5 / 3 |
| [synthetic.journal.write](../../contracts/integrations/v1/definitions/synthetic.yaml#L53) | WRITE / HUMAN_EACH_EFFECT | PROVIDER_NATIVE | 5 / 3 |

## Фактические локальные проверки

Все команды выполнялись в изолированном worktree #1242 без provider credentials,
SSH, deploy или доступа к живой БД. Go: GOMAXPROCS=4, -p 2, TMPDIR=/tmp.

| Область | Выполненная команда относительно каталога | Результат |
| --- | --- | --- |
| services/external/integration-gateway | go test -p 2 ./...; go build -p 2 -o /dev/null ./... | PASS; все 139 integration-owned operations проходят local adapter fixtures |
| services/internal/email-bridge | go test -p 2 ./...; go build -p 2 -o /dev/null ./... | PASS выполненных tests/build; PostgreSQL suites skipped, ниже NOT RUN |
| services/external/interaction-gateway | go test -p 2 ./...; go build -p 2 -o /dev/null ./... | PASS; callable catalog, subscriptions, claim/receipt, scope/range/gate negatives |
| services/internal/control-plane | Целевая команда ниже | PASS выполненных unit; PostgreSQL component cases skipped |
| services/staff/control-center | npm run test:unit -- src/features/integrations src/features/managed-configurations/integration-package.test.ts src/features/managed-configurations/integration-package-isolation.test.ts | PASS: 21 files, 102 tests |
| services/staff/control-center | npm run build | PASS: vue-tsc и Vite; существующее предупреждение chunk >500 KiB |
| services/staff/control-center | npx eslint src/features/integrations/ui/IntegrationGrantsPanel.vue src/features/integrations/ui/IntegrationGrantsPanel.clear.test.ts --max-warnings 0 | PASS |
| services/staff/control-center | npx prettier --write src/features/integrations/ui/IntegrationGrantsPanel.vue src/features/integrations/ui/IntegrationGrantsPanel.clear.test.ts | Форматирование выполнено |

Целевая проверка control-plane:

```bash
GOMAXPROCS=4 TMPDIR=/tmp go test -p 2 \
  ./internal/domain/service/emailpolicy ./internal/repository/postgres/platform \
  -run 'IntegrationGrant|IntegrationInvocation|RuntimeIntegration|FilterIntegration|Email' -count=1
```

PostgreSQL tests требуют EMAIL_BRIDGE_TEST_DSN/EMAIL_BRIDGE_TEST_ADMIN_DSN
и KODEX_CONTROL_PLANE_TEST_DSN. Эти disposable DSN не настраивались: durable
SQL restart/concurrency, owner grant admission в БД, outbox/inbox broker path,
итоговый environment render, browser E2E и весь live provider surface — NOT RUN.
Успешный выход go test с skipped cases не доказывает эти сценарии.

Негативные local fixtures покрывают точные definition/version/risk/Gate/scope/
input pins до credential; чужие repository/project/page/post/attachment;
429 и bounded retry; большие и повреждённые ответы; TLS/STARTTLS downgrade;
UID/UIDVALIDITY и snapshot cursor; read без изменения Seen; mutation unknown
без replay; SMTP/IMAP effect reservation; текущую mailbox projection и exact
owner binding. Protocol fixtures поднимают локальные SMTP/POP3/IMAP и HTTPS
серверы. Vendor HTTP fixtures исполняют настоящий adapter/mapping с контролируемым
ответом; они не подтверждают совместимость реального vendor deployment.

Проверены Context7 GitHub REST API (/websites/github_en_rest): Actions
workflow run rerun/cancel/jobs. Context7 не нашёл требуемую Go-библиотеку
knadh/go-pop3; использован официальный [RFC 1939](https://www.rfc-editor.org/rfc/rfc1939.html).
Новых vendor endpoints, зависимостей или схем в PR нет. Зафиксированные vendor
контракты и ссылки находятся в [каталоге gateway](../../services/external/integration-gateway/OPERATION_MATRIX.md).

## Ручная приёмка после отдельного разрешения владельца

1. Зафиксировать exact deployed SHA, package digests, connection/mailbox revisions
   и разрешённый provider profile. Проверить shared secret/egress materialization
   и рабочий readiness, не заменять их готовностью HTTP listener.
2. Для каждой из семи строк протокольной матрицы создать разрешённое test connection
   с exact scope. Проверить form/YAML round-trip, secret reference, health и
   отсутствие чужих ресурсов в connections/projects/recipients/capabilities.
3. В rich picker выполнить поиск, next page/infinite scroll, keyboard navigation,
   empty/error/retry. Выбрать Project/recipient, обновить тот же объект, снова
   выбрать его и убедиться, что capability очищена до submit. Смена connection
   или её версии также сбрасывает зависимые страницы.
4. Выдать отдельный grant каждой нужной operation из таблиц. Проверить MCP schema,
   фактический endpoint и exact provider resource, отрицательный foreign scope,
   revoke и смену package revision. Не предоставлять универсальный provider proxy.
5. Для каждого разрешённого write заранее согласовать безопасный объект/адресата,
   пройти Human Gate, выполнить единственный эффект, прочитать owner receipt/audit.
   Повторить тот же idempotency key; provider effect должен остаться один.
6. Отдельно проверить неизвестный исход, обрыв completion, restart и owner
   reconciliation; UNKNOWN не означает NO_EFFECT и не разрешает автоматический
   повтор. Эти проверки не отправлять в живую систему без согласованного fixture.
7. Для Email пройти SMTP, IMAP и POP3 отдельно, все 21 capability с применимостью
   профиля, scopes From/To/Cc/Bcc/folders, UIDVALIDITY, attachment bytes и policy
   DENY/ALLOW/HUMAN_GATE. Проверить реальный owner report journal recovery.
8. Записать requirement/operation, exact SHA/revisions, expected/actual,
   PASS/FAIL/NOT RUN и безопасное evidence; неизвестный исход и provider acceptance
   не объявлять PASS по local fixture. Общий live follow-up: #1031.

## Риски и rollback

Изменение ограничено PWA cascade/context keys и документацией. При выборе
родительского candidate зависимые поля нужно выбрать заново, даже если ref не
изменился; это намеренно исключает устаревший selection. Rollback: отдельный PR
с отменой изменения PWA; миграции, vendor effects и секреты не затронуты.
Старые открытые Issues не закрывались и не считались доказательством дефекта.
Секреты и приватные данные не раскрыты.
