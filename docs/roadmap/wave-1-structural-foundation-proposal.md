---
id: ROAD-MC-006
title: Архитектурное предложение волны 1 «Структурный фундамент»
type: roadmap
status: proposed
owner: architect
version: 0.3.0
updated: 2026-07-16
---

# Архитектурное предложение волны 1 «Структурный фундамент»

## Статус и границы доказательств

Документ является исправленным предложением по [GitHub Issue #65](https://github.com/codex-k8s/matter-codex/issues/65). Фактические выводы относятся к `main` в коммите `719974b8de8d28b3404f161d70994fed45a79cf1`. Предложение уточняет путь реализации [ADR-MC-001](../decisions/0001-evolutionary-service-boundaries.md), [целевых границ сервисов](../architecture/service-boundaries.md) и [доменных границ](../architecture/domain-map.md), но не заменяет их.

В документ входят:

- карта текущего `bot-service`, `agent-runner`, таблиц, миграций и границ доверия;
- обязательное сдерживание подтверждённых рисков до структурных переносов;
- контракты квитанции входного события, разветвления команд, аренды хода, завершения, обратного вызова и исходящего журнала;
- совместимые состояния `expand -> cutover -> contract` и матрица `N-1/N`;
- первые пять независимо реализуемых PR и очередь последующих задач;
- связь с задачами [#51](https://github.com/codex-k8s/matter-codex/issues/51), [#58](https://github.com/codex-k8s/matter-codex/issues/58), [#59](https://github.com/codex-k8s/matter-codex/issues/59), [#60](https://github.com/codex-k8s/matter-codex/issues/60), [#61](https://github.com/codex-k8s/matter-codex/issues/61) и [#66](https://github.com/codex-k8s/matter-codex/issues/66).

В этот результат не входят прикладной код, SQL-миграции, Kubernetes-манифесты, Ingress, развертывание и изменения действующей среды. Кластер, рабочая PostgreSQL и S3 не инспектировались. Наличие архивов, число реплик и фактические значения конфигурации не утверждаются. Значения секретов не читались.

Отсутствие проверки credential у action/dialog — не сохраняемый контракт `P2`, а подтверждённая уязвимость текущей публичной границы. Любая реализация структурной части волны блокируется до принятого и развёрнутого PR-0.

## Краткий вывод

Первые пять результатов волны должны идти в таком порядке:

1. `PR-0` закрывает публичную границу Mattermost: узкий Ingress, удостоверение action/dialog, проверенный actor, защита от replay и SSRF.
2. `PR-1` создаёт обязательный герметичный и PostgreSQL-тестовый контур и полную матрицу характеристических и secret-canary проверок.
3. `PR-2` выключает автоматическое разрушительное удаление сессионных PVC/Secret, включая путь квоты, пока нет доказанной допустимости по PostgreSQL и архиву.
4. `PR-3` вводит сериализацию сессии, lease/heartbeat/fencing, единую PostgreSQL-транзакцию завершения после archive upload, атомарный intent обратного вызова, локальный исходящий журнал результата и checkpoint `S3b bridge-ready`.
5. `PR-4` вводит версионированный конверт, одну квитанцию provider event, детерминированное разветвление в `N` команд/ходов и честную `at-least-once` доставку Mattermost.

Только после этих пяти PR допускаются узкие репозиторные порты, перенос транспорта и порт среды выполнения. `replicas >= 2` запрещены до зелёных gate квитанции/outbox/idempotency, leader/lease/fencing и управления одиночными циклами.

## 1. Фактическая карта текущей реализации

### 1.1 Компоненты и ответственность

```text
Интернет/Mattermost/GitHub
              |
       общий Ingress Prefix /
              |
         bot-service
      /       |        \
Mattermost PostgreSQL Kubernetes API
                         |
                  Pod agent-runner
                         |
           internal API и сессионный MCP
```

| Компонент | Фактическая ответственность | Свидетельство |
| --- | --- | --- |
| Корень сборки `bot-service` | Открывает PostgreSQL, при включённом режиме запускает миграции, собирает сервисы и запускает HTTP, WebSocket listener, repair и retention loops. | [`cmd/bot-service/main.go`](../../services/external/bot-service/cmd/bot-service/main.go), [`internal/app/app.go`](../../services/external/bot-service/internal/app/app.go) |
| HTTP-маршрутизатор | В одном `ServeMux` регистрирует health, readiness, metrics, slash/action/dialog, GitHub webhook, internal session API и MCP. | [`router.go`](../../services/external/bot-service/internal/transport/http/router.go), [`mcp.go`](../../services/external/bot-service/internal/transport/http/mcp.go) |
| Маршрутизация Mattermost | Фильтрует сообщения, выбирает одну или несколько целевых ролей и для каждой цели отдельно вызывает постановку хода. | [`chat_runtime.go`](../../services/external/bot-service/internal/domain/service/chat_runtime.go), [`chat_runtime_test.go`](../../services/external/bot-service/internal/domain/service/chat_runtime_test.go) |
| Сессии и ходы | Авторизует runner, выдаёт ход, завершает его, сохраняет snapshot, меняет карточку и напрямую публикует результат. | [`agent_session_service.go`](../../services/external/bot-service/internal/domain/service/agent_session_service.go), [`agent_session_service_test.go`](../../services/external/bot-service/internal/domain/service/agent_session_service_test.go) |
| Делегирование | Создаёт целевой тред и ход, а при возврате сначала создаёт или меняет callback turn и только затем выполняет CAS записи делегирования. | [`agent_delegation_service.go`](../../services/external/bot-service/internal/domain/service/agent_delegation_service.go), [`agent_delegations__set_callback.sql`](../../services/external/bot-service/internal/repository/postgres/admin/sql/agent_delegations__set_callback.sql) |
| Kubernetes-адаптер | Создаёт и удаляет Pod/PVC/Secret/Job/ConfigMap, выбирает ServiceAccount, монтирует учётные данные и выполняет retention. | [`runner.go`](../../services/external/bot-service/internal/integration/kubernetes/runner.go), [`runner_test.go`](../../services/external/bot-service/internal/integration/kubernetes/runner_test.go) |
| `agent-runner` | Получает и завершает ходы, запускает Codex, передаёт прогресс и сохраняет base64 tar/gzip snapshot. | [`agent-runner/main.go`](../../services/jobs/agent-runner/cmd/agent-runner/main.go), [`agent-runner/main_test.go`](../../services/jobs/agent-runner/cmd/agent-runner/main_test.go) |
| PostgreSQL-адаптер | Один `admin.Repository` обслуживает все таблицы через один `pgxpool.Pool`; миграции встроены в `bot-service`. | [`domain/repository/admin`](../../services/external/bot-service/internal/domain/repository/admin/repository.go), [`repository/postgres/admin`](../../services/external/bot-service/internal/repository/postgres/admin/repository.go), [`migrations.go`](../../services/external/bot-service/internal/repository/postgres/migrations/migrations.go) |

### 1.2 Подтверждённые разрывы

| Разрыв | Фактическое состояние |
| --- | --- |
| Публичные маршруты | [`ingress.yaml.tpl`](../../deploy/k8s/bot-service/ingress.yaml.tpl) публикует `/` с `pathType: Prefix`, поэтому наружу попадает весь `ServeMux`, включая `/internal/agent-sessions/`, `/mcp/sessions/` и `/metrics`. |
| Action/dialog authentication | Slash проверяет общий token, GitHub webhook — HMAC. `handleAgentsAction` и `handleAgentsDialog` принимают actor и контекст из JSON без равноценной credential check. `publishDialogResult` вызывает переданный `request.URL`. |
| Multi-mention | `routeChatPost` возвращает цель для каждой упомянутой роли, а `HandleChatPost` ставит отдельный ход каждой цели. Один provider event законно создаёт `N` ходов. |
| Claim | [`agent_session_turns__claim_next.sql`](../../services/external/bot-service/internal/repository/postgres/admin/sql/agent_session_turns__claim_next.sql) не блокирует сессию. Две транзакции могут не увидеть `running` и захватить разные строки `queued`. |
| Complete/stop | [`agent_session_turns__complete.sql`](../../services/external/bot-service/internal/repository/postgres/admin/sql/agent_session_turns__complete.sql) обновляет по `id` без status/version/lease CAS; остановка выполняется отдельным запросом [`agent_session_turns__cancel.sql`](../../services/external/bot-service/internal/repository/postgres/admin/sql/agent_session_turns__cancel.sql). |
| Внешняя доставка | `CompleteTurn` сначала переводит ход в терминальное состояние, затем отдельно вызывает Mattermost. Уникального локального delivery intent и доказанного provider idempotency primitive нет. |
| Callback | `ReturnToRequester` сначала создаёт/изменяет ход, затем устанавливает `callback_turn_id`; проигравший конкурентный CAS не отменяет уже созданный побочный эффект. |
| Retention | Retention включён по умолчанию и запускается с `DryRun: false`; путь quota retry также вызывает разрушительную очистку. Решение использует метки, фазу и возраст Kubernetes без PostgreSQL/S3 eligibility. |
| Полномочия | `bot-service` может управлять Pod/PVC/Secret/Job и `pods/exec`; runner может получить read-only либо `cluster-admin` ServiceAccount, прямые OpenAI/GitHub credentials и session/MCP token. |
| Тестовый контур | Текущий [`Makefile`](../../Makefile) содержит только `test-go`; единственный PostgreSQL-тест использует `TEST_DATABASE_DSN` и выполняет `t.Skip`. `test-go-postgres` и `test-go-all` отсутствуют. |

Текущая одна реплика в [`deployment.yaml.tpl`](../../deploy/k8s/bot-service/deployment.yaml.tpl) уменьшает вероятность части гонок, но не превращает их в обеспеченные инварианты.

## 2. Обязательный контур безопасности

### 2.1 Публичные и кластерные маршруты после PR-0

Эталонный снимок отрендерованного Ingress должен быть allowlist, а не отрицательным списком.

| Маршрут | Доступ после PR-0 | Серверная проверка |
| --- | --- | --- |
| `/mattermost/slash/agents` | Публичный только при необходимости действующей конфигурации Mattermost | constant-time проверка slash token, ограничение тела и метода |
| `/github/webhook` | Публичный | HMAC, ограничение тела и метода |
| `/mattermost/actions/agents` | Только Mattermost внутри кластера | одноразовая server-side capability и проверенный actor |
| `/mattermost/dialogs/agents` | Только Mattermost внутри кластера | одноразовая server-side capability и проверенный actor |
| health/readiness | Только Service/кластера | без секрета, но не через публичный Ingress |
| `/internal/agent-sessions/*` | Только кластер | действующая проверка session credential; отдельный credential с audience `runner-api` обязателен до runtime port |
| `/mcp/sessions/*` | Только кластер | действующая проверка session credential; отдельный credential с audience `mcp` и серверный grant обязательны до runtime port |
| `/metrics` | Только кластер/система наблюдаемости | сетевое ограничение и ServiceMonitor/аналог |

Action URL и dialog callback для Mattermost должны указывать на кластерный Service, а публичный Ingress — содержать только явно разрешённые пути. Проверка принимает эталон отрендерованного YAML и список маршрутов HTTP, чтобы новая внутренняя ручка не стала публичной автоматически.

### 2.2 Удостоверение action/dialog и actor

Минимальный механизм PR-0 — непрозрачная одноразовая capability, созданная сервером криптографически стойким генератором и сохранённая только в виде хеша. Запись связывает capability со следующими полями:

- `kind` action/dialog и разрешённая операция;
- внутренний идентификатор ресурса;
- проверенный actor или разрешённый серверной политикой набор actor;
- team/channel/post или dialog instance;
- время выдачи, время истечения и состояние `unused|consumed|expired|revoked`;
- хеш безопасного контекста, чтобы один token нельзя было применить к другим аргументам.

Callback в одной транзакции проверяет хеш, срок, точные bindings и состояние `unused`, затем переводит capability в `consumed`. Actor строится из сохранённой серверной записи и актуального сопоставления Mattermost, а `user_id`/`user_name` из payload считаются только заявленными полями. Несовпадение заявленного и проверенного actor даёт `401/403` без вызова прикладного сценария. Повтор, просроченный token, другая операция, channel, post или resource также отклоняются без побочного эффекта.

Capability удостоверяет callback, но не заменяет авторизацию. После неё сервер отдельно проверяет разрешение actor на операцию. Полная модель `Organization`/grants/quotas из #66 не входит в PR-0, однако seam обязан возвращать типизированный результат допуска `allowed|denied|indeterminate` с кодом причины; `indeterminate` трактуется как отказ. Prompt, `user_name`, инструкции, метки Kubernetes и idempotency key не являются правом.

Допустим эквивалентный механизм доверенного proxy/plugin с подписанным запросом и теми же bindings. Простая проверка IP источника, существования `user_id` или общий статический URL-token не закрывает actor spoof/replay.

### 2.3 Политика `response_url`

Предпочтительно не использовать URL из callback и публиковать результат через настроенный Mattermost client. Если `response_url` временно сохраняется, применяется единая политика:

1. origin точно совпадает с настроенным Mattermost origin; пользователь, fragment и неожиданный port запрещены;
2. `https` обязателен, кроме отдельно заданного кластерного Mattermost Service origin;
3. имя разрешается заново перед соединением, все A/AAAA проверяются; loopback, link-local, multicast, unspecified, metadata и частные/кластерные диапазоны запрещены, кроме точного заранее настроенного кластерного Service;
4. проверенный адрес закрепляется на соединение, чтобы повторное DNS-разрешение не обходило политику;
5. redirect запрещён; если в будущем разрешается, каждый переход проходит ту же проверку и имеет малый предел;
6. метод только `POST`, тело и ответ ограничены, тайм-аут задан, ошибки не содержат URL с чувствительными данными.

Отрицательные тесты покрывают внешний origin, userinfo, loopback IPv4/IPv6, private/link-local, cluster metadata, DNS rebinding и redirect на запрещённый адрес.

### 2.4 Матрица прямых и управляемых полномочий

Матрица фиксирует имена, но не значения переменных и секретов. Фактические пути материализации — `developer`, `reviewer`, `chat` и `session`; `smoke` и `codex-auth*` являются служебными путями без GitHub/MCP и с `automountServiceAccountToken: false`. Имена и PodSpec подтверждаются [`runner.go`](../../services/external/bot-service/internal/integration/kubernetes/runner.go), преобразование файлов GitHub в env — [`agent-runner/main.go`](../../services/jobs/agent-runner/cmd/agent-runner/main.go), naming account Secret — [`slash.go`](../../services/external/bot-service/internal/domain/service/slash.go), текущие ServiceAccount/RBAC — [`rbac.yaml.tpl`](../../deploy/k8s/bot-service/rbac.yaml.tpl), env `bot-service` — [`deployment.yaml.tpl`](../../deploy/k8s/bot-service/deployment.yaml.tpl), сетевые defaults — [`scripts/lib/env.sh`](../../scripts/lib/env.sh) и [`install-foundation.sh`](../../scripts/remote/install-foundation.sh).

Строка `direct` описывает действующий код. Строка `managed` описывает обязательный целевой контракт только там, где прямо написано «не реализовано»; это не существующая гарантия. Множества `D` и `M` ниже состоят из provider operations, RBAC `(verb, resource, scope)`, сетевых `(destination, port)` и доступных credential. Для каждой пары приёмка обязана доказать `M ⊆ D`; отсутствие доказательства означает отказ допуска.

| Профиль/возможность | Режим | Точные env/key/mount/ServiceAccount | Automount и RBAC | Сеть/порт и provider capability | Server-side grant/admission | Deny-by-default и доказательство `managed ⊆ direct` |
| --- | --- | --- | --- | --- | --- | --- |
| `bot-service` → Mattermost | `direct`, текущий | Secret `${MATTERCODEX_BOT_SERVICE_SECRET}`: env `MATTERCODEX_MATTERMOST_BOT_TOKEN` ← key `mattermost-bot-token`; `MATTERCODEX_MATTERMOST_SLASH_TOKEN` ← key `mattermost-slash-token`; mount нет; SA `matter-codex-bot-service` | automount в Pod/SA не задан, следовательно действует Kubernetes default `true`; дополнительно доступен namespaced Role `matter-codex-bot-service-runtime` | default internal destination `mattermost.${MATTERCODEX_NAMESPACE}.svc.cluster.local:8065`, иначе origin/port из `MATTERCODEX_MATTERMOST_INTERNAL_URL`/`MATTERCODEX_MATTERMOST_SITE_URL`; NetworkPolicy отсутствует, поэтому egress фактически не ограничен; provider scope bot token в репозитории не кодирован | slash token проверяется сервером; bot token разрешает операции Mattermost client; action/dialog эквивалентной проверки сейчас не имеют | не считать безопасным baseline для action/dialog; PR-0 блокирует дальнейшие структурные изменения |
| `bot-service` → Mattermost | `managed`, цель PR-0, не реализовано | те же два Secret key остаются только в edge-адаптере; runner не получает env/key/mount; отдельного SA не добавляется | не расширяет текущий Role; публичный Ingress не даёт доступ к internal/MCP/metrics | только настроенный Mattermost origin/Service и его явный port; provider operations ограничены allowlist slash/action/dialog/post/update | одноразовая capability, verified actor, operation/resource/channel/post bindings и типизированный `allowed\|denied\|indeterminate` | снимок env/mount и route/egress должен показать отсутствие новых credential/destination; allowlist операций должен быть подмножеством операций текущего Mattermost client |
| `bot-service` → PostgreSQL | `direct`, текущий | Secret `${MATTERCODEX_POSTGRES_SECRET}`: env `MATTERCODEX_DATABASE_DSN` ← key `mattermost-datasource`; mount нет; SA `matter-codex-bot-service` | тот же default automount и runtime Role, не нужные для PostgreSQL | installer направляет DSN на `mattermost-postgres.${MATTERCODEX_NAMESPACE}.svc.cluster.local:5432`; фактический destination/port остаётся в DSN, manifest не ограничивает egress; роль PostgreSQL задаётся вне репозитория и её SQL-права здесь не доказаны | допуск — только наличие/валидность DSN; один `admin.Repository` имеет широкий доступ | DSN не передаётся runner/DTO и не логируется; широкий SQL baseline не выдаётся за least privilege |
| `bot-service` → PostgreSQL | `managed`, целевая модульная граница, не реализовано | env/key остаются только у процесса-владельца схемы; runner и MCP не получают DSN; новый mount отсутствует | Kubernetes RBAC не добавляется | только PostgreSQL endpoint/port из DSN; provider capability — только consumer-owned repository ports и один migration owner | прикладной coordinator допускает типизированную команду; междоменный direct SQL запрещён | import/SQL contract tests и снимок env доказывают, что `M` удаляет общий `admin.Repository` у потребителей и не добавляет SQL/сеть |
| `bot-service` → GitHub | `direct`, текущий | Secret `${MATTERCODEX_GITHUB_SECRET}`: env `MATTERCODEX_GITHUB_TOKEN` ← key `github-token`; `MATTERCODEX_GITHUB_WEBHOOK_SECRET` ← key `github-webhook-secret`; mount нет; SA `matter-codex-bot-service` | тот же default automount и runtime Role, не нужные GitHub | `github.com:443`/`api.github.com:443` intended, но NetworkPolicy отсутствует; token scopes определены GitHub и не кодированы в репозитории | webhook HMAC проверяется; token используется native provider для repository/webhook operations, per-repository grant отсутствует | webhook без корректного HMAC отклоняется; token capability нельзя считать least privilege без provider scope snapshot |
| `bot-service` → GitHub | `managed`, будущая граница интеграции, не реализовано | эти GitHub env/key отсутствуют у control-plane; credential находится в gateway, SA `matter-codex-integration-github`; webhook signing key остаётся только edge adapter | Kubernetes RBAC не добавляется | edge принимает webhook; gateway egress только `github.com:443`/`api.github.com:443` | repository/capability grant, HMAC identity, actor/scope и idempotency admission | route/env/egress/provider-operation snapshot доказывает, что gateway grant не шире direct token/repository snapshot; неизвестный repository/operation даёт deny |
| `bot-service` → Kubernetes runtime | `direct`, текущий | SA `matter-codex-bot-service`; credential — projected SA token по default automount; отдельного env/key/mount в шаблоне нет | Role `matter-codex-bot-service-runtime`: `create,get,list,delete` PVC/ConfigMap/Pod; `get` `pods/log`; `create` `pods/exec`; `create,get,list,update,delete` Secret; `create,get,list,delete` Job; scope — `${MATTERCODEX_NAMESPACE}` | Kubernetes API из in-cluster config, обычно Service `:443`; NetworkPolicy отсутствует; provider capability равна перечисленному Role | HTTP/application admission отдельного grant не имеет; бизнес-сервис напрямую вызывает adapter | PR-0…PR-4 не меняют subjects/verbs/resources/scope; снимок Role и запрет новых imports являются отрицательным доказательством |
| `bot-service` → Kubernetes runtime | `managed`, будущий runtime port, не реализовано | целевой SA `matter-codex-runtime-controller`; token только у controller, не у `bot-service`/runner; Secret key/mount нет | точная копия или подмножество текущих namespaced rules; `pods/exec` и Secret verbs удаляются, если соответствующий use case не доказан отдельным gate | только Kubernetes API `:443`; NetworkPolicy запрещает иной egress | typed runtime command, resource ownership, namespace admission и idempotency key | rendered diff обязан доказать отсутствие новых subject/verb/resource/scope и `M ⊆ D`; неизвестная операция отклоняется |
| `smoke` utility | `direct`, текущий | env `MATTERCODEX_RUN_ID`, `MATTERCODEX_AGENT_ROLE`; Secret key/mount нет; workspace PVC `/workspace`; SA `matter-codex-agent-runner` | Pod и SA automount `false`; token/RBAC фактически не доступны контейнеру | NetworkPolicy отсутствует, хотя provider capability не требуется | отдельного server grant нет; Job создаёт `bot-service` runtime adapter | baseline capability set пуст; snapshot должен падать при появлении Secret/env credential, automount или provider destination |
| `smoke` utility | `managed`, целевой contract, не реализовано как отдельный режим | те же два несекретных env и workspace; Secret/mount нет; SA `matter-codex-agent-runner` | automount `false`, RBAC нет | egress deny-all | admission разрешает только локальный smoke command/image | `M = D = ∅` для credential/provider/RBAC; rendered Pod/NetworkPolicy snapshot является доказательством |
| `codex-auth`/`codex-auth-secret-check` utility | `direct`, текущий | env `MATTERCODEX_OPENAI_ACCOUNT`, `MATTERCODEX_CODEX_AUTH_SECRET`; auth Job использует emptyDir `/codex-home`; check Job монтирует volume `codex-auth-secret`, key `auth.json` в `/var/run/secrets/matter-codex-codex`; SA `matter-codex-agent-runner` | Pod и SA automount `false`; RBAC token нет | NetworkPolicy отсутствует; intended OpenAI device/auth HTTPS `:443` | account/SecretRef выбирает сервер; per-operation provider grant отсутствует | utility не получает GitHub/session/MCP/Kubernetes credential; exact Pod snapshot фиксирует это |
| Тот же auth use case | `managed`, будущий provider gateway, не реализовано | auth state и provider credential остаются в `matter-codex-provider-gateway`, SA того же имени; agent utility Job, `auth.json` mount и `MATTERCODEX_CODEX_AUTH_SECRET` отсутствуют | automount только gateway по отдельному Role; agent RBAC отсутствует | UI/edge → gateway Service port; gateway → allowlisted OpenAI auth endpoints `:443` | short-lived owner-authorized auth transaction, account binding, expiry и audit | отсутствие Job/mount и сравнение endpoint/account operations доказывают сужение; до отдельного ADR сохраняется direct utility |
| `developer`/`reviewer`/`chat`/`session` → OpenAI Codex | `direct`, текущий | volume `codex-auth-secret`, key/path `auth.json`, read-only mount `/var/run/secrets/matter-codex-codex`; runner копирует в `/workspace/codex-home/auth.json`; Secret name = `OpenAIAccount.SecretRef`, создаваемый как `<MATTERCODEX_CODEX_AUTH_SECRET>-<account>` (default base `matter-codex-codex-auth`); SA зависит только от Kubernetes capability | Pod override `automountServiceAccountToken: true` даже у SA с `false`; RBAC описан отдельными Kubernetes-строками ниже и не нужен OpenAI | NetworkPolicy отсутствует, поэтому egress не ограничен; intended provider — Codex/OpenAI HTTPS `:443`; provider capability определяется содержимым выбранной учётной записи и вне репозитория не ограничена | server выбирает `OpenAIAccountName`/SecretRef профиля; per-operation provider grant отсутствует | отсутствие выбранной учётной записи должно блокировать запуск; содержимое `auth.json` не попадает в prompt/log/result/archive |
| `developer`/`reviewer`/`chat`/`session` → OpenAI Codex | `managed`, не входит в PR-0…PR-4 и не реализовано | agent env `MATTERCODEX_PROVIDER_GATEWAY_URL`, `MATTERCODEX_PROVIDER_SESSION_TOKEN` ← key `provider-session-token` Secret `mc-provider-session-<session-key>`; `codex-auth-secret`/`auth.json` отсутствуют; gateway SA `matter-codex-provider-gateway` | automount agent Pod определяется независимой Kubernetes capability; provider RBAC отсутствует | agent → provider gateway Service port; gateway → allowlisted OpenAI endpoints `:443` | short-lived audience `provider`, account/revision/model/session grant; неизвестная capability отклоняется | negative snapshots подтверждают отсутствие auth mount/direct egress и что grant model/actions являются подмножеством direct account; эти имена — целевой contract, не существующий manifest |
| `developer`/`reviewer` и назначенные `chat`/`session` → GitHub | `direct`, текущий | volume `github-secret`; keys `github-token`, `github-username`, `github-email`; read-only mount `/var/run/secrets/matter-codex-github`; Secret name = `GitHubAccount.SecretRef`, generated `<MATTERCODEX_GITHUB_SECRET>` для `primary` или `<base>-<account>`; runner создаёт `GH_TOKEN`, `GITHUB_TOKEN`, `GITHUB_USERNAME`, `GITHUB_USER`, `GITHUB_EMAIL`, `GIT_*`, `MATTERCODEX_GIT_ASKPASS`, `MATTERCODEX_GITHUB_TOKEN_FILE`; SA зависит от Kubernetes capability | Pod override automount `true`; Kubernetes RBAC независимо от GitHub | NetworkPolicy отсутствует; intended `github.com:443`/`api.github.com:443`, но технически доступен любой reachable destination/port; token scopes задаются GitHub и в репозитории не доказаны | допуск — наличие GitHub account/SecretRef у профиля; per-repository/per-operation server grant отсутствует | без назначения у `chat`/`session` нет volume/env; `developer`/`reviewer` всегда получают заданный Secret; direct нельзя сочетать с обещанием mandatory approval |
| Те же профили → GitHub | `managed`, будущий integration gateway, не реализовано | в agent Pod отсутствуют `github-secret`, перечисленные GitHub/Git env и token file; остаются только `MATTERCODEX_MCP_URL` и отдельный credential audience `mcp` из key `mcp-token`; целевой gateway SA `matter-codex-integration-github` | agent automount не включается из-за GitHub; Kubernetes RBAC gateway не требуется | agent → integration gateway на cluster Service port; gateway → `github.com:443`/`api.github.com:443`; другие destinations запрещены | grant связывает organization/workspace/agent/repository/capability (`read\|issue\|pull_request\|contents_write\|admin`) и approval policy | snapshot env/mount/NetworkPolicy + negative tool catalog; набор GitHub operations каждого grant сравнивается с token scopes и direct profile, неизвестное даёт deny |
| Все рабочие профили → Kubernetes read-only | `direct`, текущий default | env Kubernetes инжектирует `KUBERNETES_SERVICE_*`/`KUBERNETES_PORT*`; projected files `/var/run/secrets/kubernetes.io/serviceaccount/{token,ca.crt,namespace}`; SA `${MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT}` (default `matter-codex-agent-runner`) | SA объявлен с automount `false`, но Pod явно ставит `true`; Role `matter-codex-agent-runner-readonly`: `get,list,watch` для core `pods,pods/log,services,endpoints,configmaps,persistentvolumeclaims,events`, apps `deployments,statefulsets,daemonsets,replicasets`, batch `jobs,cronjobs`, networking `ingresses`; scope namespace | Kubernetes API из injected env, обычно `:443`; egress не ограничен; provider capability — точный namespaced read-only Role | строка `kubernetes_access=read-only` проходит enum validation, но отдельного server-side grant нет | неизвестное значение нормализуется в `read-only`, однако запуск без Kubernetes capability сейчас не существует: token всё равно монтируется; это открытый gap |
| Те же профили → Kubernetes read-only | `managed`, будущий MCP, не реализовано | agent Pod: SA `matter-codex-agent-runner`, `automountServiceAccountToken: false`, projected token/env отсутствуют; только `MATTERCODEX_MCP_URL` + key `mcp-token`; gateway SA `matter-codex-integration-kubernetes-readonly` | gateway Role — точное подмножество текущего `matter-codex-agent-runner-readonly`; scope тот же namespace | agent → gateway Service port; gateway → Kubernetes API `:443`; прямой agent → API запрещён NetworkPolicy | grant `kubernetes.read` содержит allowlist resource/namespace; сервер повторно проверяет каждый verb/resource | RBAC/NetworkPolicy/tool snapshots и отрицательные `create/update/patch/delete/exec`; математическое сравнение rules доказывает `M ⊆ D` |
| Только профиль с `kubernetes_access=cluster-admin`, включая bootstrap `mattercodex-admin` | `direct`, текущий | те же injected env/projected files; SA `${MATTERCODEX_AGENT_RUNNER_CLUSTER_ADMIN_SERVICE_ACCOUNT}` (default `matter-codex-agent-runner-cluster-admin`) | SA `automount: false`, Pod override `true`; `ClusterRoleBinding` `matter-codex-agent-runner-cluster-admin` → встроенный `cluster-admin`, все verbs/resources, cluster scope | Kubernetes API `:443`, egress не ограничен; provider capability — полный cluster-admin | admission фактически ограничен enum/profile field; [`system_roles.go`](../../services/external/bot-service/internal/domain/service/system_roles.go) создаёт `mattercodex-admin` с этим значением; per-operation grant/approval отсутствует | до решения владельца новые назначения запрещены; существующая широкая capability не выдаётся за безопасную или управляемую |
| Тот же логический admin use case | `managed`, будущий MCP, не реализовано | agent Pod без Kubernetes token/env; gateway SA `matter-codex-integration-kubernetes-admin`; `cluster-admin` этому SA запрещён — права задаются отдельными Role/ClusterRole для принятого набора операций | automount только gateway; verbs/resources/scope не шире явно принятого direct use-case и снимка до миграции | agent → gateway Service port; gateway → Kubernetes API `:443`; прочий egress запрещён | `platform_admin` grant + human approval или явно принятый emergency grant, arguments hash, expiry, idempotency и audit | `cluster-admin` RoleBinding/ClusterRoleBinding в managed варианте отсутствует; negative tests для secret read, RBAC escalation, impersonate, exec и cluster-scoped writes вне allowlist доказывают сужение |
| `session` → runner API | `direct`, текущий внутренний путь | env `MATTERCODEX_BOT_SERVICE_URL`, `MATTERCODEX_SESSION_TOKEN`; volume `session-secret`, key `token` в Secret `mc-session-token-<session-key>`; тот же key mounted read-only как `/var/run/secrets/matter-codex-session/token`; SA как выше | automount `true`; Kubernetes RBAC независимо | default `matter-codex-bot-service.${MATTERCODEX_NAMESPACE}.svc.cluster.local:${MATTERCODEX_BOT_SERVICE_PORT}` (`8080` по умолчанию); NetworkPolicy отсутствует | bearer сверяется с session в `Snapshot/Claim/Complete/Status`; audience не отделена от MCP | Secret/session mismatch даёт deny, но reuse одного token для двух audiences — gap |
| `session` → runner API | `managed`, цель PR-3, не реализовано | env `MATTERCODEX_SESSION_TOKEN` ← key `runner-api-token`; mount `/var/run/secrets/matter-codex-session/runner-api-token`; отдельный `mcp-token`; SA не меняется | не добавляет RBAC | только cluster Service route `/internal/agent-sessions/*` на объявленном port | short-lived audience `runner-api`, session/turn/lease/fence/command bindings | cross-audience replay и чужая session отклоняются; credential/tool/route snapshot не добавляет provider capability |
| `session` → Mattermost без MCP | `direct`, текущий | Mattermost bot/slash token, keys и mounts в agent Pod отсутствуют; SA как выше | automount/RBAC независимо | прямое соединение runner → Mattermost не требуется контрактом, но NetworkPolicy сейчас его не запрещает | direct provider capability агенту не выдана; prompt также не является grant | baseline logical capability для сравнения задаётся разрешёнными серверными operations, а не отсутствующим token; прямой egress к Mattermost должен быть закрыт в целевом snapshot |
| `session` → Mattermost MCP tools | `managed`, текущий частичный | env `MATTERCODEX_MCP_URL`, `MATTERCODEX_MCP_TOKEN`; сейчас оба token env читают один key `token`; отдельного mount для MCP нет; SA как выше | automount `true`; Kubernetes RBAC независимо | bot-service route `/mcp/sessions/<session>`; egress не ограничен | bearer/session check; зарегистрированы thread/search/post/status/catalog/delegation/callback tools в [`mcp.go`](../../services/external/bot-service/internal/transport/http/mcp.go); per-tool grant отсутствует | runner не получает Mattermost bot token, но prompt-инструкция является только правилом поведения, не grant; текущий managed режим ещё не доказывает `M ⊆ D` |
| `session` → Mattermost MCP tools | `managed`, целевой контракт PR-3/PR-4, не реализовано | `MATTERCODEX_MCP_TOKEN` ← отдельный key `mcp-token`; `MATTERCODEX_SESSION_TOKEN` не принимается MCP; mount/provider credential отсутствуют | MCP не включает Kubernetes automount/RBAC | только MCP cluster Service port; server → Mattermost только через native adapter | audience `mcp`, allowlisted tool/capability, session/workspace/agent scope, verified actor и side-effect grant; prompt не участвует в admission | tool-catalog и cross-audience negative tests; операции каждого grant — подмножество текущего server tool set, неизвестный tool/scope/grant даёт deny и ноль side effects |
| Любой рабочий профиль → произвольная runtime variable | `direct`, текущий | env name = сохранённый `RuntimeEnvVar.Name`; Secret name = `SecretRef`; key = `SecretKey` или default `value`; mount нет; имя включается в `MATTERCODEX_RUNTIME_ENV_ALLOWLIST`; SA как у профиля | automount/RBAC независимо | из-за отсутствия NetworkPolicy значение может использоваться к любому reachable destination/port | допуск — enabled binding роли; schema capability/provider semantics отсутствуют | до typed capability это только direct и не может считаться managed; snapshot обязан перечислить точные имена конкретной `RuntimeRevision` без значений |
| Любой рабочий профиль → произвольная runtime variable | `managed`, будущий contract, не реализовано | соответствующий env/key полностью отсутствует в agent Pod; остаются `MATTERCODEX_MCP_URL` и `MATTERCODEX_MCP_TOKEN` ← key `mcp-token`; credential остаётся gateway; SA naming contract `matter-codex-integration-<IntegrationDefinition.metadata.name>` | agent не получает дополнительный automount/RBAC | только gateway Service port и allowlisted provider destination/port | IntegrationGrant + risk/approval/idempotency contract | negative snapshot конкретной `RuntimeRevision` проверяет отсутствие прежнего env и прямого egress; без типизированной capability migration запрещена |

Обязательное acceptance-доказательство для каждой фактически развёртываемой роли — side-by-side снимок `direct` и `managed`: env names, Secret/key references, mounts, Pod `serviceAccountName`/automount, нормализованные RBAC rules и scope, NetworkPolicy destinations/ports, provider capability и server grants. Проверка машинно сравнивает множества и падает при новой переменной, mount, subject, verb, resource, scope, destination, port или provider operation. Пустой/неизвестный grant и отсутствующий снимок дают deny. До появления этих реализаций managed-строки остаются acceptance contract, а не гарантией.

`replicas >= 2` остаются запрещены до зелёных gates квитанции/outbox/idempotency, leader/lease/fencing, single-loop ownership и матрицы `N-1/N`; одна только матрица полномочий этот запрет не снимает.

## 3. Владение данными и внешними идентификаторами

### 3.1 Один логический владелец таблицы и мутации

Все миграции физически остаются одним упорядоченным потоком `bot-service` до отдельного принятого владельца схемы. Применённые `000001`–`000020` не перемещаются и не редактируются.

| Таблица/группа | Логический владелец волны 1 | Разрешённые писатели |
| --- | --- | --- |
| проекты, чаты, участники, репозитории чата, `thread_contexts`, будущие Mattermost bindings и `InboundEventReceipt` | `conversations` | только прикладные сценарии `conversations` |
| профили, роли, шаблоны, runtime variables, bot identities | `agents` | только сценарии `agents`; создание bot идёт через порт Mattermost |
| OpenAI/GitHub accounts, credentials references, repositories | `providers` как переходный владелец | только сценарии `providers`; значения credential не хранятся в DTO |
| `matter_codex_agent_sessions`, `matter_codex_agent_session_turns`, `matter_codex_agent_delegations`, `matter_codex_agent_runs` | `runtime` | только транзакции `runtime` |
| `matter_codex_agent_flows` | `processes` | только переходные сценарии `processes` |
| `matter_codex_audit_events` | `audit` | append-only порт аудита |
| строка outbox | домен, создавший бизнес-изменение | транзакция этого домена; relay только меняет delivery state |
| `goose_db_version` и порядок миграций | один владелец схемы | один migration job/process; прикладные реплики миграции конкурентно не запускают |

Нельзя делить владение по колонкам одной таблицы. Междоменная команда или событие может вызвать мутацию владельца, но другой домен не выполняет прямой `UPDATE` и не читает таблицу как контракт.

### 3.2 Внешние идентификаторы Mattermost в sessions/turns

Таблицы sessions/turns целиком принадлежат `runtime`, включая физически существующие `mattermost_channel_id`, `mattermost_root_post_id`, `mattermost_post_id`, `user_id` и `user_name` из [`000014_agent_sessions.sql`](../../services/external/bot-service/internal/repository/postgres/migrations/000014_agent_sessions.sql). Эти поля имеют один из двух смыслов:

- неизменяемый snapshot цели доставки/источника на момент принятия команды;
- стабильная reference на binding, принадлежащий `conversations`.

Создаёт snapshot только транзакция `runtime` из типизированной команды. После создания исходные external IDs не редактируются из `conversations`; изменение binding создаёт новую версию/событие, а не междоменный `UPDATE`. `user_name` является снимком отображения и не используется для авторизации.

Потребитель `runtime` объявляет у себя порт чтения `ConversationRoutingPort`; его адаптер получает проверенный `ConversationRouteSnapshot` из владельца `conversations`. Порт возвращает внутренние references, проверенного actor и целевые external IDs без доступа к таблицам другого домена. Для доставки `conversations` объявляет собственный consumer-owned порт к `runtime` только для чтения типизированного delivery intent, а не таблиц sessions/turns.

Итоговый контракт:

- `conversations` владеет актуальным сопоставлением Workspace/Room/Conversation с Mattermost;
- `runtime` владеет каждой строкой session/turn и неизменяемым snapshot, использованным конкретным ходом;
- transport не владеет ни тем, ни другим и только преобразует DTO;
- прямое междоменное чтение и column-level split ownership запрещены.

## 4. Обязательные долговечные контракты

### 4.1 Версионированный конверт событий и команд

События и команды используют общий набор метаданных, но разные поля типа и версии.

| Поле | Контракт |
| --- | --- |
| `event_id`/`command_id` | внутренний неизменяемый идентификатор сообщения |
| `event_type` + `event_version` или `command_type` + `command_version` | стабильное имя и положительная целая версия схемы |
| `occurred_at`/`created_at` | серверное время события/команды |
| `authenticated_actor` | типизированная ссылка на actor, способ удостоверения и server-side subject reference |
| `asserted_actor` | необязательные поля provider payload только для диагностики; не authorization |
| `organization_scope` | обязательная installation-scoped ссылка уже в первом профиле; не требует таблиц полной #66 |
| `workspace_scope` | проверенная внутренняя ссылка на текущий Project/будущий Workspace |
| `session_scope` | необязательная внутренняя ссылка или детерминированный session discriminator |
| `correlation_id` | одна цепочка от provider event до turn и delivery |
| `causation_id` | непосредственное входное событие/команда, породившее сообщение |
| `idempotency_key` | детерминированный ключ повтора внутри указанного scope |
| `payload` | типизированная версия данных без секретов и неотфильтрованного provider payload |
| `admission` | типизированный результат `admitted\|ignored\|rejected\|duplicate\|deferred` и безопасный reason code |

`organization_scope` в волне 1 является стабильной ссылкой на одну установку и проходит через порт авторизации; создание общей модели Organization/Membership/grants/quotas остаётся в #66. Неизвестный scope, actor или admission даёт fail-closed `rejected`, а не значение по умолчанию.

Prompt, `user_name`, текст instructions, idempotency key и наличие Kubernetes label не являются авторизацией. Сохранённый результат duplicate возвращается только после повторной проверки actor и фактического scope агрегата.

### 4.2 Одна квитанция и детерминированное разветвление в N целей

`InboundEventReceipt` уникален по `(provider, provider_event_type, provider_event_id)`. Для Mattermost `provider_event_id` берётся из стабильной идентичности события/поста, а `provider_event_type` является обязательным discriminator, чтобы создание и изменение одного поста не смешивались. Квитанция хранит хеш канонического безопасного payload и admission outcome.

Алгоритм первого приёма:

1. Удостоверить источник/actor и вычислить scope.
2. Вычислить каноническую provider identity и попытаться создать одну квитанцию.
3. Детерминированно получить цели, удалить дубли и отсортировать по `target_discriminator`.
4. Сохранить неизменяемый target snapshot в результате квитанции.
5. Для каждой цели создать отдельный command intent и outbox в той же локальной транзакции.
6. `runtime` идемпотентно материализует отдельную session/turn на каждый command.

`target_discriminator` включает как минимум `target_role_id`, `target_session_scope` и канонический `session_key`/root reference. Ключи:

```text
receipt: <provider>:<provider_event_type>:<provider_event_id>
command: mattermost.user-instruction.v1:<receipt_id>:<hash(target_discriminator)>
turn:    runtime.turn.v1:<command_id>:<target_discriminator>
```

Повтор с тем же provider identity и другим payload hash даёт типизированный conflict и ноль побочных эффектов. Повтор после изменения ролей возвращает исходный сохранённый target snapshot и не добавляет новые цели. Один multi-mention event создаёт одну квитанцию, `N` команд и `N` ходов — по одному на целевую session.

Обязательны последовательный и конкурентный duplicate multi-mention тесты через два PostgreSQL-соединения. Они проверяют одну квитанцию, исходный `N`-fan-out, по одной команде/turn на discriminator и отсутствие новых целей при повторе.

### 4.3 Сериализация session, lease, heartbeat и fencing

Claim выполняется одной транзакцией:

1. блокируется строка session `FOR UPDATE` или эквивалентный session guard;
2. проверяется отсутствие живой аренды активного хода;
3. повтор с тем же `claim_request_id` возвращает ранее выданный claim;
4. выбирается старейший `queued` по `(sequence, id)`;
5. создаётся аренда с `lease_owner`, `lease_expires_at` и монотонным `fencing_token`;
6. ход меняется `queued -> running` через status/version CAS.

Частичный уникальный индекс «не более одного `running` на session» служит последней защитой, но не заменяет session lock. Heartbeat продлевает только совпадающие `turn_id + lease_owner + fencing_token + version`. Просроченный ход можно reclaim, увеличив fencing token. Старый worker после reclaim не может выполнить heartbeat, complete или stop.

Complete и stop конкурируют через один переход `where status = 'running' and version = ? and fencing_token = ?`. Победитель увеличивает version и создаёт terminal state; проигравший получает `stale_claim|already_terminal|conflict` и не меняет snapshot, run, outbox или Mattermost. Повтор победившей команды с тем же idempotency key возвращает сохранённый outcome.

Обязательные проверки PR-3:

- два worker одновременно claim одну session;
- потерянный HTTP-ответ и повтор того же `claim_request_id`;
- lease expiry/reclaim и рост fencing token;
- stale complete после reclaim;
- restart до claim, после claim, до complete и после локального complete;
- concurrent stop/complete и повтор каждой команды;
- минимум два настоящих PostgreSQL-соединения, без in-memory mutex как единственной защиты.

#### 4.3.1 Единая транзакция завершения после загрузки архива

Текущий [`CompleteTurn`](../../services/external/bot-service/internal/domain/service/agent_session_service.go) сначала переводит turn в terminal, а затем отдельными вызовами обновляет session snapshot, run и Mattermost; runner передаёт base64-архив в том же HTTP payload. Это подтверждённое crash window, а не допустимый целевой контракт.

PR-3 обязан заменить этот порядок следующим протоколом:

1. Worker с действующими `turn_id + lease_owner + fencing_token + version` запрашивает серверную completion reservation. CAS не делает turn terminal; он связывает `completion_attempt_id` с текущим fence и выдаёт короткоживущую upload capability только для неизменяемого ключа `session/<session_id>/turn/<turn_id>/fence/<fencing_token>/attempt/<completion_attempt_id>`.
2. До PostgreSQL-транзакции завершения S3 adapter загружает immutable archive version, вычисляет/проверяет checksum и возвращает `archive_version_id + storage_key + checksum + size`. Ошибка upload/verify оставляет turn nonterminal и retryable; reservation можно безопасно повторить с тем же idempotency key.
3. После upload одна PostgreSQL-транзакция повторно блокирует session/turn и проверяет status/version/lease/fence/completion attempt. Она атомарно:
   - переводит turn в единственное terminal состояние и сохраняет final/error/artifact metadata;
   - обновляет session snapshot и `codex_session_id`;
   - сохраняет `archive_version_id`, reference, checksum и version как текущий архив session;
   - меняет session status, очищает active turn, закрывает active lease/completion reservation и сохраняет последний монотонный fencing token без возможности отката к меньшему значению;
   - сохраняет status/result текущего run и outcome команды/idempotency key;
   - создаёт ровно один local outbox intent результата с ключом из раздела 4.5.
4. Только успешный commit делает terminal state и archive reference видимыми. Следующий claim блокирует ту же session row и получает новый snapshot, `codex_session_id` и archive reference, созданные победившим complete.

PostgreSQL и S3 не объединяются в распределённую транзакцию, и документ её не обещает. Если DB commit не состоялся после успешного upload, immutable archive version остаётся безопасным orphan без ссылки из terminal session. Reconciliation находит его по `completion_attempt_id`; GC удаляет только после grace period, проверки отсутствия PostgreSQL reference/hold и audit. Повтор complete может повторно использовать ту же проверенную version либо создать новую version, но terminal reference выбирается только победившим commit.

Запрос, который уже stale при выдаче reservation или при финальном CAS, не меняет turn, session, snapshot, `codex_session_id`, active lease/fence, run/outcome или outbox и не получает действующую upload capability. Если worker стал stale уже после принятого upload, но до commit, upload может остаться только непривязанным orphan по правилу выше; он не становится текущим архивом и не влияет на следующий claim. Это ограничение внешнего S3 side effect сформулировано явно и не скрывается обещанием межсистемной транзакции.

| Точка отказа/гонка | Состояние PostgreSQL | Состояние S3/outbox | Обязательная проверка |
| --- | --- | --- | --- |
| До reservation | turn остаётся `running` с текущей lease/fence | archive/outbox отсутствуют | повтор с тем же complete idempotency key начинает тот же логический attempt |
| После reservation, до upload | turn nonterminal/retryable; terminal/session snapshot/run не меняются | объекта и outbox нет | crash/restart освобождает или повторно использует reservation без нового terminal outcome |
| Ошибка upload или checksum verify | turn nonterminal/retryable; lease может быть продлена либо reclaim повышает fence | неуспешная version не получает reference; outbox отсутствует | следующий допустимый worker повторяет upload; stale attempt отвергается |
| Upload успешен, до DB transaction | terminal state отсутствует | immutable unreferenced version; outbox отсутствует | reconciliation видит attempt; next claim не использует этот orphan |
| Ошибка/rollback DB transaction | ни один из terminal turn/session/snapshot/run/outcome объектов не изменён | безопасный orphan; outbox отсутствует | fault injection после каждого SQL statement подтверждает полный rollback |
| Commit успешен, HTTP ack потерян | terminal turn, новый snapshot/archive/session/run/outcome и один intent видимы вместе | archive referenced; один `pending` outbox | повтор complete возвращает сохранённый outcome и не создаёт version/intent заново |
| Stale complete до upload | все перечисленные объекты неизменны | archive/outbox отсутствуют | старые lease owner/fence/version/attempt получают `stale_claim` |
| Fence устарел после upload | все PostgreSQL business objects неизменны | version остаётся orphan; outbox отсутствует | финальный CAS отклоняет worker; orphan проходит reconciliation/GC |
| Следующий claim после commit | видит только новую session version, snapshot, `codex_session_id` и archive reference | delivery может быть `pending`, что не блокирует чтение snapshot | два PostgreSQL-соединения: claim не может увидеть terminal turn со старым snapshot |

### 4.4 Атомарный callback intent

Возврат делегирования не создаёт и не меняет turn до фиксации права на callback. Одна локальная транзакция:

1. блокирует delegation;
2. проверяет terminal result и payload hash повтора;
3. переводит delegation в `callback_pending`;
4. создаёт ровно один command/outbox intent с ключом `delegation.callback.v1:<delegation_id>`;
5. сохраняет idempotency outcome.

Consumer с тем же deterministic key либо один раз добавляет callback к одному queued turn через version CAS, либо создаёт новый turn, затем сохраняет `callback_turn_id` в собственной транзакции. Повтор возвращает тот же outcome. Другой payload после первого принятого callback даёт conflict и не изменяет prompt.

Тесты: два конкурентных callback; crash до/после транзакции intent; crash после materialize turn до фиксации consumer outcome; restart; проигравший повтор не создаёт второй prompt/turn и получает ссылку на первый результат.

### 4.5 Локальный outbox и внешняя доставка Mattermost

Репозиторий не доказывает наличие у Mattermost idempotency primitive или надёжного readback по пользовательскому ключу. Поэтому внешний результат **не объявляется exactly-once**.

Доказуемый контракт:

- terminal transition и уникальный local outbox intent создаются в одной PostgreSQL-транзакции;
- ключ вида `mattermost.turn-result.v1:<turn_id>:<result_version>:<destination>` уникален локально;
- один fenced consumer выполняет `at-least-once` доставку с ограниченным exponential backoff и jitter;
- состояния: `pending|claimed|retry_wait|delivered|ambiguous|dead_letter`;
- успешный `post_id`, число попыток, безопасный код ошибки и correlation сохраняются;
- dead letter и ambiguous доступны оператору и процедуре reconciliation.

| Crash window | Доказуемый результат |
| --- | --- |
| До внешнего вызова | intent остаётся и безопасно повторяется; внешнего эффекта нет |
| После ошибки до retry state | lease истекает, intent повторяется |
| После подтверждённого ответа и локального ack | `delivered`, повтор не выполняется |
| После внешнего side effect, но до локального ack | состояние неоднозначно; повтор может создать внешний дубль |
| Timeout, когда provider мог принять запрос | `ambiguous`; автоматический повтор допускается только принятой политикой риска |

Reconciliation использует provider readback только после отдельного тестового доказательства. Без него оператор сверяет целевой thread, correlation/idempotency metadata и local outbox, затем помечает intent `delivered` либо разрешает повтор. Ручная проверка специально воспроизводит crash до и после side effect и подтверждает отсутствие потери локального intent; возможный внешний дубль в последнем окне фиксируется как ограничение, а не скрывается.

## 5. Обязательные тестовые шлюзы

### 5.1 Тестовый контур PostgreSQL как результат PR-1

Текущие команды ниже отсутствуют; PR-1 обязан их создать согласно [активному PostgreSQL guide](../design-guidelines/go/infrastructure_integration_requirements.md):

- `make test-go` — только герметичные unit/component tests, без PostgreSQL, Docker и тестовых DSN;
- `make test-go-postgres` — required режим repository/migration/concurrency tests; использует имена вида `MATTERCODEX_*_TEST_DATABASE_DSN`, не печатает значения и завершается явным failure/not-run, а не `t.Skip`;
- `make test-go-all` — последовательно выполняет оба контура и не маскирует отсутствие PostgreSQL;
- Kubernetes-native временный PostgreSQL — основной remote-agent путь; Docker допустим только как локальный fallback.

Harness применяет миграции с нуля и отдельно обновляет копию предыдущей схемы с репрезентативными session/turn/delegation данными. Для concurrency tests открываются минимум два независимых соединения/транзакции. Production DSN запрещён. PR-1 не должен описывать эти targets как уже существующие или объявлять PostgreSQL-проверку успешной без фактического запуска.

### 5.2 Матрица характеристических проверок

| Gate | Минимальный набор |
| --- | --- |
| Transport/auth | missing/invalid/replayed/expired capability, spoofed actor/channel/post, SSRF origin/IP/DNS/redirect; `401/403`, ноль DB/Kubernetes/Mattermost side effects; снимок public routes |
| Routing | unknown channel, человек/bot, `#notrigger`, thread/default role, multi-mention `N` targets |
| Receipt/fan-out | sequential/concurrent duplicate, target order, payload conflict, crash после каждой локальной стадии |
| Session | FIFO, two-worker claim, lost response, lease reclaim, stale complete, restart, stop/complete CAS; archive upload/verify, completion transaction rollback, orphan reconciliation и следующий claim с новым snapshot |
| Callback | concurrent return, crash/restart, deterministic consumer key, один resulting turn |
| Delivery | local unique outbox, retry/backoff/dead-letter, crash до/после side effect, ambiguous reconciliation |
| Retention | active, queued, approval, callback, no archive, archive failed, grace, unknown PostgreSQL, unknown S3 — удаление запрещено |
| Privileges | снимки env names/mounts/SA/RBAC/NetworkPolicy/provider capabilities, deny-by-default и отсутствие privilege expansion |
| Compatibility | migrations from scratch/upgrade, `N-1/N`, actual `S3b` binary на S4 data, consumer fence, drain, rollback/replay provider event без второго command/turn |

Все отказные тесты проверяют не только HTTP/status outcome, но и ноль побочных эффектов в БД, Kubernetes и Mattermost.

### 5.3 Матрица синтетических секретов

Используются только уникальные синтетические canary для классов OpenAI, GitHub, Mattermost, Kubernetes, PostgreSQL DSN и session/MCP token. Действующие значения из env, Secret или `.env` не читаются.

Каждый класс canary пропускается через все релевантные каналы, после чего проверяется отсутствие **значения**, а не имени ключа:

| Канал | Что проверяется |
| --- | --- |
| generated prompt/config | `AGENTS.md`, Codex `config.toml`, provider config, env allowlist и manifest не содержат canary |
| structured logs | обычные и error logs, поля и stack context не содержат canary |
| error/final/status | типизированная ошибка, final message, progress и status card не содержат canary |
| Mattermost payload | post/update/dialog/action response не содержит canary |
| audit | action/outcome/correlation и safe metadata не содержат canary |
| artifacts/archive | artifacts map, JSONL/stderr tail, session snapshot и archive metadata не содержат canary за пределами специально зашифрованного provider-файла, который не публикуется |
| rendered YAML | Deployment, Pod, Secret references и диагностический render не содержат canary; допустимы только имена Secret/key/env |

## 6. Эволюционная миграция и совместимость

### 6.1 Правила переключения

- Смена владельца схемы, владельца транспорта и поведения выполняется разными контрольными точками.
- `event_path=legacy|durable`, `delivery_path=legacy|outbox` и `transport_owner=legacy|typed` — отдельные взаимоисключающие переключатели. Одновременно активен ровно один владелец записи и один обработчик каждого эффекта.
- Переключатели и `consumer_epoch` хранятся серверно; промпт и локальная память процесса не являются источником истины.
- Каждый обработчик требует аренду/лидера и fencing epoch. Процесс со старым epoch не создаёт побочный эффект.
- Перед переключением или откатом приём приостанавливается, выполняемая и захваченная работа дренируется либо явно возвращается в `pending`, затем меняется epoch.
- `contract` выполняется позже, когда нет старых читателей/писателей и принят отдельный план отката.
- `N-1` ниже означает непосредственно предыдущую принятую контрольную точку, а не произвольный старый исполняемый файл. Для S4 это ровно `S3b bridge-ready`, финальный исполняемый checkpoint PR-3 с compatibility reader/dedup bridge; его source SHA и image digest фиксируются в отчёте приёмки PR-3. Нельзя перескочить этот checkpoint и затем требовать безопасный откат на более старую версию.

Перед каждым поведенческим переключением новый исполняемый файл сначала развёртывается с новым путём выключенным. После проверки он становится поддерживаемым `N-1` для следующей контрольной точки.

### 6.2 Состояния

| Состояние | Главное измерение | Внешнее поведение |
| --- | --- | --- |
| `S0 baseline` | текущий код | подтверждённые риски существуют; структурные PR запрещены |
| `S1 containment` | PR-0/PR-1/PR-2: публичная граница, тестовый контур, сдерживание очистки | полезная маршрутизация сохранена, опасные callback/cleanup закрыты по умолчанию |
| `S2 expand` | только добавочная схема для lease, idempotency, callback intent, receipt/outbox и переключателей | все пути остаются `legacy`; обработчики новых записей выключены |
| `S3a durable behavior` | PR-3: lease/fencing/CAS, единая completion transaction, callback intent и переключение delivery outbox | транспорт и routing DTO прежние; меняется только надёжность runtime/delivery |
| `S3b bridge-ready` | финальный checkpoint PR-3: S3a плюс включённый в legacy inbound path compatibility reader/dedup bridge для будущих S4 receipt/command/outcome | `event_path=legacy`; bridge только читает чужие S4 records при rollback/replay и не создаёт их на обычном S3 трафике |
| `S4 inbound durable` | PR-4: envelope, receipt и переключение команд `1 -> N` | тот же WebSocket/HTTP transport; один долговечный владелец записи и обработчик |
| `S5 structural seams` | последующие узкие repository/transport/runtime ports | данные и поведение S4 не меняются |
| `S6 contract` | удаление legacy paths и возможное физическое выделение | не входит в первые пять PR и требует отдельной приёмки |

### 6.3 Матрица `N-1/N` для S2/S3a/S3b/S4

| Состояние | `N-1` | `N` | Единственный владелец записи/обработчик | Развертывание/переключение | Откат/повтор |
| --- | --- | --- | --- | --- | --- |
| `S2 expand` | Читает/пишет старые таблицы, игнорирует добавочные таблицы и nullable-поля | Применяет только добавочную миграцию, но работает с переключателями `legacy` | legacy writer; новые обработчики выключены; миграции запускает один job/process | Сначала миграция, затем последовательное обновление до N; новых побочных эффектов нет | Остановить N и вернуть N-1; схема остаётся. Новые долговечные очереди пусты, повтор не нужен |
| `S3a durable behavior` | Предыдущий expand-capable исполняемый файл знает переключатели и новые записи и при чужом epoch не выполняет побочные эффекты обработчика | Поддерживает lease/CAS/completion/callback/outbox | после атомарного переключения только fenced N runtime/delivery consumer; transport writer прежний | Развернуть N с переключателями `legacy`, убедиться, что все экземпляры их понимают; остановить приём completion/callback, дренировать старые claims/direct deliveries, выдать новый epoch, включить `delivery_path=outbox` | Приостановить новые terminal/callback команды, оградить N, дождаться отсутствия claims; доставить либо пометить ambiguous outbox, вернуть `legacy`, затем N-1. Старые lease не принимаются, queued turns читаются N-1; локальных дублей/потерь нет |
| `S3b bridge-ready` | `S3a` image | Добавляет compatibility reader/dedup bridge в legacy inbound path; `event_path=legacy`, durable writer/consumer выключены | legacy inbound writer + read-only bridge; S3 delivery outbox consumer остаётся единственным | Развернуть exact PR-3 final binary с bridge, выполнить actual-binary test и зафиксировать SHA/digest; обычный трафик не пишет S4 receipt/command | Можно вернуть S3a только до первого S4 cutover. После появления S4 data поддерживаемым N-1 становится только этот S3b binary |
| `S4 inbound durable` | Только exact `S3b bridge-ready` из PR-3; читает receipt identity/hash/admission, immutable target snapshot, command state/resulting turn IDs и сохранённый idempotency outcome | Пишет одну receipt и `N` command intents, обработчик материализует существующие session/turn rows | после переключения только fenced N inbound writer и command consumer; transport всё ещё `legacy`; delivery outbox имеет отдельного единственного S3/S4 consumer | Сначала развернуть N с `event_path=legacy`; остановить лидера listener, дренировать provider callbacks и claimed commands, выдать новый epoch, включить `event_path=durable`, затем открыть приём | Выполнить протокол §6.4 и вернуть только exact S3b. Bridge для найденной receipt возвращает сохранённый outcome и никогда не вызывает legacy create; identity без receipt идёт в legacy path |

Для S3a/S3b/S4 откат запрещён как простая замена Pod. Если drain/fence/reconciliation не завершены, откат считается небезопасным и останавливается. Внешняя Mattermost доставка остаётся `at-least-once`: матрица гарантирует отсутствие второго **локального** intent, но не скрывает возможный внешний дубль после побочного эффекта до подтверждения.

Владелец транспорта в S2–S4 не меняется. `transport_owner=typed` переключается только в S5 отдельным PR после доказанного паритета, поэтому владение, транспорт и смена поведения не смешиваются.

### 6.4 Реализуемая граница rollback S4

Выбранный рекомендуемый контракт — добавить bridge в более ранний PR-3. Поддерживаемый `N-1` для S4 имеет точное логическое имя `S3b bridge-ready`: это финальный принятый source SHA и image digest PR-3, а не любой S3-compatible binary. До human gate раздела 10 это рекомендация; если владелец выберет вариант без bridge, S4 cutover с rollback на PR-3 запрещён и граница сужается согласно варианту B таблицы решений.

Bridge выполняется в существующем legacy inbound path до любого legacy create. По `(provider, provider_event_type, provider_event_id)` он читает:

- receipt identity, canonical payload hash и `admission=admitted|ignored|rejected|duplicate|deferred`;
- неизменяемый target snapshot;
- для каждой command состояние `pending|claimed|materialized|terminal|retry_wait|failed`, deterministic key и `resulting_turn_id`, если он уже назначен;
- сохранённый idempotency outcome с безопасными references на receipt/commands/turns.

Если receipt найдена и hash/scope/actor совпадают, bridge возвращает сохранённый admission/outcome и не вызывает legacy create, независимо от повторной доставки provider. Conflict по hash/scope/actor отклоняется с нулём side effects. Если receipt отсутствует, exact S3b обрабатывает событие обычным legacy writer. Bridge не становится вторым command consumer и не материализует `pending|claimed` commands.

Протокол отката S4 → S3b:

1. Закрыть новый provider intake и дождаться завершения уже принятых HTTP/WebSocket callbacks.
2. Fence S4 inbound writer и command consumer новым `consumer_epoch`; старый process после fence не пишет receipt, command, turn или outcome.
3. Claimed command lease либо завершается, либо явно возвращается в `pending` после истечения/отзыва fence. Затем S4 consumer материализует все `pending|retry_wait` commands в legacy-compatible turns и сохраняет `resulting_turn_id/outcome`. Если остаётся `pending|claimed|retry_wait` без безопасного результата, rollback останавливается.
4. Для delivery outbox: claimed lease S4 consumer завершается или освобождается; `pending|retry_wait` продолжит exact S3b delivery consumer; `ambiguous` остаётся только для reconciliation, `dead_letter` — для оператора. Удалять или объявлять доставленными эти записи при rollback запрещено.
5. Проверить отсутствие активных inbound claims, один terminal/queued turn на каждый materialized command, сохранённые receipt outcomes и единственного writer/consumer. Зафиксировать gate snapshot.
6. Развернуть exact S3b SHA/digest с `event_path=legacy`, новым epoch и закрытым intake; выполнить replay probes на S4 data. Только после зелёных probes открыть provider intake.

Обязательный compatibility test не подменяется mock/stub. CI собирает или берёт подписанный image exact `S3b bridge-ready` SHA, поднимает его на PostgreSQL schema/data, созданных фактическим S4 binary, и повторно доставляет provider event для каждой receipt/outcome ветки, включая multi-mention и lost provider ack. Проверка утверждает: receipt остаётся одна, новых command/turn нет, target snapshot не меняется, сохранённый outcome не теряется и возвращается повтору. Отдельно процесс воспроизводит claimed command и claimed delivery outbox перед fence, выполняет протокол drain/requeue и подтверждает отсутствие потери и второго локального intent.

## 7. Первые пять реализуемых PR

### PR-0. Сдерживание публичной границы Mattermost

**Результат:** разрешающий список Ingress; internal/MCP/metrics доступны только внутри кластера; action/dialog идут через кластерный Service; одноразовая серверная capability; проверенный actor; replay/expiry bindings; политика `response_url` из раздела 2.3.

**Автоматические проверки:** отрицательная матрица auth/actor/replay/SSRF с нулём side effects; снимки зарегистрированных и опубликованных маршрутов; render Ingress; существующие slash/action/dialog/GitHub contract tests.

**Ручная проверка:** легитимная slash -> action -> dialog цепочка работает; forged callback отклоняется; с внешнего адреса доступны только allowlisted paths, а internal/MCP/metrics недоступны.

**Ограничение:** PR не переносит transport packages, не меняет доменную модель и остаётся блокирующим условием любого следующего merge/deploy волны.

### PR-1. Обязательный тестовый контур и характеристические доказательства

**Результат:** создаются `make test-go`, `make test-go-postgres`, `make test-go-all`; обязательный режим PostgreSQL с `MATTERCODEX_*_TEST_DATABASE_DSN`; миграции с нуля и обновление копии; минимум два соединения; контур внесения отказов routing/session/callback/delivery; полная матрица синтетических секретов.

**Автоматические проверки:** все targets из раздела 5.1; явный тест, что required PostgreSQL mode не делает silent skip; проверка front matter, уникальности `id`, относительных ссылок и архитектурных импортов.

**Ручная проверка:** владелец видит отдельные исходы «герметичные тесты пройдены», «PostgreSQL пройден» либо явное «не запущен/упал»; зелёный общий статус невозможен без обязательного PostgreSQL контура для storage PR.

**Ограничение:** это новый результат PR, а не описание существующих команд. Прикладная schema/behavior не меняется.

### PR-2. Запрещающее по умолчанию сдерживание очистки сессий

**Результат:** автоматическое разрушительное удаление session PVC и session token Secret выключено; retention работает как inventory/dry-run для сессионных данных; повтор после ошибки квоты не вызывает destructive cleanup и возвращает типизированную capacity error. Предикат eligibility принимает факты PostgreSQL/архива, но до доказанного S3 archive любой `unknown` даёт отказ.

**Автоматические проверки:** отрицательная матрица `active|queued|approval|callback|no_archive|archive_failed|grace|unknown_db|unknown_s3`; отдельный quota test; повтор inventory; отсутствие удаления PVC/Secret и audit записи о разрешённом удалении.

**Ручная проверка:** в изолированной тестовой установке создать старый orphan test PVC и вызвать preview/quota scenario; ресурс остаётся, система показывает безопасную причину и не утверждает наличие архива.

**Ограничение:** полная DB+S3 eligibility и включение автоматической очистки — отдельная последующая задача. Labels и age никогда не дают права удаления.

### PR-3. Транзакционная надёжность session/turn/callback/delivery

**Результат:** добавочная схема и затем отдельное переключение для session guard, sequence, lease/heartbeat/fencing, status/version CAS complete/stop, completion reservation, единой PostgreSQL-транзакции terminal turn + session snapshot/`codex_session_id`/archive reference/checksum + session lease/fence + run/idempotency outcome + unique delivery intent, атомарного callback intent и local delivery outbox. Archive upload/version/checksum завершается до commit; upload failure оставляет turn nonterminal/retryable, а commit failure — только безопасный orphan для reconciliation/GC. В S2 также добавляются неактивные receipt/command schema и `event_path` gate. Финальный checkpoint `S3b bridge-ready` добавляет compatibility reader/dedup bridge в legacy inbound path без записи обычного S3 traffic. Работает один fenced consumer каждого типа; этапы S2/S3a/S3b обязательны.

**Автоматические проверки:** вся матрица разделов 4.3–4.5, включая fault injection до/после upload и каждой SQL-стадии, lost ack, stale complete, orphan reconciliation и next claim с новым snapshot; два PostgreSQL-соединения; crash/restart; старый reader на расширенной схеме; `N-1/N` S2/S3a; exact `S3b` binary bridge fixture; проверка запрета `replicas >= 2`.

**Ручная проверка:** два безопасных worker одновременно запрашивают одну session; выполняется один turn. Затем имитируются archive upload failure, DB commit failure после upload, потеря complete response и недоступность Mattermost: до commit нет terminal state; после commit новый snapshot/archive/run/outcome и один local outbox видимы вместе; следующий claim получает новый snapshot, доставка повторяется согласно `at-least-once` контракту. Отдельно exact S3b image читает подготовленную S4 receipt и возвращает outcome без второго turn.

**Ограничение:** транзакция между PostgreSQL и S3 и внешний Mattermost exactly-once не заявляются. Transport и repository ownership не переносятся.

### PR-4. Квитанция, версионированный конверт и детерминированное разветвление по целям

**Результат:** поля envelope из раздела 4.1; authenticated actor seam; installation/workspace/session scopes без полной #66; одна unique receipt на provider event; immutable target snapshot; `N` per-target commands/turns; typed admission outcome; idempotent consumers; этап S4.

**Автоматические проверки:** sequential/concurrent duplicate multi-mention; payload conflict; stable target order/discriminator; crash после receipt/command/consume; все correlation/causation/idempotency fields; deny без actor/scope/grant; `N-1/N` S4 и protocol §6.4 с фактическими exact S3b code/image, S4 data и provider redelivery.

**Ручная проверка:** одно сообщение с двумя упоминаниями создаёт одну receipt и по одному ходу каждой цели. Последовательный и конкурентный повтор не добавляет receipt/target/turn, а изменение списка ролей после первого приёма не меняет сохранённый fan-out. После fence/drain S4 заменяется exact S3b checkpoint; provider redelivery возвращает сохранённый outcome без второго command/turn.

**Ограничение:** WebSocket/HTTP transport остаётся на месте. Новые Organization/Membership/grants/quotas не создаются.

## 8. Очередь после первых пяти PR

После ручной приемки состава менеджер создаёт отдельные Issues, не смешивая типы результата:

1. **Волна 1: узкие repository ports и реестр владельцев миграций.** Consumer-owned interfaces, один `pgxpool`, один schema owner, запрет новых импортов `admin.Repository`; зависит от PR-1, PR-3 и PR-4.
2. **Волна 1: transport Mattermost и восстановление listener.** DTO -> versioned command, прежние URL/карточки, повторяемый `BotUserID`, диагностическое состояние #59; зависит от PR-0 и PR-4.
3. **Волна 1: runtime port и локальные пакеты `agent-runner`.** Capability matrix, typed admission, snapshot PodSpec/RBAC/NetworkPolicy, без privilege expansion; зависит от PR-2–PR-4.
4. **Надёжность #51: pre-start transient retry и безопасная HA.** Классы ошибок, retry/backoff, leader для всех loops и только затем `replicas >= 2`.
5. **Хранение: доказанная PostgreSQL+S3 eligibility и включение cleanup.** Archive checksum/reference, grace/hold/lease/audit, dry-run report и ручной enable gate; зависит от ADR-MC-006 и PR-2.
6. **Инфраструктура #58: воспроизводимые Mattermost bot settings.** Отдельный SRE-результат без переноса transport.
7. **Инструкции #60:** версии seed, checksum/drift и управляемое обновление без молчаливой перезаписи.
8. **Provider account #61:** безопасный alias, неизменяемая session affinity и `RuntimeRevision` по ADR-MC-004.
9. **Авторизация #66:** отдельные архитектурный, schema и поведенческие срезы Organization/Membership/grants/quotas после стабилизации envelope/admission seam.

Физическое выделение `runtime-controller` начинается только после задач 1–3. `interaction-gateway`, `control-plane`, `integration-gateway` и `automation-scheduler` не создаются в первых пяти PR.

## 9. Основные риски и ограничения

| Риск | Gate |
| --- | --- |
| Forged callback/SSRF | PR-0 до любого структурного merge/deploy |
| Тихо пропущенные PostgreSQL tests | PR-1 required target; storage PR без него не принимается |
| Потеря PVC/session state | PR-2 fail-closed; полное удаление только после DB+S3 proof |
| Два active turn/stale complete | PR-3 session guard + lease/fencing/CAS |
| Двойной callback prompt | PR-3 atomic intent + deterministic consumer key |
| Потеря/дубль результата Mattermost | local unique outbox + `at-least-once`; ambiguous/manual reconciliation честно видимы |
| Multi-mention потерян или дублирован | PR-4 `1 receipt -> N commands/turns` и immutable target snapshot |
| Несовместимый rollback | последовательные checkpoint S2/S3a/S3b/S4, exact `N-1`, fence/drain/replay |
| Расширение полномочий | снимки direct/managed matrix, deny-by-default, отдельный typed admission |
| Случайное включение #66 | только scope/actor/admission seam; продуктовые таблицы и политики остаются отдельной задачей |

## 10. Открытые решения владельца

Ни одна рекомендация таблицы не является решением владельца. До human gate выбранные варианты не считаются принятыми, а будущие acceptance gates — выполненными.

| Решение | Вариант A | Вариант B | Аварийный/переходный вариант | Рекомендация | Последствия для PR-0…PR-4 | Security/operations | Migration/rollback | Будущая #66 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Механизм action/dialog callback | Кластерный callback + одноразовая opaque capability с bindings из §2.2 | Доверенный Mattermost proxy/plugin подписывает callback; `bot-service` проверяет signature, nonce, expiry, actor и те же bindings | Выключить action/dialog routes и оставить slash/текстовый fallback; это containment, не завершение PR-0 | **A:** минимальный механизм под контролем MatterCodex, без новой зависимости | A — PR-0 добавляет capability store/consume, PR-1 тесты; PR-2…PR-4 без перестановки.<br>B — PR-0 включает plugin/proxy contract/deploy и может разделиться на два PR; остальные ждут его.<br>E — PR-0 может временно закрыть exposure, но функциональный PR-0 остаётся незавершённым и блокирует PR-1…PR-4 deploy | A — replay/actor контролирует сервер, нужна очистка expired records.<br>B — добавляется trust/key rotation и эксплуатация proxy/plugin.<br>E — минимальная поверхность, но потеря UI-сценария | A — additive table, простой rollback с отключением route.<br>B — двухкомпонентный rollback и совместимость ключей/signature versions.<br>E — обратное включение только после A/B | A/B сохраняют verified actor seam без Organization tables; grants развивает #66.<br>E не решает модель actor/grant |
| Retention containment | Выключить destructive session PVC/Secret и quota cleanup; оставить inventory/dry-run до PostgreSQL+S3 eligibility | Реализовать полный fail-closed DB+S3 predicate, archive checksum/reference, grace/hold/lease/audit и ручной enable уже в PR-2 | Полностью остановить retention/quota eviction loops и возвращать capacity error до отдельного storage PR | **A:** минимально прекращает потерю данных и сохраняет PR-2 ограниченным | A — PR-2 остаётся containment; полное включение после PR-4 отдельным PR.<br>B — PR-2 поглощает часть storage/ADR-MC-006 и требует PR-1 PostgreSQL harness; PR-3/PR-4 сдвигаются.<br>E — малый hotfix до PR-2, но не заменяет его inventory/diagnostics | A — рост storage контролируется inventory/alerts.<br>B — меньше ручной работы, но выше риск ошибки destructive predicate.<br>E — быстрый рост PVC и раннее исчерпание quota | A/E — rollback прост: deletion остаётся off; миграций удаления нет.<br>B — enable только после dry-run comparison и rollback в off; удалённые данные не восстановить без backup | Не зависит от Organization schema; будущая policy может добавить organization scope. B преждевременно связывает retention с ещё не принятой policy #66 |
| Прямой `cluster-admin` | Заморозить новые назначения; оставить только уже сконфигурированные профили, добавить явный server-side profile grant/admission и audit до managed migration | Удалить прямой `cluster-admin` из agent Pod; Kubernetes writes выполнять только через managed MCP с узкими Role/ClusterRole и approval | Отключить все cluster-admin profiles/`ClusterRoleBinding`; административные операции вручную выполняет владелец вне агента | **A** для волны 1 как наименьший scope; **B** — целевое состояние отдельного security/runtime PR | A — PR-1 фиксирует snapshot/negative tests; PR-0…PR-4 не расширяют binding; runtime port после PR-4 готовит B.<br>B — нужен integration gateway/RBAC/approval раньше runtime port, что расширяет и переставляет PR-3/PR-4.<br>E — отдельный containment PR до PR-0 возможен, workflow admin временно недоступен | A — сохраняется высокий blast radius, требуются human gate и аудит; prompt не является контролем.<br>B — credential остаётся gateway, least privilege/approval.<br>E — наименьший риск платформы, наибольшая операционная ручная нагрузка | A — rollback к прежнему PodSpec только с тем же frozen subject; новые assignments запрещены.<br>B — dual-run запрещён; cutover после parity, rollback возвращает только frozen A profile.<br>E — возврат требует отдельного owner OK | A использует временный installation/profile grant без полной schema.<br>B естественно подключается к IntegrationGrant #66.<br>E откладывает policy, но не определяет её |
| Неоднозначная доставка Mattermost | Для `ambiguous` разрешить автоматический retry по принятой risk policy, признавая возможный внешний дубль | Карантинировать `ambiguous`; автоматически повторять только доказанно неотправленные `pending\|retry_wait`, а ambiguous решать readback/manual reconciliation | Остановить delivery consumer, сохранить outbox и временно публиковать оператором после сверки | **B:** честная и безопасная семантика до доказанного provider readback | A — PR-3 реализует policy switch и duplicate warning; PR-4 без перестановки.<br>B — PR-3 реализует `ambiguous` queue/operator path; PR-4 без перестановки.<br>E — допустим incident mode PR-3, не acceptance default | A — меньше задержек, выше вероятность duplicate.<br>B — нет автоматического duplicate из unknown window, но возможна задержка/ручная работа.<br>E — накапливается очередь и нужна наблюдаемость | Во всех вариантах local unique intent сохраняется.<br>A↔B меняется только retry policy; downgrade не удаляет state.<br>E возобновляет consumer с теми же leases/fence | Не зависит от #66; будущая policy может задаваться по organization/channel/risk class |
| Граница rollback S4 | PR-3 завершается exact checkpoint `S3b bridge-ready` с compatibility reader/dedup bridge; S4 поддерживает rollback только на этот SHA/digest | Не добавлять bridge в PR-3; внутри PR-4 сначала выпустить отдельный `S4-expand-reader` binary с S4 reader/dedup и `event_path=legacy`, и поддерживать rollback S4 только на него | Закрыть intake и выполнять forward-fix на S4; downgrade запрещён до reconciliation | **A:** bridge раньше cutover даёт проверяемый непосредственно предыдущий N-1 | A — расширяет PR-3 bridge и actual-binary test; PR-4 cutover после его gate.<br>B — PR-3 меньше, но PR-4 делится на reader checkpoint и durable cutover; порядок PR-0…PR-3 прежний, PR-4 длиннее.<br>E — не меняет scope, но не даёт rollback acceptance | A/B требуют хранения exact image, epoch/fence/drain runbook и тест provider redelivery.<br>E требует операционного окна и способности безопасно держать intake закрытым | A — только S4→exact S3b по §6.4.<br>B — только S4 durable→exact S4-expand-reader; PR-3 и baseline после появления S4 data не поддерживаются.<br>E — только roll-forward, pending/claimed state сохраняется | Не зависит от #66, но receipt actor/scope проверяются существующим installation seam; будущая migration не может менять provider identity |
| Scope и очередность #66 | В PR-0/PR-4 оставить только installation/workspace/session scope, verified actor и deny-by-default admission seam; Organization/Membership/grants/quotas делать отдельными slices после PR-4 | До PR-4 добавить минимальные Organization/Membership/IntegrationGrant schema и policy evaluator, затем строить receipt/admission на них | Сохранить single-install static organization reference и запретить новые external/admin capabilities; PR-4 допускает только явно allowlisted текущие сценарии | **A:** стабилизировать envelope/idempotency до продуктовой авторизации | A — PR-0 seam, PR-1 tests, PR-4 поля envelope; #66 следует после первых пяти PR.<br>B — новый architecture/schema/behavior PR между PR-1 и PR-4, PR-4 зависит от него; выше scope волны.<br>E — PR-4 ограничен текущей установкой, расширение capability блокируется до #66 | A — fail-closed seam без полной policy, меньше migration surface.<br>B — раньше появляется полноценный grant, но возрастает риск смешать auth, transport и reliability.<br>E — безопасное ограничение, но нет пользовательского управления правами/квотами | A/E — additive future migration, stable installation reference remap.<br>B — нужны backfill membership/grants и совместимый rollback policy/schema до S4 | A — #66 отдельная последовательность architecture→schema→behavior после PR-4.<br>B — часть #66 входит в волну и меняет её порядок.<br>E — вся #66 позже, новые capabilities запрещены |

## 11. Критерии принятия и цель третьего полного рецензирования

Предложение можно передавать к созданию implementation Issues, когда владелец:

- выбрал решения раздела 10;
- принял PR-0 как самостоятельный блокирующий результат;
- согласовал первые пять PR и отдельную ручную проверку каждого;
- подтвердил, что structural ports начинаются только после PR-4;
- принял внешний `at-least-once` контракт Mattermost и ограничение ambiguous crash window;
- подтвердил поддерживаемый диапазон `N-1/N` и запрет опасного отката без drain/fence.

Точная цель третьего полного прохода рецензента:

1. повторно проверить все факты по указанным code/SQL ссылкам;
2. убедиться, что PR-0 закрывает route exposure, actor/replay и SSRF с нулём side effects;
3. проверить cardinality `1 receipt -> N commands/turns`, envelope и authorization seam;
4. проверить session serialization, lease/heartbeat/fencing, complete/stop CAS, единую completion transaction после archive upload и atomic callback intent;
5. проверить честную семантику Mattermost delivery и crash windows;
6. доказать `N-1/N` для S2/S3a/S3b/S4, actual-binary compatibility bridge, single writer/consumer, feature gates, drain и rollback/replay;
7. проверить retention fail-closed, PostgreSQL required harness, secret canaries и privilege matrix;
8. подтвердить одного владельца каждой таблицы/мутации и отсутствие split ownership external IDs;
9. убедиться, что первые пять PR независимо реализуемы и проверяемы, а #66 и структурные переносы не попали в их скрытый объём.

PostgreSQL integration, rendered Kubernetes и live-проверки не считаются выполненными данным документационным PR. Их наличие здесь — критерии будущих implementation PR, а не заявление об уже полученном доказательстве.
