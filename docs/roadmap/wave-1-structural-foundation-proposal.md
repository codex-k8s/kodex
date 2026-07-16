---
id: ROAD-MC-006
title: Архитектурное предложение волны 1 «Структурный фундамент»
type: roadmap
status: proposed
owner: architect
version: 0.2.0
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
4. `PR-3` вводит сериализацию сессии, lease/heartbeat/fencing, CAS завершения и остановки, атомарный intent обратного вызова и локальный исходящий журнал результата.
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

Матрица фиксирует имена, но не значения переменных и секретов.

| Возможность | Текущий прямой путь | Целевая граница до runtime port | Обязательный запрет |
| --- | --- | --- | --- |
| Mattermost от `bot-service` | `MATTERCODEX_MATTERMOST_BOT_TOKEN`, `MATTERCODEX_MATTERMOST_SLASH_TOKEN` через Secret env | edge-адаптер с минимальным bot scope; callback actor проверяется сервером | token не попадает в runner, prompt, audit или payload |
| PostgreSQL `bot-service` | `MATTERCODEX_DATABASE_DSN` через Secret env | единственный владелец схемы и узкие порты | DSN не передаётся в доменные DTO и не логируется |
| OpenAI Codex runner | `auth.json` как read-only Secret mount | прямой режим только для выбранной provider account; ссылка и ревизия входят в admission | содержимое не попадает в generated config/log/result/archive вне необходимого provider-файла |
| GitHub runner | Secret mount с token/username/email и env клиента | прямой режим только для явно разрешённой роли; опасные изменения позднее переходят в managed MCP | отсутствие назначения означает отсутствие mount/env/network capability |
| Kubernetes read-only | отдельный namespaced ServiceAccount/RBAC | прямой режим только для чтения, снимок разрешённых verbs/resources | deny-by-default; ServiceAccount token не монтируется без capability |
| Kubernetes `cluster-admin` | отдельный ServiceAccount с `ClusterRoleBinding` | до полной #66 — только существующий явно выданный серверный grant и типизированный admission; новые назначения запрещены по умолчанию | PR не расширяет subjects, verbs, scope или automount |
| MCP | один session bearer сейчас обслуживает runner API и все tools | разные short-lived credentials с audience `runner-api` и `mcp`; каждый side-effecting tool проверяет server-side grant | prompt/instructions не разрешают инструмент |
| Сеть provider/MCP | фактически определяется Pod и кластером | capability matrix задаёт допустимые назначения/порты и проверяемую NetworkPolicy | неизвестная capability не открывает egress |

До переноса runtime port обязательны снимки env names, Secret mounts, PodSpec, ServiceAccount/RBAC, NetworkPolicy и серверных grants для каждого профиля. Изменение границы не может расширять полномочия. `replicas >= 2` остаются запрещены до раздела 4 и зелёной матрицы `N-1/N`.

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
| `admission` | типизированный результат `admitted|ignored|rejected|duplicate|deferred` и безопасный reason code |

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
| Session | FIFO, two-worker claim, lost response, lease reclaim, stale complete, restart, stop/complete CAS |
| Callback | concurrent return, crash/restart, deterministic consumer key, один resulting turn |
| Delivery | local unique outbox, retry/backoff/dead-letter, crash до/после side effect, ambiguous reconciliation |
| Retention | active, queued, approval, callback, no archive, archive failed, grace, unknown PostgreSQL, unknown S3 — удаление запрещено |
| Privileges | снимки env names/mounts/SA/RBAC/NetworkPolicy/provider capabilities, deny-by-default и отсутствие privilege expansion |
| Compatibility | migrations from scratch/upgrade, `N-1/N`, consumer fence, drain, rollback/replay |

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
- `N-1` ниже означает непосредственно предыдущую принятую контрольную точку, а не произвольный старый исполняемый файл. Нельзя перескочить expand и затем требовать безопасный откат на версию, которая не понимает переключатель и новые долговечные записи.

Перед каждым поведенческим переключением новый исполняемый файл сначала развёртывается с новым путём выключенным. После проверки он становится поддерживаемым `N-1` для следующей контрольной точки.

### 6.2 Состояния

| Состояние | Главное измерение | Внешнее поведение |
| --- | --- | --- |
| `S0 baseline` | текущий код | подтверждённые риски существуют; структурные PR запрещены |
| `S1 containment` | PR-0/PR-1/PR-2: публичная граница, тестовый контур, сдерживание очистки | полезная маршрутизация сохранена, опасные callback/cleanup закрыты по умолчанию |
| `S2 expand` | только добавочная схема для lease, idempotency, callback intent, receipt/outbox и переключателей | все пути остаются `legacy`; обработчики новых записей выключены |
| `S3 durable behavior` | PR-3: lease/fencing/CAS, callback intent и переключение delivery outbox | транспорт и routing DTO прежние; меняется только надёжность runtime/delivery |
| `S4 inbound durable` | PR-4: envelope, receipt и переключение команд `1 -> N` | тот же WebSocket/HTTP transport; один долговечный владелец записи и обработчик |
| `S5 structural seams` | последующие узкие repository/transport/runtime ports | данные и поведение S4 не меняются |
| `S6 contract` | удаление legacy paths и возможное физическое выделение | не входит в первые пять PR и требует отдельной приёмки |

### 6.3 Матрица `N-1/N` для S2/S3/S4

| Состояние | `N-1` | `N` | Единственный владелец записи/обработчик | Развертывание/переключение | Откат/повтор |
| --- | --- | --- | --- | --- | --- |
| `S2 expand` | Читает/пишет старые таблицы, игнорирует добавочные таблицы и nullable-поля | Применяет только добавочную миграцию, но работает с переключателями `legacy` | legacy writer; новые обработчики выключены; миграции запускает один job/process | Сначала миграция, затем последовательное обновление до N; новых побочных эффектов нет | Остановить N и вернуть N-1; схема остаётся. Новые долговечные очереди пусты, повтор не нужен |
| `S3 durable behavior` | Предыдущий expand-capable исполняемый файл знает переключатели и новые записи и при чужом epoch не выполняет побочные эффекты обработчика | Поддерживает lease/CAS/callback/outbox | после атомарного переключения только fenced N runtime/delivery consumer; transport writer прежний | Развернуть N с переключателями `legacy`, убедиться, что все экземпляры их понимают; остановить приём completion/callback, дренировать старые claims/direct deliveries, выдать новый epoch, включить `delivery_path=outbox` | Приостановить новые terminal/callback команды, оградить N, дождаться отсутствия claims; доставить либо пометить ambiguous outbox, вернуть `legacy`, затем N-1. Старые lease не принимаются, queued turns читаются N-1; локальных дублей/потерь нет |
| `S4 inbound durable` | Предыдущий S3-compatible исполняемый файл уже понимает receipt/command gate, но новый writer выключен до переключения | Пишет одну receipt и `N` command intents, обработчик материализует существующие session/turn rows | после переключения только fenced N inbound writer и runtime consumer; transport всё ещё `legacy` | Сначала развернуть N с `event_path=legacy`; остановить лидера listener, дренировать provider callbacks и claimed commands, выдать новый epoch, включить `event_path=durable`, затем открыть приём | Закрыть приём, оградить writer/consumer, материализовать все принятые commands в доступные legacy turns, убедиться в отсутствии unclaimed receipt/outbox, вернуть переключатель и N-1. Повтор принимает только identities без receipt; уже принятые events возвращают сохранённый outcome |

Для S3/S4 откат запрещён как простая замена Pod. Если drain/fence/reconciliation не завершены, откат считается небезопасным и останавливается. Внешняя Mattermost доставка остаётся `at-least-once`: матрица гарантирует отсутствие второго **локального** intent, но не скрывает возможный внешний дубль после побочного эффекта до подтверждения.

Владелец транспорта в S2–S4 не меняется. `transport_owner=typed` переключается только в S5 отдельным PR после доказанного паритета, поэтому владение, транспорт и смена поведения не смешиваются.

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

**Результат:** добавочная схема и затем отдельное переключение для session guard, sequence, lease/heartbeat/fencing, status/version CAS complete/stop, атомарного callback intent и local delivery outbox. В S2 также добавляются неактивные receipt/command schema и `event_path` gate без записи входных событий: это совместимый фундамент отката PR-4. Работает один fenced consumer; этапы S2/S3 обязательны.

**Автоматические проверки:** вся матрица разделов 4.3–4.5; два PostgreSQL-соединения; crash/restart fault injection; старый reader на расширенной схеме; `N-1/N` S2/S3; проверка запрета `replicas >= 2`.

**Ручная проверка:** два безопасных worker одновременно запрашивают одну session; выполняется один turn. Затем имитируется потеря complete response и недоступность Mattermost: terminal result и один local outbox сохраняются, после восстановления доставка повторяется согласно `at-least-once` контракту.

**Ограничение:** внешний Mattermost exactly-once не заявляется. Transport и repository ownership не переносятся.

### PR-4. Квитанция, версионированный конверт и детерминированное разветвление по целям

**Результат:** поля envelope из раздела 4.1; authenticated actor seam; installation/workspace/session scopes без полной #66; одна unique receipt на provider event; immutable target snapshot; `N` per-target commands/turns; typed admission outcome; idempotent consumers; этап S4.

**Автоматические проверки:** sequential/concurrent duplicate multi-mention; payload conflict; stable target order/discriminator; crash после receipt/command/consume; все correlation/causation/idempotency fields; deny без actor/scope/grant; `N-1/N` S4 и rollback/replay.

**Ручная проверка:** одно сообщение с двумя упоминаниями создаёт одну receipt и по одному ходу каждой цели. Последовательный и конкурентный повтор не добавляет receipt/target/turn, а изменение списка ролей после первого приёма не меняет сохранённый fan-out.

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
| Несовместимый rollback | последовательные checkpoint S2/S3/S4, gate-aware `N-1`, fence/drain/replay |
| Расширение полномочий | снимки direct/managed matrix, deny-by-default, отдельный typed admission |
| Случайное включение #66 | только scope/actor/admission seam; продуктовые таблицы и политики остаются отдельной задачей |

## 10. Открытые решения владельца

1. **Механизм action/dialog authentication.** Рекомендуется кластерный callback плюс одноразовая server-side capability. Альтернатива — доверенный proxy/plugin с подписью и теми же bindings. Временное отключение action/dialog допустимо только как аварийный containment, не как завершённый PR-0.
2. **Retention containment.** Рекомендуется выключить автоматическое удаление session PVC/Secret и quota cleanup до DB+S3 eligibility. Альтернатива — реализовать полный fail-closed predicate уже в PR-2, но это увеличит его объём.
3. **`cluster-admin`.** Рекомендуется запретить новые назначения и потребовать явный server-side grant для уже существующих профилей до полной #66. Фактические назначения live-среды в этом документе не определены.
4. **Mattermost ambiguous delivery.** Рекомендуется принять `at-least-once` и ручную reconciliation до доказанного readback. Альтернатива — не включать автоматический retry ambiguous state до отдельного provider contract test.
5. **Поддерживаемый rollback.** Рекомендуется считать поддерживаемым только непосредственно предыдущий принятый checkpoint и запретить пропуск S2. Откат на исходный baseline после S3/S4 без compatibility bridge не обещается.
6. **Область #66.** Рекомендуется оставить в волне только обязательные scope/actor/admission поля и deny-by-default seam, без Organization/Membership/grants/quotas schema.

## 11. Критерии принятия и цель второго полного рецензирования

Предложение можно передавать к созданию implementation Issues, когда владелец:

- выбрал решения раздела 10;
- принял PR-0 как самостоятельный блокирующий результат;
- согласовал первые пять PR и отдельную ручную проверку каждого;
- подтвердил, что structural ports начинаются только после PR-4;
- принял внешний `at-least-once` контракт Mattermost и ограничение ambiguous crash window;
- подтвердил поддерживаемый диапазон `N-1/N` и запрет опасного отката без drain/fence.

Точная цель второго полного прохода рецензента:

1. повторно проверить все факты по указанным code/SQL ссылкам;
2. убедиться, что PR-0 закрывает route exposure, actor/replay и SSRF с нулём side effects;
3. проверить cardinality `1 receipt -> N commands/turns`, envelope и authorization seam;
4. проверить session serialization, lease/heartbeat/fencing, complete/stop CAS и atomic callback intent;
5. проверить честную семантику Mattermost delivery и crash windows;
6. доказать `N-1/N` для S2/S3/S4, single writer/consumer, feature gates, drain и rollback/replay;
7. проверить retention fail-closed, PostgreSQL required harness, secret canaries и privilege matrix;
8. подтвердить одного владельца каждой таблицы/мутации и отсутствие split ownership external IDs;
9. убедиться, что первые пять PR независимо реализуемы и проверяемы, а #66 и структурные переносы не попали в их скрытый объём.

PostgreSQL integration, rendered Kubernetes и live-проверки не считаются выполненными данным документационным PR. Их наличие здесь — критерии будущих implementation PR, а не заявление об уже полученном доказательстве.
