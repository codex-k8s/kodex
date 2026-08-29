---
id: ARCH-MC-010
title: Runtime-controller и role Pod
type: architecture
status: approved
owner: architect
version: 1.2.0
updated: 2026-08-29
---

# Runtime-controller и role Pod

Документ закрепляет принятые решения `D1-A`-`D3-A`, `D4-B` и
`D5-A`-`D8-A` без совместимости с прежней моделью прототипа.

## Граница ответственности

Runtime-controller claim-ит server-owned execution tasks и materialize-ит
Kubernetes resources exact attempt. Он не запускает provider process внутри
себя и не владеет Project, Agent, Session, Turn, Run lifecycle, permissions,
callback route или Human Gate.

Control-plane выдаёт immutable `RuntimeExecution` с exact:

- organization/project/agent/session/turn/run/node/attempt refs;
- `RuntimeRevision` version и SHA-256;
- runtime configuration, provider policy, published overlay, Agent binding и
  instruction-template refs/versions/SHA-256;
- immutable `RuntimeEnvironmentRevision` и promoted role image
  `repository@sha256` с runtime ABI digest;
- non-secret environment values, exact Secret descriptors, разрешённый набор
  tools, resources, volumes, network policy и scoped RBAC digests;
- input/result bounds, capabilities и credential bindings;
- claim generation, fence и expiry.

Caller-provided owner/root/parent, role name, prompt и external conversation IDs
не используются как authority.

## RuntimeRevision перед каждым turn

Перед каждым новым turn, retry и continuation control-plane создаёт свежую
immutable `RuntimeRevision`. Она не копируется из предыдущего execution и не
меняется после выдачи claim. В revision входят точные опубликованные версии и
дайджесты Agent, instruction template, typed `config.toml` overlay, provider
policy, `RuntimeEnvironmentRevision`, promoted image, Secret grants, tool
manifest, integration/MCP bindings, resources, volumes, network и scoped RBAC.

Изменение образа, окружения, секрета, набора инструментов, конфигурации или
полномочий не мутирует уже начатый turn. Следующий turn той же Session получает
новую `RuntimeRevision`, а прежняя остаётся read-only частью lineage. Если хотя
бы одна ссылка устарела, отозвана, не опубликована, не совпадает по версии или
дайджесту либо больше не разрешена actor-у, materialization закрыто отклоняется.

Эффективные runtime-полномочия являются пересечением полномочий actor-а,
опубликованного environment profile и platform admission policy. Поле запроса,
пользовательский Dockerfile, env value, prompt template или имя инструмента не
могут расширить это пересечение.

## Обычный turn

Каждый turn, retry и continuation создаёт новый execution-scoped Pod из exact
promoted role image. Role image содержит собственное окружение, пакеты,
инструменты и ПО конкретной роли. Пользователь редактирует полный Dockerfile,
но supply chain собирает его только вместе с неизменяемым platform-owned final
wrapper: после недоверенных пользовательских стадий добавляет защищённые
`kodex-init` и `kodex-agent-runner` из trusted base, назначает обязательный
container contract и подтверждает runtime ABI перед promotion. Запуск напрямую
из пользовательской стадии или обход final wrapper запрещены.

Pod использует отдельный ServiceAccount, immutable Secret input, bounded
workspace и exact Secret projections. Он не получает namespace-wide access,
control-plane database DSN, integration/provider master credentials, registry
push/admin credential или external channel token.

Runtime-controller проверяет claimed revision/fence, создаёт immutable input и
execution-scoped opaque ticket в отдельном Secret и materialize-ит Pod.
`agent-runner` не claim-ит Turn повторно: он подтверждает уже выданную attempt
через exact mTLS callback + ticket, запускает provider runtime, передаёт bounded
progress, обслуживает разрешённые MCP servers/tools и завершает attempt через
typed RPC. Provider process работает отдельным UID без Kubernetes token и
authority credential.

Wire contract `kodex.agent-runner-input.v6` содержит non-secret environment
values и только Secret descriptors. Для каждого descriptor runtime-controller
читает exact immutable source Secret, сверяет name/key, UID,
`resourceVersion`, UTF-8/size и SHA-256, затем копирует проверенные байты в
immutable execution ticket под непрозрачным ключом `environment-<hash>`.
`runtime.json` сохраняет descriptor, но не value. Только `provider-runtime`
получает `env.secretKeyRef` на exact execution ticket/key; `role-runtime` не
получает Secret projection. Любой stale UID/resourceVersion, отсутствующий key
или digest mismatch закрывает materialization до создания Pod.

## Runtime Environment и инструменты

`RuntimeEnvironmentRevision` является immutable snapshot и pin-ит точный
promoted image `repository@sha256`; mutable tag, `latest` и автоматический
переход на новую image revision запрещены. Несколько окружений могут ссылаться
на один digest. Обновление Dockerfile создаёт новую image revision, а перевод
окружения на неё выполняется отдельной versioned операцией.

Окружение не устанавливает ПО. Оно определяет только:

- обычные process env values и exact versioned Secret references;
- поднабор инструментов из доказанного image tool manifest и их безопасные
  пользовательские описания;
- requests/limits, разрешённые volume kinds и mount policy;
- типизированные egress/network profiles;
- workload identity и scoped Kubernetes RBAC profile в пределах authority
  actor-а и admission policy платформы.

Raw Kubernetes `Role`, `RoleBinding`, `NetworkPolicy`, Pod spec и произвольный
ServiceAccount не являются полями окружения. Control-plane materialize-ит
типизированные profiles в exact resources и сохраняет их canonical digests в
`RuntimeRevision`.

Image build для каждого инструмента фиксирует `name`, canonical command,
executable path, version/probe и result digest в подписанном tool manifest.
Окружение может выбрать только элементы этого manifest. Перед запуском provider
process `kodex-init` повторяет bounded executable/readiness probes уже в
materialized container. Отсутствие executable, несовпавшая version/probe или
инструмент вне разрешённого набора закрыто завершают attempt до обработки
пользовательского prompt. В материализованный prompt попадает только этот
проверенный разрешённый поднабор.

## Secret-broker boundary

Control-center, browser и основной control-plane не получают Kubernetes
credentials для записи Secret. Специализированный `secret-broker` с минимальным
namespace-scoped ServiceAccount выполняет create/rotate/revoke и создаёт
versioned immutable Kubernetes Secret. PostgreSQL хранит только metadata:
scope, owner, type, Kubernetes reference, version, rotation state, timestamps и
безопасный `display_hint`; plaintext и обратимо зашифрованная копия там не
хранятся.

Обычные list/get, browser stream, audit, domain events, logs и cache никогда не
содержат значение. `display_hint` формируется только для строковых значений:
суммарно не более 15 процентов исходной длины и не более 12 символов; короткие,
binary и structured secrets не получают фрагментов значения.

Повторное раскрытие реализует принятый D4-B flow: отдельное exact permission,
свежая OIDC re-authentication и одноразовая короткоживущая reveal authorization.
Значение возвращает непосредственно `secret-broker` в `no-store` response,
который нельзя повторить, кэшировать или получить через resource API. Audit
фиксирует actor, secret/version, основание и исход reveal без значения.

Workload не использует reveal flow. Runtime-controller получает только
разрешённый `RuntimeRevision` descriptor, читает exact immutable source Secret и
копирует значение в execution-scoped ticket описанным выше способом. Отзыв или
rotation закрывает новые turns; уже выданная revision не становится источником
для следующего execution.

Agent-runner не объединяет TOML как текст. Он повторно разбирает published
overlay strict parser-ом и кодирует один typed `config.toml`: model,
approval/sandbox, permissions, auth store, MCP и shell boundary назначает
сервер; overlay заполняет только закрытый non-authority allowlist; environment
set управляет только разрешёнными process environment names. Ни overlay, ни
environment set не могут переопределить server-owned поля.

Instruction template обрабатывается ограниченным Go `text/template` с
типизированными namespaces и закрытым allowlist функций. Поддерживается `range`
для подтверждённого списка tools; доступ к filesystem, process environment,
network, reflection и произвольным функциям отсутствует. Publish требует parse,
type validation и preview на безопасных descriptors. Рендер использует только
значения, закреплённые в текущей `RuntimeRevision`; Secret values в template
catalog и prompt не передаются.

Terminal Pod не переиспользуется для следующего turn. Retry получает новую
attempt, RuntimeRevision, claim, credentials и Pod; прежний execution остаётся
read-only lineage.

## MCP boundary

RuntimeRevision содержит только разрешённые bindings. Для managed integration
role Pod получает session-scoped MCP endpoint/credential, а provider secret и
external effect остаются в integration-gateway. Платформенные MCP tools
делегирования, callback, sync и owner attention вызывают специализированные
control-plane commands через тот же authorization context.

Raw provider response, stdout/stderr, Codex JSONL, arbitrary tool payload и
secret value не передаются в domain event или browser stream.

## Always-hot помощник

System assistant имеет отдельный warm materialization, потому что обычный
one-Pod-per-turn путь не обеспечивает hot-first-request. Reconciler поддерживает
ровно один ready system role Pod требуемой revision, resource limits, heartbeat
и durable system Session. Idle не является active Turn; turns выполняются FIFO.

После Pod/process restart controller materialize-ит warm runtime заново. Positive
assistant readiness означает, что exact desired prompt/runtime revision реально
обслуживается и может принять turn. Warm state не даёт database, Kubernetes или
secret-store authority; typed MCP operation повторно проверяет текущего User.
`Failed`/`Succeeded` warm Pod и immutable ticket другого controller instance или
revision являются stale materialization и заменяются reconciler-ом.

Повторный bootstrap сверяет digest текущего immutable core prompt. Более новая
поставляемая revision создаёт следующую published instruction version,
forward-only меняет desired runtime revision, переводит assistant в
`RECOVERING` и фиксирует системный audit. Rollback revision и совпавшая revision
с другим digest закрывают startup.

Warm report связан с exact desired revision и controller instance. Повторный
report того же state обновляет только `last_heartbeat_at`, не создаёт новые
idempotency receipts, audit и outbox events и не увеличивает aggregate version.
Смена state/revision/instance атомарно увеличивает version и публикует одно
событие. Поэтому heartbeat действительно поддерживает readiness и не создаёт
периодический поток ложных configuration changes.

## Health/readiness

- process `/healthz` проверяет только собственную жизнь;
- `/readyz` читает локальный рассчитанный snapshot и не выполняет network call
  на probe;
- direct readiness dependencies controller-а: local authority sidecar,
  Kubernetes API observation и NATS/claim consumer, если они используются;
- control-plane, integration-gateway, provider и optional adapters не входят в
  Pod readiness; рабочий отказ возвращает typed `Unavailable`;
- Kubernetes observation допускает bounded LKG только при transport failure;
  signature/digest/revision rollback/conflict/expiry fail closed немедленно;
- outage/recovery логируются один раз как state transition.

## Состояния materialization

`CLAIMED -> MATERIALIZING -> POD_READY -> RUNNING -> TERMINAL -> CLEANED`.
Cancel может перевести любой non-terminal execution в `CANCELLING -> CANCELLED`.
Lease expiry возвращает task в bounded retry либо создаёт terminal incident по
server policy. Controller cleanup не определяет terminal Run самостоятельно.

## Network и admission

Base deny-all. Role Pod получает DNS, exact provider egress proxy, exact MCP
service и только разрешённый project access profile. Admission проверяет exact
image digest, runtime ABI, container layout, ServiceAccount, volumes, commands,
resources, immutable input binding и execution ticket Secret. Admission также
требует exact revision/config/environment/tool/network/RBAC digests, запрещает
`stringData` и лишние ticket keys, а Secret projections разрешает только из
этого ticket в `provider-runtime`. Mutable image, extra container, raw
user-supplied Kubernetes policy, broad token, host access и privileged fallback
запрещены.

Прежние role images, mutable environment records, старый runner input,
неверсионированные Secret bindings и fallback на старый Pod contract не
поддерживаются и не materialize-ятся. Миграционный dual-read/dual-write путь не
создаётся.

## Критерии приёмки

- разные роли действительно запускаются в своих Docker images;
- пользовательский Dockerfile не может удалить или подменить final wrapper и
  runtime ABI;
- окружение запускает только закреплённый promoted digest и не следует за tag;
- выбранный tool доступен в image manifest и проходит probe в materialized Pod;
- новый turn создаёт новый Pod, а system assistant использует отдельный warm Pod;
- каждый turn/retry/continuation получает свежую immutable `RuntimeRevision`;
- stale claim/fence/revision не запускает workload;
- stale Secret revision или digest не запускает workload, а runner input не
  содержит Secret values;
- обычные API не возвращают Secret value, а reveal требует permission, свежую
  OIDC re-authentication и одноразовый `no-store` ответ secret-broker;
- Mattermost и любая optional integration не участвуют в materialization;
- cancel/retry/restart не создают две активные attempt одного Turn.
