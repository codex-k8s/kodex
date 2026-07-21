---
id: ROAD-MC-006
title: Архитектурное предложение волны 1 «Структурный фундамент»
type: roadmap
status: proposed
owner: architect
version: 0.6.4
updated: 2026-07-21
---

# Архитектурное предложение волны 1 «Структурный фундамент»

## Статус и границы доказательств

Документ является исправленным предложением по [задаче GitHub #65](https://github.com/codex-k8s/matter-codex/issues/65). Фактические выводы относятся к `main` в коммите `719974b8de8d28b3404f161d70994fed45a79cf1`. Предложение уточняет путь реализации [ADR-MC-001](../decisions/0001-evolutionary-service-boundaries.md), [целевых границ сервисов](../architecture/service-boundaries.md) и [доменных границ](../architecture/domain-map.md), но не заменяет их. На первом ручном шлюзе 2026-07-17 владелец предварительно принял пакет `1A/2A/3A/4B/5A/6A`; раздел 10 фиксирует его как единственный активный выбор. Общий статус документа остаётся `proposed` до полного повторного прохода рецензента и финального OK владельца.

Описанные ниже шлюзы относятся к приемке самого архитектурного предложения. Реализационные PR после его принятия используют текущую delivery policy; до 27 июля 2026 года это `prototype-accelerated` из [ROAD-MC-007](accelerated-mvp-prototype.md). Это меняет число проходов и owner gates, но не порядок PR-0…PR-4 и их обязательные технические контракты.

В документ входят:

- карта текущего `bot-service`, `agent-runner`, таблиц, миграций и границ доверия;
- обязательное сдерживание подтверждённых рисков до структурных переносов;
- контракты квитанции входного события, разветвления команд, аренды хода, завершения, обратного вызова и исходящего журнала;
- совместимые состояния `expand -> cutover -> contract` и матрица `N-1/N`;
- первые пять независимо реализуемых PR и очередь последующих задач;
- связь с задачами [#51](https://github.com/codex-k8s/matter-codex/issues/51), [#58](https://github.com/codex-k8s/matter-codex/issues/58), [#59](https://github.com/codex-k8s/matter-codex/issues/59), [#60](https://github.com/codex-k8s/matter-codex/issues/60), [#61](https://github.com/codex-k8s/matter-codex/issues/61) и [#66](https://github.com/codex-k8s/matter-codex/issues/66).

В этот результат не входят прикладной код, SQL-миграции, Kubernetes-манифесты, Ingress, развертывание и изменения действующей среды. Кластер, рабочая PostgreSQL и S3 не инспектировались. Наличие архивов, число реплик и фактические значения конфигурации не утверждаются. Значения секретов не читались.

Отсутствие проверки учётных данных у обратных вызовов `action`/`dialog` — не сохраняемый контракт `P2`, а подтверждённая уязвимость текущей публичной границы. Любая реализация структурной части волны блокируется до принятого и развёрнутого PR-0.

### Активные эксплуатационные шлюзы реализации PR #72

- MCP POST имеет transport-level предел полного JSON envelope до SDK/auth/admission и полный server-owned read deadline; ручная приёмка включает oversized `Content-Length`, oversized/slow chunked, unauthenticated и exact-boundary control с нулём доменных побочных эффектов на отказе.
- Делегирование ограничивает UTF-8 bytes/runes всех пользовательских metadata до storage. Title нормализуется как непрозрачное недоверенное значение и безопасно рендерится во всех четырёх Mattermost/prompt paths. Возврат ordinary и `cluster-admin` роли одной транзакцией сохраняет callback transition, точный immutable plan и manifest обеих обязательных destinations; durable lease/reconcile delivery возвращает ошибку до полного подтверждения и не дублирует частичный успех после timeout, перезапуска или потери DB mark. Mattermost 11.6 projection допускает только точный client payload и server-owned `from_bot: "true"`.
- `make test-go-postgres` по умолчанию владеет временными `PGDATA`/Unix socket и создаёт случайную database только после атомарного one-shot bootstrap proof. Погашение proof до `CREATE` неизменно резервирует точное имя target, хеш marker и OID владельца; наблюдаемый OID database фиксируется только как согласованность ledger и не доказывает происхождение. При ошибке состояния `reserved/database_oid=0` и `created` до exact applied marker сохраняют target и ledger без adoption или `DROP`. После marker destructive cleanup разрешён только зарегистрированному generated private cluster с точными private `PGDATA`/Unix socket, `system_identifier`, offline registry и in-process authority; явный внешний/shared endpoint всегда передаёт очистку владельцу. Явный внешний CI bootstrap требует точный разрешающий список endpoint и тот же привязанный к серверу proof без статического fallback. Запрещающий по умолчанию допуск отклоняет промышленный, канонический или резервный target, просроченный, повторный или неверный proof и повторно сверяет точные database/endpoint/server/marker перед DDL оснастки. Replacement или несовпадающий registry сохраняется без `DROP`. Затем lifecycle сериализует глобальный для database `CREATE EXTENSION vector`, устанавливает extension один раз в `public`, использует `search_path=<isolated_schema>,public` и удаляет только проверенную пакетную схему. Приёмка повторяет generated PostgreSQL 15 и 16 без внешнего `GOFLAGS` и проверяет единственный extension и доступный `public.vector`.
- `000025`, `000026`, `000027`, `000028`, `000029`, `000030` и `000032` являются forward-only. Каждый `down` fail-closed, а rollback приложения разрешён только на заранее проверенный exact N-1 reader/runtime при отключённых callback route и integration MCP/action/worker, без удаления scoped fence/outbox/manifest/integration receipts/triggers и без отката мер PR #74/#75. Версия `000031` зарезервирована независимой волной runtime revisions и не входит в этот результат.
- Capability cleanup охватывает `pending`, `unused`, `consumed` и `revoked` только при строгом `expires_at < now - retention`. Ручная приёмка доказывает удаление stale `pending`, сохранение `pending` внутри grace и ровно на границе, а также пригодность действующей `unused` capability.

Остаточные риски: checks GitHub могут отсутствовать и не считаются успешным CI; retry callback запускается повторным MCP-вызовом; callback delivery rows и manifests не имеют временной очистки в MVP, а внешний ключ блокирует удаление delegation; test bootstrap требует право `CREATE DATABASE` только после проверенного one-shot proof. Integration recording worker существует только для PostgreSQL-квитанции Issue #93 и не является автоматическим Codex resume. Любой target до exact applied marker и любой существующий target на внешнем/shared endpoint намеренно сохраняются с явной передачей ручной очистки владельцу: catalog identity не доказывает происхождение, wildcard/prefix cleanup запрещён. Физический rollback миграций `000025`–`000030` и `000032` не поддерживается.

## Краткий вывод

Первые пять результатов волны должны идти в таком порядке:

1. `PR-0` по обязательным решениям 1A, 3A и 6A закрывает публичную границу Mattermost: узкий Ingress, кластерный обратный вызов `action`/`dialog`, непрозрачная одноразовая серверная возможность доступа, сохранённая как хеш, проверенный субъект, отдельный допуск, защита от повторного воспроизведения и SSRF; одновременно новые назначения `cluster-admin` замораживаются, а уже настроенные профили требуют явно заданных на сервере права профиля и допуска с аудитом.
2. `PR-1` создаёт обязательный герметичный и PostgreSQL-тестовый контур и полную матрицу характеристических проверок и проверок синтетических секретов.
3. `PR-2` по обязательному решению 2A выключает разрушительное удаление сессионных PVC/Secret и очистку по квоте; остаются инвентаризация, предварительный просмотр и типизированная ошибка ёмкости. Полный предикат допустимости по PostgreSQL и S3 и включение очистки идут отдельным результатом после PR-4.
4. `PR-3` по обязательным решениям 4B и 5A вводит сериализацию сессии, аренду, пульс и ограждение устаревшего исполнителя, единую PostgreSQL-транзакцию завершения после загрузки архива, атомарное намерение обратного вызова, локальный исходящий журнал результата с карантином `ambiguous` и точную контрольную точку `S3b bridge-ready`.
5. `PR-4` по обязательным решениям 5A и 6A вводит версионированный конверт, одну квитанцию события поставщика, детерминированное разветвление в `N` команд/ходов и честную доставку Mattermost с семантикой `at-least-once`; в область входят только установка, рабочая область, сессия, проверенный субъект и точка сопряжения допуска с отказом по умолчанию, но не схема #66.

Только после этих пяти PR допускаются узкие репозиторные порты, перенос транспорта и порт среды выполнения. `replicas >= 2` запрещены до успешного прохождения шлюзов квитанции, исходящего журнала, идемпотентности, выбора лидера, аренды, ограждения устаревшего исполнителя и управления одиночными циклами.

`PR-1` не реализует восстановление из #51: обязательная зелёная характеристическая проверка фиксирует фактический долг базовой версии — при недоступности обязательного MatterCodex MCP до первых Codex-событий ход завершается как `failed`. Это известный риск #51, а не целевой контракт и не приемлемое целевое поведение. Отдельная задача #51 после первых пяти PR использует созданную оснастку, меняет ожидаемый исход на `queued` или `retry_wait`, проверяет выполнение после восстановления зависимости и отсутствие необратимого `failed`. Физическое разделение очереди и `runtime-controller` допускается только после задач 1–4 раздела 8, включая принятую реализацию #51.

Пакет `1A/2A/3A/4B/5A/6A` является обязательным контрактом первых пяти PR, а не набором рекомендаций. Подписывающий прокси или плагин, полный предикат хранения внутри PR-2, расширение либо преждевременная управляемая миграция `cluster-admin`, автоматический повтор `ambiguous`, основная стратегия `S4-expand-reader` и минимальная схема #66 до PR-4 остаются рассмотренными, но неактивными альтернативами. Их применение требует нового явного решения владельца и пересмотра затронутых границ.

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
           внутренний API и сессионный MCP
```

| Компонент | Фактическая ответственность | Свидетельство |
| --- | --- | --- |
| Корень сборки `bot-service` | Открывает PostgreSQL, при включённом режиме запускает миграции, собирает сервисы и запускает HTTP, обработчик WebSocket и циклы восстановления и хранения. | [`cmd/bot-service/main.go`](../../services/external/bot-service/cmd/bot-service/main.go), [`internal/app/app.go`](../../services/external/bot-service/internal/app/app.go) |
| HTTP-маршрутизатор | В одном `ServeMux` регистрирует проверки работоспособности и готовности, метрики, `slash`/`action`/`dialog`, вебхук GitHub, внутренний API сессий и MCP. | [`router.go`](../../services/external/bot-service/internal/transport/http/router.go), [`mcp.go`](../../services/external/bot-service/internal/transport/http/mcp.go) |
| Маршрутизация Mattermost | Фильтрует сообщения, выбирает одну или несколько целевых ролей и для каждой цели отдельно вызывает постановку хода. | [`chat_runtime.go`](../../services/external/bot-service/internal/domain/service/chat_runtime.go), [`chat_runtime_test.go`](../../services/external/bot-service/internal/domain/service/chat_runtime_test.go) |
| Сессии и ходы | Авторизует `agent-runner`, выдаёт ход, завершает его, сохраняет снимок, меняет карточку и напрямую публикует результат. | [`agent_session_service.go`](../../services/external/bot-service/internal/domain/service/agent_session_service.go), [`agent_session_service_test.go`](../../services/external/bot-service/internal/domain/service/agent_session_service_test.go) |
| Делегирование | Создаёт целевое обсуждение и ход, а при возврате сначала создаёт или меняет ход обратного вызова и только затем выполняет CAS записи делегирования. | [`agent_delegation_service.go`](../../services/external/bot-service/internal/domain/service/agent_delegation_service.go), [`agent_delegations__set_callback.sql`](../../services/external/bot-service/internal/repository/postgres/admin/sql/agent_delegations__set_callback.sql) |
| Kubernetes-адаптер | Создаёт и удаляет Pod/PVC/Secret/Job/ConfigMap, выбирает ServiceAccount, монтирует учётные данные и выполняет хранение. | [`runner.go`](../../services/external/bot-service/internal/integration/kubernetes/runner.go), [`runner_test.go`](../../services/external/bot-service/internal/integration/kubernetes/runner_test.go) |
| `agent-runner` | Получает и завершает ходы, запускает Codex, передаёт ход работы и сохраняет снимок сессии в формате base64 tar/gzip. | [`agent-runner/main.go`](../../services/jobs/agent-runner/cmd/agent-runner/main.go), [`agent-runner/main_test.go`](../../services/jobs/agent-runner/cmd/agent-runner/main_test.go) |
| PostgreSQL-адаптер | Один `admin.Repository` обслуживает все таблицы через один `pgxpool.Pool`; миграции встроены в `bot-service`. | [`domain/repository/admin`](../../services/external/bot-service/internal/domain/repository/admin/repository.go), [`repository/postgres/admin`](../../services/external/bot-service/internal/repository/postgres/admin/repository.go), [`migrations.go`](../../services/external/bot-service/internal/repository/postgres/migrations/migrations.go) |

### 1.2 Подтверждённые разрывы

| Разрыв | Фактическое состояние |
| --- | --- |
| Публичные маршруты | [`ingress.yaml.tpl`](../../deploy/k8s/bot-service/ingress.yaml.tpl) публикует `/` с `pathType: Prefix`, поэтому наружу попадает весь `ServeMux`, включая `/internal/agent-sessions/`, `/mcp/sessions/` и `/metrics`. |
| Удостоверение `action`/`dialog` | `slash` проверяет общий токен, вебхук GitHub — HMAC. `handleAgentsAction` и `handleAgentsDialog` принимают субъект и контекст из JSON без равноценной проверки учётных данных. `publishDialogResult` вызывает переданный `request.URL`. |
| Несколько упоминаний | `routeChatPost` возвращает цель для каждой упомянутой роли, а `HandleChatPost` ставит отдельный ход каждой цели. Одно событие поставщика законно создаёт `N` ходов. |
| Захват | [`agent_session_turns__claim_next.sql`](../../services/external/bot-service/internal/repository/postgres/admin/sql/agent_session_turns__claim_next.sql) не блокирует сессию. Две транзакции могут не увидеть `running` и захватить разные строки `queued`. |
| Завершение/остановка | [`agent_session_turns__complete.sql`](../../services/external/bot-service/internal/repository/postgres/admin/sql/agent_session_turns__complete.sql) обновляет по `id` без CAS состояния, версии и аренды; остановка выполняется отдельным запросом [`agent_session_turns__cancel.sql`](../../services/external/bot-service/internal/repository/postgres/admin/sql/agent_session_turns__cancel.sql). |
| Внешняя доставка | `CompleteTurn` сначала переводит ход в терминальное состояние, затем отдельно вызывает Mattermost. Уникального локального намерения доставки и доказанного механизма идемпотентности поставщика нет. |
| Обратный вызов | `ReturnToRequester` сначала создаёт или изменяет ход, затем устанавливает `callback_turn_id`; проигравший конкурентный CAS не отменяет уже созданный побочный эффект. |
| Хранение | Хранение включено по умолчанию и запускается с `DryRun: false`; повтор после исчерпания квоты также вызывает разрушительную очистку. Решение использует метки, фазу и возраст Kubernetes без проверки допустимости по PostgreSQL и S3. |
| Полномочия | `bot-service` может управлять Pod/PVC/Secret/Job и `pods/exec`; `agent-runner` может получить ServiceAccount только для чтения либо `cluster-admin`, прямые учётные данные OpenAI/GitHub и токены сессии/MCP. |
| Тестовый контур | Текущий [`Makefile`](../../Makefile) содержит только `test-go`; единственный PostgreSQL-тест использует `TEST_DATABASE_DSN` и выполняет `t.Skip`. `test-go-postgres` и `test-go-all` отсутствуют. |

Текущая одна реплика в [`deployment.yaml.tpl`](../../deploy/k8s/bot-service/deployment.yaml.tpl) уменьшает вероятность части гонок, но не превращает их в обеспеченные инварианты.

## 2. Обязательный контур безопасности

### 2.1 Публичные и кластерные маршруты после PR-0

Эталонный снимок отрендерованного Ingress должен быть разрешающим, а не отрицательным списком.

| Маршрут | Доступ после PR-0 | Серверная проверка |
| --- | --- | --- |
| `/mattermost/slash/agents` | Публичный только при необходимости действующей конфигурации Mattermost | проверка slash-токена за постоянное время, ограничение тела и метода |
| `/github/webhook` | Публичный | HMAC, ограничение тела и метода |
| `/mattermost/actions/agents` | Только Mattermost внутри кластера | одноразовая серверная возможность доступа и проверенный субъект |
| `/mattermost/dialogs/agents` | Только Mattermost внутри кластера | одноразовая серверная возможность доступа и проверенный субъект |
| Проверки работоспособности и готовности | Только Service/кластера | без секрета, но не через публичный Ingress |
| `/internal/agent-sessions/*` | Только кластер | действующая проверка учётных данных сессии; отдельные учётные данные с назначением `runner-api` обязательны до порта среды выполнения |
| `/mcp/sessions/*` | Только кластер | действующая проверка учётных данных сессии; отдельные учётные данные с назначением `mcp` и серверное право обязательны до порта среды выполнения |
| `/metrics` | Только кластер/система наблюдаемости | сетевое ограничение и ServiceMonitor/аналог |

URL действия и обратный вызов диалога Mattermost должны указывать на кластерный Service, а публичный Ingress — содержать только явно разрешённые пути. Проверка принимает эталон отрендерованного YAML и список маршрутов HTTP, чтобы новая внутренняя ручка не стала публичной автоматически.

### 2.2 Удостоверение обратных вызовов `action`/`dialog` и субъекта

Минимальный механизм PR-0 — непрозрачная одноразовая возможность доступа, созданная сервером криптографически стойким генератором и сохранённая только в виде хеша. Запись связывает эту возможность со следующими полями:

- `kind` со значением `action`/`dialog` и разрешённая операция;
- внутренний идентификатор ресурса;
- проверенный субъект или разрешённый серверной политикой набор субъектов;
- команда, канал, пост или экземпляр диалога;
- время выдачи, время истечения и состояние `unused|consumed|expired|revoked`;
- хеш безопасного контекста, чтобы один токен нельзя было применить к другим аргументам.

Обратный вызов в одной транзакции проверяет хеш, срок, точные привязки и состояние `unused`, затем переводит возможность доступа в `consumed`. Субъект строится из сохранённой серверной записи и актуального сопоставления Mattermost, а `user_id`/`user_name` из полезной нагрузки считаются только заявленными полями. Несовпадение заявленного и проверенного субъекта даёт `401/403` без вызова прикладного сценария. Повтор, просроченный токен, другая операция, канал, пост или ресурс также отклоняются без побочного эффекта.

Возможность доступа удостоверяет обратный вызов, но не заменяет авторизацию. После неё сервер отдельно проверяет разрешение субъекта на операцию. По решению 6A модели `Organization`, `Membership`, `IntegrationGrant`, grants и квот из #66 не входят в PR-0, однако точка сопряжения обязана возвращать типизированный результат допуска `allowed|denied|indeterminate` с кодом причины; `indeterminate` трактуется как отказ. Промпт, `user_name`, инструкции, метки Kubernetes и ключ идемпотентности не являются правом.

По принятому решению 1A PR-0 использует именно кластерный обратный вызов и описанную серверную возможность доступа. Подписывающий прокси или плагин остаётся рассмотренной, но отклонённой альтернативой; отключение маршрутов `action`/`dialog` допустимо только как аварийное сдерживание и не завершает PR-0. Простая проверка IP источника, существования `user_id` или общий статический URL-токен не защищает от подмены субъекта и повторного воспроизведения.

### 2.3 Политика `response_url`

Предпочтительно не использовать URL из обратного вызова и публиковать результат через настроенный клиент Mattermost. Если `response_url` временно сохраняется, применяется единая политика:

1. источник точно совпадает с настроенным источником Mattermost; пользователь, фрагмент и неожиданный порт запрещены;
2. `https` обязателен, кроме отдельно заданного источника кластерного Service Mattermost;
3. имя разрешается заново перед соединением, все A/AAAA проверяются; адреса обратной петли, локального канала, групповой рассылки, неопределённые адреса, адреса метаданных и частные/кластерные диапазоны запрещены, кроме точного заранее настроенного кластерного Service;
4. проверенный адрес закрепляется на соединение, чтобы повторное DNS-разрешение не обходило политику;
5. перенаправление запрещено; если в будущем оно разрешается, каждый переход проходит ту же проверку и имеет малый предел;
6. метод только `POST`, тело и ответ ограничены, тайм-аут задан, ошибки не содержат URL с чувствительными данными.

Отрицательные тесты покрывают внешний источник, сведения о пользователе, адреса обратной петли IPv4/IPv6, частные и локальные для канала адреса, метаданные кластера, повторное связывание DNS и перенаправление на запрещённый адрес.

### 2.4 Матрица прямых и управляемых полномочий

Матрица фиксирует имена, но не значения переменных и секретов. Фактические пути материализации — `developer`, `reviewer`, `chat` и `session`; `smoke` и `codex-auth*` являются служебными путями без GitHub/MCP и с `automountServiceAccountToken: false`. Имена и PodSpec подтверждаются [`runner.go`](../../services/external/bot-service/internal/integration/kubernetes/runner.go), преобразование файлов GitHub в переменные среды — [`agent-runner/main.go`](../../services/jobs/agent-runner/cmd/agent-runner/main.go), правила именования Secret учётной записи — [`slash.go`](../../services/external/bot-service/internal/domain/service/slash.go), текущие ServiceAccount/RBAC — [`rbac.yaml.tpl`](../../deploy/k8s/bot-service/rbac.yaml.tpl), переменные среды `bot-service` — [`deployment.yaml.tpl`](../../deploy/k8s/bot-service/deployment.yaml.tpl), сетевые значения по умолчанию — [`scripts/lib/env.sh`](../../scripts/lib/env.sh) и [`install-foundation.sh`](../../scripts/remote/install-foundation.sh).

Строка `direct` описывает действующий код. Строка `managed` описывает обязательный целевой контракт только там, где прямо написано «не реализовано»; это не существующая гарантия. Множества `D` и `M` ниже состоят из операций поставщика, RBAC `(verb, resource, scope)`, сетевых пар «назначение, порт» и доступных учётных данных. Для каждой пары приёмка обязана доказать `M ⊆ D`; отсутствие доказательства означает отказ допуска.

| Профиль/возможность | Режим | Точные переменные среды, ключи, подключения и ServiceAccount | Автоподключение и RBAC | Сеть/порт и возможность поставщика | Серверное право и допуск | Отказ по умолчанию и доказательство `managed ⊆ direct` |
| --- | --- | --- | --- | --- | --- | --- |
| `bot-service` → Mattermost | `direct`, текущий | Secret `${MATTERCODEX_BOT_SERVICE_SECRET}`: переменная среды `MATTERCODEX_MATTERMOST_BOT_TOKEN` ← ключ `mattermost-bot-token`; `MATTERCODEX_MATTERMOST_SLASH_TOKEN` ← ключ `mattermost-slash-token`; подключение отсутствует; ServiceAccount `matter-codex-bot-service` | `automount` в Pod/ServiceAccount не задан, поэтому действует значение Kubernetes по умолчанию `true`; дополнительно доступен Role `matter-codex-bot-service-runtime` в namespace | внутреннее назначение по умолчанию `mattermost.${MATTERCODEX_NAMESPACE}.svc.cluster.local:8065`, иначе источник/порт из `MATTERCODEX_MATTERMOST_INTERNAL_URL`/`MATTERCODEX_MATTERMOST_SITE_URL`; NetworkPolicy отсутствует, поэтому исходящий трафик фактически не ограничен; область токена бота у поставщика в репозитории не кодирована | slash-токен проверяется сервером; токен бота разрешает операции клиента Mattermost; `action`/`dialog` равноценной проверки сейчас не имеют | не считать безопасным исходным состоянием для `action`/`dialog`; PR-0 блокирует дальнейшие структурные изменения |
| `bot-service` → Mattermost | `managed`, цель PR-0, не реализовано | те же два ключа Secret остаются только в граничном адаптере; `agent-runner` не получает переменные среды, ключи и подключения; отдельный ServiceAccount не добавляется | не расширяет текущий Role; публичный Ingress не даёт доступ к внутренним маршрутам, MCP и метрикам | только настроенный источник/Service Mattermost и его явный порт; операции поставщика ограничены разрешающим списком `slash`/`action`/`dialog`/`post`/`update` | одноразовая возможность доступа, проверенный субъект, привязки операции, ресурса, канала и поста и типизированный `allowed\|denied\|indeterminate` | снимок переменных, подключений, маршрутов и исходящего трафика должен показать отсутствие новых учётных данных и назначений; разрешающий список операций должен быть подмножеством операций текущего клиента Mattermost |
| `bot-service` → PostgreSQL | `direct`, текущий | Secret `${MATTERCODEX_POSTGRES_SECRET}`: runtime `MATTERCODEX_DATABASE_DSN` ← `bot-service-runtime-datasource`, migration `MATTERCODEX_MIGRATIONS_DATABASE_DSN` ← `mattermost-datasource`; один существующий Service/порт; ServiceAccount `matter-codex-bot-service` | те же `automount` по умолчанию и Role среды выполнения, не нужные для PostgreSQL | оба DSN обязаны указывать на `mattermost-postgres.${MATTERCODEX_NAMESPACE}.svc.cluster.local:5432` и одну базу; migration owner создаёт/ужесточает отдельный `NOSUPERUSER NOBYPASSRLS` runtime login, а миграция выдаёт точные DML/sequence/function grants без DDL/TEMP | стартовая проверка отклоняет владельца схемы/таблиц, superuser, `BYPASSRLS`, опасное членство, `CREATE` и `TEMP`; fallback на owner DSN отсутствует | DSN и raw Secret values не передаются `agent-runner`/DTO и не журналируются; в frozen state хранится только безопасная SHA-256/UID/version metadata |
| `bot-service` → PostgreSQL | `managed`, целевая модульная граница, не реализовано | переменная среды и ключ остаются только у процесса-владельца схемы; `agent-runner` и MCP не получают DSN; новое подключение отсутствует | Kubernetes RBAC не добавляется | только адрес и порт PostgreSQL из DSN; возможность поставщика ограничена репозиторными портами, определёнными потребителями, и одним владельцем миграций | прикладной координатор допускает типизированную команду; междоменный прямой SQL запрещён | проверки импортов и SQL-контрактов и снимок переменных среды доказывают, что `M` удаляет общий `admin.Repository` у потребителей и не добавляет SQL-доступ или сеть |
| `bot-service` → GitHub | `direct`, текущий | Secret `${MATTERCODEX_GITHUB_SECRET}`: переменная среды `MATTERCODEX_GITHUB_TOKEN` ← ключ `github-token`; `MATTERCODEX_GITHUB_WEBHOOK_SECRET` ← ключ `github-webhook-secret`; подключение отсутствует; ServiceAccount `matter-codex-bot-service` | те же `automount` по умолчанию и Role среды выполнения, не нужные GitHub | предполагаются `github.com:443`/`api.github.com:443`, но NetworkPolicy отсутствует; области токена определены GitHub и не кодированы в репозитории | HMAC вебхука проверяется; токен используется нативным адаптером поставщика для операций с репозиторием и вебхуком; право на отдельный репозиторий отсутствует | вебхук без корректного HMAC отклоняется; возможность токена нельзя считать минимально необходимой без снимка области у поставщика |
| `bot-service` → GitHub | `managed`, будущая граница интеграции, не реализовано | эти переменные и ключи GitHub отсутствуют у `control-plane`; учётные данные находятся в шлюзе, ServiceAccount `matter-codex-integration-github`; ключ подписи вебхука остаётся только в граничном адаптере | Kubernetes RBAC не добавляется | граничный адаптер принимает вебхук; исходящий трафик шлюза ограничен `github.com:443`/`api.github.com:443` | право на репозиторий и возможность, HMAC-идентичность, субъект/область и допуск по ключу идемпотентности | снимок маршрутов, переменных, исходящего трафика и операций поставщика доказывает, что право шлюза не шире снимка прямого токена и репозитория; неизвестные репозиторий или операция дают отказ |
| `bot-service` → среда выполнения Kubernetes | `direct`, текущий | ServiceAccount `matter-codex-bot-service`; учётные данные — проецируемый токен ServiceAccount по `automount` по умолчанию; отдельных переменных, ключей и подключений в шаблоне нет | Role `matter-codex-bot-service-runtime`: `create,get,list,delete` PVC/ConfigMap/Pod; `get` `pods/log`; `create` `pods/exec`; `create,get,list,update,patch,delete` Secret; `create,get,list,delete` Job; область — `${MATTERCODEX_NAMESPACE}` | Kubernetes API из внутрикластерной конфигурации, обычно Service `:443`; NetworkPolicy отсутствует; возможность поставщика равна перечисленному Role | отдельного права для допуска HTTP/приложения нет; бизнес-сервис напрямую вызывает адаптер | Принята единственная узкая оговорка к исходному verb set: `patch` только для UID/resourceVersion-fenced снятия управляемого finalizer перед удалением Secret. Новых `resources`, `apiGroups`, wildcard или namespace нет; снимок Role и запрет новых импортов служат отрицательным доказательством |
| `bot-service` → среда выполнения Kubernetes | `managed`, будущий порт среды выполнения, не реализовано | целевой ServiceAccount `matter-codex-runtime-controller`; токен есть только у контроллера, но не у `bot-service`/`agent-runner`; ключ и подключение Secret отсутствуют | точная копия или подмножество текущих правил namespace; `pods/exec` и глаголы Secret удаляются, если соответствующий сценарий не доказан отдельным шлюзом | только Kubernetes API `:443`; NetworkPolicy запрещает иной исходящий трафик | типизированная команда среды выполнения, владение ресурсом, допуск namespace и ключ идемпотентности | отрендерованный diff обязан доказать отсутствие новых субъектов, глаголов, ресурсов и областей и `M ⊆ D`; неизвестная операция отклоняется |
| Служебный `smoke` | `direct`, текущий | переменные среды `MATTERCODEX_RUN_ID`, `MATTERCODEX_AGENT_ROLE`; ключ и подключение Secret отсутствуют; PVC рабочей области `/workspace`; ServiceAccount `matter-codex-agent-runner` | для Pod и ServiceAccount `automount: false`; токен и RBAC фактически недоступны контейнеру | NetworkPolicy отсутствует, хотя возможность поставщика не требуется | отдельного серверного права нет; Job создаёт адаптер среды выполнения `bot-service` | исходное множество возможностей пусто; снимок должен падать при появлении Secret, учётной переменной среды, `automount` или назначения поставщика |
| Служебный `smoke` | `managed`, целевой контракт, не реализовано как отдельный режим | те же две несекретные переменные среды и рабочая область; Secret и подключение отсутствуют; ServiceAccount `matter-codex-agent-runner` | `automount: false`, RBAC нет | исходящий трафик запрещён полностью | допуск разрешает только локальную команду и образ `smoke` | `M = D = ∅` для учётных данных, поставщика и RBAC; доказательством служит отрендерованный снимок Pod/NetworkPolicy |
| Служебные `codex-auth`/`codex-auth-secret-check` | `direct`, текущий | переменные среды `MATTERCODEX_OPENAI_ACCOUNT`, `MATTERCODEX_CODEX_AUTH_SECRET`; Job авторизации использует emptyDir `/codex-home`; проверочный Job подключает том `codex-auth-secret`, ключ `auth.json` в `/var/run/secrets/matter-codex-codex`; ServiceAccount `matter-codex-agent-runner` | для Pod и ServiceAccount `automount: false`; токена RBAC нет | NetworkPolicy отсутствует; предполагаются HTTPS-адреса устройства и авторизации OpenAI `:443` | сервер выбирает учётную запись/SecretRef; право поставщика на отдельную операцию отсутствует | служебный процесс не получает учётные данные GitHub, сессии, MCP или Kubernetes; это фиксирует точный снимок Pod |
| Тот же сценарий авторизации | `managed`, будущий шлюз поставщика, не реализовано | состояние авторизации и учётные данные поставщика остаются в `matter-codex-provider-gateway`, ServiceAccount с тем же именем; служебный Job агента, подключение `auth.json` и `MATTERCODEX_CODEX_AUTH_SECRET` отсутствуют | `automount` только у шлюза по отдельному Role; RBAC агента отсутствует | UI/граничный адаптер → порт Service шлюза; шлюз → разрешённые адреса авторизации OpenAI `:443` | краткоживущая разрешённая владельцем транзакция авторизации, привязка учётной записи, срок действия и аудит | отсутствие Job/подключения и сравнение операций адреса и учётной записи доказывают сужение; до отдельного ADR сохраняется служебный процесс `direct` |
| `developer`/`reviewer`/`chat`/`session` → OpenAI Codex | `direct`, текущий | том `codex-auth-secret`, ключ/путь `auth.json`, подключение только для чтения `/var/run/secrets/matter-codex-codex`; `agent-runner` материализует private per-run `CODEX_HOME` только в container-ephemeral каталоге и ставит исходный и переписанный `auth.json` под fail-closed event guard; имя Secret = `OpenAIAccount.SecretRef`, создаётся как `<MATTERCODEX_CODEX_AUTH_SECRET>-<account>` с базовым именем по умолчанию `matter-codex-codex-auth`; ServiceAccount зависит только от возможности Kubernetes | Pod переопределяет `automountServiceAccountToken: true` даже у ServiceAccount с `false`; RBAC описан отдельными строками Kubernetes ниже и не нужен OpenAI | NetworkPolicy отсутствует, поэтому исходящий трафик не ограничен; предполагаемый поставщик — Codex/OpenAI по HTTPS `:443`; возможность поставщика определяется содержимым выбранной учётной записи и вне репозитория не ограничена | сервер выбирает `OpenAIAccountName`/SecretRef профиля; право поставщика на отдельную операцию отсутствует | отсутствие выбранной учётной записи должно блокировать запуск; содержимое `auth.json` не попадает в промпт, журнал, результат или архив; create/write/delete/rename/replace и watcher failure до завершения child запрещают публикацию |
| `developer`/`reviewer`/`chat`/`session` → OpenAI Codex | `managed`, не входит в PR-0…PR-4 и не реализовано | переменные среды агента `MATTERCODEX_PROVIDER_GATEWAY_URL`, `MATTERCODEX_PROVIDER_SESSION_TOKEN` ← ключ `provider-session-token` Secret `mc-provider-session-<session-key>`; `codex-auth-secret`/`auth.json` отсутствуют; ServiceAccount шлюза `matter-codex-provider-gateway` | `automount` Pod агента определяется независимой возможностью Kubernetes; RBAC поставщика отсутствует | агент → порт Service шлюза поставщика; шлюз → разрешённые адреса OpenAI `:443` | краткоживущее назначение `provider`, право на учётную запись, ревизию, модель и сессию; неизвестная возможность отклоняется | отрицательные снимки подтверждают отсутствие подключения авторизации и прямого исходящего трафика и то, что право на модели и действия является подмножеством прямой учётной записи; эти имена — целевой контракт, а не существующий манифест |
| `developer`/`reviewer` и назначенные `chat`/`session` → GitHub | `direct`, текущий | том `github-secret`; ключи `github-token`, `github-username`, `github-email`; подключение только для чтения `/var/run/secrets/matter-codex-github`; имя Secret = `GitHubAccount.SecretRef`, формируется как `<MATTERCODEX_GITHUB_SECRET>` для `primary` или `<base>-<account>`; `agent-runner` создаёт `GH_TOKEN`, `GITHUB_TOKEN`, `GITHUB_USERNAME`, `GITHUB_USER`, `GITHUB_EMAIL`, `GIT_*`, `MATTERCODEX_GIT_ASKPASS`, `MATTERCODEX_GITHUB_TOKEN_FILE`; ServiceAccount зависит от возможности Kubernetes | Pod переопределяет `automount: true`; Kubernetes RBAC не зависит от GitHub | NetworkPolicy отсутствует; предполагаются `github.com:443`/`api.github.com:443`, но технически доступны любые достижимые назначение и порт; области токена задаются GitHub и в репозитории не доказаны | допуск — наличие учётной записи GitHub и SecretRef у профиля; серверное право на отдельный репозиторий или операцию отсутствует | без назначения у `chat`/`session` нет тома и переменных среды; `developer`/`reviewer` всегда получают заданный Secret; `direct` нельзя сочетать с обещанием обязательного согласования |
| Те же профили → GitHub | `managed`, будущий шлюз интеграции, не реализовано | в Pod агента отсутствуют `github-secret`, перечисленные переменные GitHub/Git и файл токена; остаются только `MATTERCODEX_MCP_URL` и отдельные учётные данные с назначением `mcp` из ключа `mcp-token`; целевой ServiceAccount шлюза `matter-codex-integration-github` | `automount` агента не включается из-за GitHub; Kubernetes RBAC шлюзу не требуется | агент → шлюз интеграции на порту кластерного Service; шлюз → `github.com:443`/`api.github.com:443`; другие назначения запрещены | право связывает организацию, рабочую область, агента, репозиторий, возможность (`read\|issue\|pull_request\|contents_write\|admin`) и политику согласования | снимок переменных, подключений и NetworkPolicy и отрицательный каталог инструментов; набор операций GitHub каждого права сравнивается с областями токена и прямым профилем, неизвестное даёт отказ |
| Все рабочие профили → Kubernetes только для чтения | `direct`, текущее значение по умолчанию | Kubernetes вводит переменные среды `KUBERNETES_SERVICE_*`/`KUBERNETES_PORT*`; проецируемые файлы `/var/run/secrets/kubernetes.io/serviceaccount/{token,ca.crt,namespace}`; ServiceAccount `${MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT}` с именем по умолчанию `matter-codex-agent-runner` | ServiceAccount объявлен с `automount: false`, но Pod явно ставит `true`; Role `matter-codex-agent-runner-readonly`: `get,list,watch` для core `pods,pods/log,services,endpoints,configmaps,persistentvolumeclaims,events`, apps `deployments,statefulsets,daemonsets,replicasets`, batch `jobs,cronjobs`, networking `ingresses`; область — namespace | Kubernetes API из введённых переменных среды, обычно `:443`; исходящий трафик не ограничен; возможность поставщика — точный Role namespace только для чтения | строка `kubernetes_access=read-only` проходит проверку перечисления, но отдельного серверного права нет | неизвестное значение нормализуется в `read-only`, однако запуск без возможности Kubernetes сейчас не существует: токен всё равно подключается; это открытый разрыв |
| Те же профили → Kubernetes только для чтения | `managed`, будущий MCP, не реализовано | Pod агента: ServiceAccount `matter-codex-agent-runner`, `automountServiceAccountToken: false`, проецируемые токен и переменные среды отсутствуют; только `MATTERCODEX_MCP_URL` + ключ `mcp-token`; ServiceAccount шлюза `matter-codex-integration-kubernetes-readonly` | Role шлюза — точное подмножество текущего `matter-codex-agent-runner-readonly`; область — тот же namespace | агент → порт Service шлюза; шлюз → Kubernetes API `:443`; прямой доступ агента к API запрещён NetworkPolicy | право `kubernetes.read` содержит разрешающий список ресурсов и namespace; сервер повторно проверяет каждый глагол и ресурс | снимки RBAC/NetworkPolicy/инструментов и отрицательные `create/update/patch/delete/exec`; математическое сравнение правил доказывает `M ⊆ D` |
| Только профиль с `kubernetes_access=cluster-admin`, включая начальную настройку `mattercodex-admin` | `direct`, текущий путь под сдерживанием 3A | те же введённые переменные среды и проецируемые файлы; ServiceAccount `${MATTERCODEX_AGENT_RUNNER_CLUSTER_ADMIN_SERVICE_ACCOUNT}` с именем по умолчанию `matter-codex-agent-runner-cluster-admin` | ServiceAccount `automount: false`, Pod переопределяет `true`; `ClusterRoleBinding` `matter-codex-agent-runner-cluster-admin` → встроенный `cluster-admin`, все глаголы и ресурсы, область кластера | Kubernetes API `:443`, исходящий трафик не ограничен; возможность поставщика — полный `cluster-admin` | новые назначения заморожены; только уже настроенные профиль и назначение могут пройти явно заданные на сервере право профиля и допуск с аудитом, а неизвестные или новые назначения получают отказ; [`system_roles.go`](../../services/external/bot-service/internal/domain/service/system_roles.go) создаёт `mattercodex-admin`, но промпт и само значение перечисления не являются правом | обязательное решение 3A не разрешает расширение субъектов, глаголов, ресурсов или области; существующая широкая возможность не выдаётся за безопасную или управляемую, а снимок уже настроенных назначений фиксирует закрытый разрешающий список |
| Тот же логический административный сценарий | `managed`, выбранное целевое состояние отдельного будущего PR, не реализовано в PR-0…PR-4 | Pod агента без токена и переменных Kubernetes; ServiceAccount шлюза `matter-codex-integration-kubernetes-admin`; `cluster-admin` этому ServiceAccount запрещён — права задаются отдельными Role/ClusterRole для принятого набора операций | `automount` только у шлюза; глаголы, ресурсы и область не шире явно принятого прямого сценария и снимка до миграции | агент → порт Service шлюза; шлюз → Kubernetes API `:443`; прочий исходящий трафик запрещён | право `platform_admin` + согласование человеком или явно принятое аварийное право, хеш аргументов, срок действия, идемпотентность и аудит | `cluster-admin` RoleBinding/ClusterRoleBinding в варианте `managed` отсутствует; отрицательные тесты чтения секретов, повышения RBAC, выдачи себя за другой субъект, `exec` и операций записи уровня кластера вне разрешающего списка доказывают сужение |
| `session` → API компонента запуска | `direct`, текущий внутренний путь | переменные среды `MATTERCODEX_BOT_SERVICE_URL`, `MATTERCODEX_SESSION_TOKEN`; том `session-secret`, ключ `token` в Secret `mc-session-token-<session-key>`; тот же ключ подключён только для чтения как `/var/run/secrets/matter-codex-session/token`; ServiceAccount как выше | `automount: true`; Kubernetes RBAC не зависит от этого доступа | по умолчанию `matter-codex-bot-service.${MATTERCODEX_NAMESPACE}.svc.cluster.local:${MATTERCODEX_BOT_SERVICE_PORT}` (`8080` по умолчанию); NetworkPolicy отсутствует | токен предъявителя сверяется с сессией в `Snapshot/Claim/Complete/Status`; назначение не отделено от MCP | несовпадение Secret и сессии даёт отказ, но повторное использование одного токена для двух назначений остаётся разрывом |
| `session` → API компонента запуска | `managed`, цель PR-3, не реализовано | переменная среды `MATTERCODEX_SESSION_TOKEN` ← ключ `runner-api-token`; подключение `/var/run/secrets/matter-codex-session/runner-api-token`; отдельный `mcp-token`; ServiceAccount не меняется | не добавляет RBAC | только кластерный маршрут Service `/internal/agent-sessions/*` на объявленном порту | краткоживущее назначение `runner-api`, привязки сессии, хода, аренды, ограждения и команды | повтор между назначениями и чужая сессия отклоняются; снимок учётных данных, инструментов и маршрутов не добавляет возможность поставщика |
| `session` → Mattermost без MCP | `direct`, текущий | токены бота и slash Mattermost, ключи и подключения в Pod агента отсутствуют; ServiceAccount как выше | `automount` и RBAC не зависят от этого доступа | прямое соединение `agent-runner` → Mattermost не требуется контрактом, но NetworkPolicy сейчас его не запрещает | прямая возможность поставщика агенту не выдана; промпт также не является правом | исходная логическая возможность для сравнения задаётся разрешёнными серверными операциями, а не отсутствующим токеном; прямой исходящий трафик к Mattermost должен быть закрыт в целевом снимке |
| `session` → инструменты MCP Mattermost | `managed`, текущий частичный | переменные среды `MATTERCODEX_MCP_URL`, `MATTERCODEX_MCP_TOKEN`; сейчас обе токенные переменные читают один ключ `token`; отдельного подключения MCP нет; ServiceAccount как выше | `automount: true`; Kubernetes RBAC не зависит от этого доступа | маршрут `bot-service` `/mcp/sessions/<session>`; исходящий трафик не ограничен | проверка токена предъявителя и сессии; зарегистрированы инструменты обсуждений, поиска, публикации, статуса, каталога, делегирования и обратного вызова в [`mcp.go`](../../services/external/bot-service/internal/transport/http/mcp.go); право на отдельный инструмент отсутствует | `agent-runner` не получает токен бота Mattermost, но инструкция промпта является только правилом поведения, а не правом; текущий режим `managed` ещё не доказывает `M ⊆ D` |
| `session` → инструменты MCP Mattermost | `managed`, целевой контракт PR-3/PR-4, не реализовано | `MATTERCODEX_MCP_TOKEN` ← отдельный ключ `mcp-token`; `MATTERCODEX_SESSION_TOKEN` не принимается MCP; подключения и учётные данные поставщика отсутствуют | MCP не включает `automount`/RBAC Kubernetes | только порт кластерного Service MCP; сервер → Mattermost только через нативный адаптер | назначение `mcp`, разрешённые инструмент и возможность, область сессии, рабочей области и агента, проверенный субъект и право побочного эффекта; промпт не участвует в допуске | каталог инструментов и отрицательные тесты между назначениями; операции каждого права — подмножество текущего серверного набора инструментов, неизвестные инструмент, область или право дают отказ и ноль побочных эффектов |
| Любой рабочий профиль → произвольная переменная среды выполнения | `direct`, текущий | имя переменной = сохранённый `RuntimeEnvVar.Name`; имя Secret = `SecretRef`; ключ = `SecretKey` или `value` по умолчанию; подключения нет; имя включается в `MATTERCODEX_RUNTIME_ENV_ALLOWLIST`; ServiceAccount как у профиля | `automount`/RBAC не зависят от переменной | из-за отсутствия NetworkPolicy значение может использоваться для любых достижимых назначения и порта | допуск — включённая привязка роли; типизированная семантика схемы, возможности и поставщика отсутствует | до типизации возможности это только `direct` и не может считаться `managed`; снимок обязан перечислить точные имена конкретной `RuntimeRevision` без значений |
| Любой рабочий профиль → произвольная переменная среды выполнения | `managed`, будущий контракт, не реализовано | соответствующие переменная и ключ полностью отсутствуют в Pod агента; остаются `MATTERCODEX_MCP_URL` и `MATTERCODEX_MCP_TOKEN` ← ключ `mcp-token`; учётные данные остаются в шлюзе; контракт имени ServiceAccount `matter-codex-integration-<IntegrationDefinition.metadata.name>` | агент не получает дополнительные `automount`/RBAC | только порт Service шлюза и разрешённые назначение и порт поставщика | `IntegrationGrant` + контракт риска, согласования и идемпотентности | отрицательный снимок конкретной `RuntimeRevision` проверяет отсутствие прежней переменной и прямого исходящего трафика; без типизированной возможности миграция запрещена |

Обязательное приёмочное доказательство для каждой фактически развёртываемой роли — сопоставленные рядом снимки `direct` и `managed`: имена переменных среды, ссылки на Secret и ключи, подключения, Pod `serviceAccountName`/`automount`, нормализованные правила и область RBAC, назначения и порты NetworkPolicy, возможность поставщика и серверные права. Проверка машинно сравнивает множества и падает при новой переменной, подключении, субъекте, глаголе, ресурсе, области, назначении, порте или операции поставщика. Пустое или неизвестное право и отсутствующий снимок дают отказ. До появления этих реализаций строки `managed` остаются приёмочным контрактом, а не гарантией.

`replicas >= 2` остаются запрещены до успешного прохождения шлюзов квитанции, исходящего журнала, идемпотентности, выбора лидера, аренды, ограждения устаревшего исполнителя, единственного владельца циклов и матрицы `N-1/N`; одна только матрица полномочий этот запрет не снимает.

## 3. Владение данными и внешними идентификаторами

### 3.1 Один логический владелец таблицы и мутации

Все миграции физически остаются одним упорядоченным потоком `bot-service` до отдельного принятого владельца схемы. Применённые `000001`–`000020` не перемещаются и не редактируются.

| Таблица/группа | Логический владелец волны 1 | Разрешённые писатели |
| --- | --- | --- |
| проекты, чаты, участники, репозитории чата, `thread_contexts`, будущие привязки Mattermost и `InboundEventReceipt` | `conversations` | только прикладные сценарии `conversations` |
| профили, роли, шаблоны, переменные среды выполнения, учётные записи ботов | `agents` | только сценарии `agents`; создание бота идёт через порт Mattermost |
| учётные записи OpenAI/GitHub, ссылки на учётные данные, репозитории | `providers` как переходный владелец | только сценарии `providers`; значения учётных данных не хранятся в DTO |
| `matter_codex_agent_sessions`, `matter_codex_agent_session_turns`, `matter_codex_agent_delegations`, `matter_codex_agent_runs` | `runtime` | только транзакции `runtime` |
| `matter_codex_agent_flows` | `processes` | только переходные сценарии `processes` |
| `matter_codex_audit_events` | `audit` | порт аудита только для добавления |
| строка исходящего журнала `OutboxEvent` | домен, создавший бизнес-изменение | транзакция этого домена; обработчик только меняет состояние доставки |
| `goose_db_version` и порядок миграций | один владелец схемы | одно задание или процесс миграции; прикладные реплики миграции конкурентно не запускают |

Нельзя делить владение по колонкам одной таблицы. Междоменная команда или событие может вызвать мутацию владельца, но другой домен не выполняет прямой `UPDATE` и не читает таблицу как контракт.

### 3.2 Внешние идентификаторы Mattermost в сессиях и ходах

Таблицы сессий и ходов целиком принадлежат `runtime`, включая физически существующие `mattermost_channel_id`, `mattermost_root_post_id`, `mattermost_post_id`, `user_id` и `user_name` из [`000014_agent_sessions.sql`](../../services/external/bot-service/internal/repository/postgres/migrations/000014_agent_sessions.sql). Эти поля имеют один из двух смыслов:

- неизменяемый снимок цели доставки или источника на момент принятия команды;
- стабильная ссылка на привязку, принадлежащую `conversations`.

Снимок создаёт только транзакция `runtime` из типизированной команды. После создания исходные внешние идентификаторы не редактируются из `conversations`; изменение привязки создаёт новую версию или событие, а не междоменный `UPDATE`. `user_name` является снимком отображения и не используется для авторизации.

Потребитель `runtime` объявляет у себя порт чтения `ConversationRoutingPort`; его адаптер получает проверенный `ConversationRouteSnapshot` из владельца `conversations`. Порт возвращает внутренние ссылки, проверенного субъекта и целевые внешние идентификаторы без доступа к таблицам другого домена. Для доставки `conversations` объявляет собственный порт, определённый потребителем, к `runtime` только для чтения типизированного намерения доставки, а не таблиц сессий и ходов.

Итоговый контракт:

- `conversations` владеет актуальным сопоставлением Workspace/Room/Conversation с Mattermost;
- `runtime` владеет каждой строкой сессии и хода и неизменяемым снимком, использованным конкретным ходом;
- транспорт не владеет ни тем, ни другим и только преобразует DTO;
- прямое междоменное чтение и разделение владения по колонкам запрещены.

## 4. Обязательные долговечные контракты

### 4.1 Версионированный конверт событий и команд

События и команды используют общий набор метаданных, но разные поля типа и версии.

| Поле | Контракт |
| --- | --- |
| `event_id`/`command_id` | внутренний неизменяемый идентификатор сообщения |
| `event_type` + `event_version` или `command_type` + `command_version` | стабильное имя и положительная целая версия схемы |
| `occurred_at`/`created_at` | серверное время события/команды |
| `authenticated_actor` | типизированная ссылка на субъекта, способ удостоверения и серверная ссылка на субъект |
| `asserted_actor` | необязательные поля полезной нагрузки поставщика только для диагностики; не используются для авторизации |
| `organization_scope` | обязательная ссылка в области установки уже в первом профиле; не требует таблиц полной #66 |
| `workspace_scope` | проверенная внутренняя ссылка на текущий Project/будущий Workspace |
| `session_scope` | необязательная внутренняя ссылка или детерминированный различитель сессии |
| `correlation_id` | одна цепочка от события поставщика до хода и доставки |
| `causation_id` | непосредственное входное событие/команда, породившее сообщение |
| `idempotency_key` | детерминированный ключ повтора внутри указанной области |
| `payload` | типизированная версия данных без секретов и неотфильтрованной полезной нагрузки поставщика |
| `admission` | типизированный результат `admitted\|ignored\|rejected\|duplicate\|deferred` и безопасный код причины |

По решению 6A `organization_scope` в волне 1 является только стабильной ссылкой на одну установку и проходит через точку сопряжения допуска; PR-0 и PR-4 не создают общую модель `Organization`, `Membership`, `IntegrationGrant`, продуктовые grants и квоты. Эти сущности и полная #66 реализуются отдельными архитектурным, схемным и поведенческими срезами после PR-4. Неизвестная область, субъект или результат допуска дают закрытый отказ `rejected`, а не значение по умолчанию.

Промпт, `user_name`, текст инструкций, ключ идемпотентности и наличие метки Kubernetes не являются авторизацией. Сохранённый результат `duplicate` возвращается только после повторной проверки субъекта и фактической области агрегата.

### 4.2 Одна квитанция и детерминированное разветвление в N целей

`InboundEventReceipt` уникален по `(provider, provider_event_type, provider_event_id)`. Для Mattermost `provider_event_id` берётся из стабильной идентичности события или поста, а `provider_event_type` является обязательным различителем, чтобы создание и изменение одного поста не смешивались. Квитанция хранит хеш канонической безопасной полезной нагрузки и результат допуска.

Алгоритм первого приёма:

1. Удостоверить источник и субъекта и вычислить область.
2. Вычислить каноническую идентичность события у поставщика и попытаться создать одну квитанцию.
3. Детерминированно получить цели, удалить дубли и отсортировать по `target_discriminator`.
4. Сохранить неизменяемый снимок целей в результате квитанции.
5. Для каждой цели создать отдельное намерение команды и `OutboxEvent` в той же локальной транзакции.
6. `runtime` идемпотентно материализует отдельные сессию и ход на каждую команду.

`target_discriminator` включает как минимум `target_role_id`, `target_session_scope` и канонический `session_key` или ссылку на корень. Ключи:

```text
receipt: <provider>:<provider_event_type>:<provider_event_id>
command: mattermost.user-instruction.v1:<receipt_id>:<hash(target_discriminator)>
turn:    runtime.turn.v1:<command_id>:<target_discriminator>
```

Повтор с той же идентичностью события у поставщика и другим хешем полезной нагрузки даёт типизированный конфликт и ноль побочных эффектов. Повтор после изменения ролей возвращает исходный сохранённый снимок целей и не добавляет новые цели. Одно событие с несколькими упоминаниями создаёт одну квитанцию, `N` команд и `N` ходов — по одному на целевую сессию.

Обязательны последовательный и конкурентный тесты повтора события с несколькими упоминаниями через два соединения PostgreSQL. Они проверяют одну квитанцию, исходное разветвление в `N` целей, по одной команде и одному ходу на различитель и отсутствие новых целей при повторе.

### 4.3 Сериализация сессии, аренда, пульс и ограждение устаревшего исполнителя

Захват выполняется одной транзакцией:

1. блокируется строка сессии `FOR UPDATE` или эквивалентная защита сессии;
2. проверяется отсутствие живой аренды активного хода;
3. повтор с тем же `claim_request_id` возвращает ранее выданный захват;
4. выбирается старейший `queued` по `(sequence, id)`;
5. создаётся аренда с `lease_owner`, `lease_expires_at` и монотонным `fencing_token`;
6. ход меняется `queued -> running` через CAS состояния и версии.

Частичный уникальный индекс «не более одного `running` на сессию» служит последней защитой, но не заменяет блокировку сессии. Пульс продлевает только совпадающие `turn_id + lease_owner + fencing_token + version`. Просроченный ход можно захватить повторно с увеличением `fencing_token`. Старый исполнитель после повторного захвата не может обновить пульс, завершить или остановить ход.

Завершение и остановка конкурируют через один переход `where status = 'running' and version = ? and fencing_token = ?`. Победитель увеличивает версию и создаёт терминальное состояние; проигравший получает `stale_claim|already_terminal|conflict` и не меняет снимок, запуск, `OutboxEvent` или Mattermost. Повтор победившей команды с тем же ключом идемпотентности возвращает сохранённый результат.

Обязательные проверки PR-3:

- два исполнителя одновременно захватывают одну сессию;
- потерянный HTTP-ответ и повтор того же `claim_request_id`;
- истечение аренды, повторный захват и рост `fencing_token`;
- устаревшее завершение после повторного захвата;
- перезапуск до и после захвата, до завершения и после локального завершения;
- конкурентные остановка и завершение и повтор каждой команды;
- минимум два настоящих соединения PostgreSQL, без mutex в памяти как единственной защиты.

#### 4.3.1 Единая транзакция завершения после загрузки архива

Текущий [`CompleteTurn`](../../services/external/bot-service/internal/domain/service/agent_session_service.go) сначала переводит ход в терминальное состояние, а затем отдельными вызовами обновляет снимок сессии, запуск и Mattermost; `agent-runner` передаёт base64-архив в той же полезной нагрузке HTTP. Это подтверждённое окно отказа, а не допустимый целевой контракт.

PR-3 обязан заменить этот порядок следующим протоколом:

1. Исполнитель с действующими `turn_id + lease_owner + fencing_token + version` запрашивает серверное резервирование завершения. CAS не делает ход терминальным; он связывает `completion_attempt_id` с текущим значением ограждения и выдаёт краткоживущее право загрузки только для неизменяемого ключа `session/<session_id>/turn/<turn_id>/fence/<fencing_token>/attempt/<completion_attempt_id>`.
2. До PostgreSQL-транзакции завершения адаптер S3 загружает неизменяемую версию архива, вычисляет и проверяет контрольную сумму и возвращает `archive_version_id + storage_key + checksum + size`. Ошибка загрузки или проверки оставляет ход нетерминальным и доступным для повтора; резервирование можно безопасно повторить с тем же ключом идемпотентности.
3. После загрузки одна PostgreSQL-транзакция повторно блокирует сессию и ход и проверяет состояние, версию, аренду, ограждение и попытку завершения. Она атомарно:
   - переводит ход в единственное терминальное состояние и сохраняет метаданные итогового ответа, ошибки и артефакта;
   - обновляет снимок сессии и `codex_session_id`;
   - сохраняет `archive_version_id`, ссылку, контрольную сумму и версию как текущий архив сессии;
   - меняет состояние сессии, очищает активный ход, закрывает активные аренду и резервирование завершения и сохраняет последний монотонный `fencing_token` без возможности отката к меньшему значению;
   - сохраняет состояние и результат текущего запуска и результат команды с ключом идемпотентности;
   - создаёт ровно одно локальное намерение результата `OutboxEvent` с ключом из раздела 4.5.
4. Только успешная фиксация транзакции делает терминальное состояние и ссылку на архив видимыми. Следующий захват блокирует ту же строку сессии и получает новые снимок, `codex_session_id` и ссылку на архив, созданные победившим завершением.

PostgreSQL и S3 не объединяются в распределённую транзакцию, и документ её не обещает. Если фиксация БД не состоялась после успешной загрузки, неизменяемая версия архива остаётся безопасной непривязанной версией без ссылки из терминальной сессии. Сверка находит её по `completion_attempt_id`; сборщик мусора удаляет её только после периода отсрочки, проверки отсутствия ссылки или блокировки хранения в PostgreSQL и записи аудита. Повтор завершения может повторно использовать ту же проверенную версию либо создать новую, но терминальную ссылку выбирает только победившая фиксация.

Запрос, который уже устарел при выдаче резервирования или при финальном CAS, не меняет ход, сессию, снимок, `codex_session_id`, активные аренду и ограждение, запуск, результат или `OutboxEvent` и не получает действующего права загрузки. Если исполнитель устарел уже после принятой загрузки, но до фиксации, загруженная версия может остаться только непривязанной по правилу выше; она не становится текущим архивом и не влияет на следующий захват. Это ограничение внешнего побочного эффекта S3 сформулировано явно и не скрывается обещанием межсистемной транзакции.

| Точка отказа/гонка | Состояние PostgreSQL | Состояние S3/исходящего журнала | Обязательная проверка |
| --- | --- | --- | --- |
| До резервирования | ход остаётся `running` с текущими арендой и ограждением | архив и `OutboxEvent` отсутствуют | повтор с тем же ключом идемпотентности завершения начинает ту же логическую попытку |
| После резервирования, до загрузки | ход нетерминальный и доступен для повтора; терминальное состояние, снимок сессии и запуск не меняются | объекта и `OutboxEvent` нет | отказ или перезапуск освобождает либо повторно использует резервирование без нового терминального результата |
| Ошибка загрузки или проверки контрольной суммы | ход нетерминальный и доступен для повтора; аренду можно продлить либо повторный захват повышает ограждение | неуспешная версия не получает ссылку; `OutboxEvent` отсутствует | следующий допустимый исполнитель повторяет загрузку; устаревшая попытка отвергается |
| Загрузка успешна, до транзакции БД | терминальное состояние отсутствует | неизменяемая непривязанная версия; `OutboxEvent` отсутствует | сверка видит попытку; следующий захват не использует эту непривязанную версию |
| Ошибка или откат транзакции БД | ни один из терминальных объектов хода, сессии, снимка, запуска и результата не изменён | безопасная непривязанная версия; `OutboxEvent` отсутствует | внесение отказа после каждого SQL-оператора подтверждает полный откат |
| Фиксация успешна, подтверждение HTTP потеряно | терминальный ход, новые снимок, архив, сессия, запуск, результат и одно намерение видимы вместе | архив привязан; один `OutboxEvent` в состоянии `pending` | повтор завершения возвращает сохранённый результат и не создаёт версию или намерение заново |
| Устаревшее завершение до загрузки | все перечисленные объекты неизменны | архив и `OutboxEvent` отсутствуют | старые владелец аренды, ограждение, версия и попытка получают `stale_claim` |
| Ограждение устарело после загрузки | все бизнес-объекты PostgreSQL неизменны | версия остаётся непривязанной; `OutboxEvent` отсутствует | финальный CAS отклоняет исполнителя; непривязанная версия проходит сверку и сборку мусора |
| Следующий захват после фиксации | видит только новую версию сессии, снимок, `codex_session_id` и ссылку на архив | доставка может быть `pending`, что не блокирует чтение снимка | два соединения PostgreSQL: захват не может увидеть терминальный ход со старым снимком |

### 4.4 Атомарное намерение обратного вызова

Возврат делегирования не создаёт и не меняет ход до фиксации права на обратный вызов. Одна локальная транзакция:

1. блокирует запись делегирования;
2. проверяет терминальный результат и хеш полезной нагрузки повтора;
3. переводит делегирование в `callback_pending`;
4. создаёт ровно одно намерение команды и `OutboxEvent` с ключом `delegation.callback.v1:<delegation_id>`;
5. сохраняет результат идемпотентной операции.

Обработчик с тем же детерминированным ключом либо один раз добавляет обратный вызов к одному ходу `queued` через CAS версии, либо создаёт новый ход, затем сохраняет `callback_turn_id` в собственной транзакции. Повтор возвращает тот же результат. Другая полезная нагрузка после первого принятого обратного вызова даёт конфликт и не изменяет промпт.

Тесты: два конкурентных обратных вызова; отказ до или после транзакции намерения; отказ после материализации хода до фиксации результата обработчика; перезапуск; проигравший повтор не создаёт второй промпт или ход и получает ссылку на первый результат.

### 4.5 Локальный исходящий журнал и внешняя доставка Mattermost

Репозиторий не доказывает наличие у Mattermost механизма идемпотентности или надёжного чтения результата по пользовательскому ключу. Поэтому для внешнего результата **не заявляется семантика `exactly-once`**.

Доказуемый контракт:

- терминальный переход и уникальный локальный `OutboxEvent` создаются в одной PostgreSQL-транзакции;
- ключ вида `mattermost.turn-result.v1:<turn_id>:<result_version>:<destination>` уникален локально;
- один обработчик, защищённый ограждением, выполняет доставку `at-least-once`; автоматический повтор с ограниченной экспоненциальной задержкой и случайным разбросом разрешён только для доказанно неотправленных `pending|retry_wait`;
- состояния: `pending|claimed|retry_wait|delivered|ambiguous|dead_letter`;
- успешный `post_id`, число попыток, доказательство `not_sent` или причина неоднозначности, безопасный код ошибки и идентификатор корреляции сохраняются;
- `ambiguous` немедленно карантинируется и автоматически не повторяется; при доказанном чтении результата у поставщика выполняется сверка, иначе требуется ручное решение оператора;
- состояния очереди необработанных событий `dead_letter` и карантина `ambiguous` доступны оператору и процедуре сверки;
- в аварийном режиме обработчик останавливается, но локальное намерение, аренда, эпоха и состояние сверки сохраняются.

| Окно отказа | Доказуемый результат |
| --- | --- |
| До внешнего вызова | намерение остаётся `pending` либо доказанно неотправленным `retry_wait` и безопасно повторяется; внешнего эффекта нет |
| Подтверждённый отказ до отправки | сохраняются доказательство `not_sent` и `retry_wait`; после задержки разрешён автоматический повтор |
| После подтверждённого ответа и локального подтверждения | `delivered`, повтор не выполняется |
| После внешнего побочного эффекта, но до локального подтверждения | `ambiguous` и карантин; автоматический повтор запрещён, потому что он может создать внешний дубль |
| Истечение срока, когда поставщик мог принять запрос | `ambiguous` и карантин; обязательны доказанное чтение результата у поставщика либо ручная сверка |

Сверка использует чтение результата у поставщика только после отдельного тестового доказательства. Без него оператор сверяет целевое обсуждение, метаданные корреляции и идемпотентности и локальный исходящий журнал, затем помечает намерение `delivered` либо вручную разрешает повтор с зафиксированным риском дубля. Перевод `ambiguous` обратно в автоматический цикл по умолчанию запрещён. Ручная проверка специально воспроизводит отказ до и после побочного эффекта и подтверждает отсутствие потери локального намерения; возможный внешний дубль при ручном повторе фиксируется как ограничение, а не скрывается.

## 5. Обязательные тестовые шлюзы

### 5.1 Тестовый контур PostgreSQL как результат PR-1

PR-1 реализует команды ниже согласно [действующему руководству PostgreSQL](../design-guidelines/go/infrastructure_integration_requirements.md) и [единой инструкции тестовых контуров](../guides/go-test-contours.md):

- `make test-go` — только герметичные модульные и компонентные тесты, без PostgreSQL, Docker и тестовых DSN;
- `make test-go-postgres` — обязательный режим тестов репозиториев, миграций и конкурентности, последовательно выполняемый на PostgreSQL 15 и 16; использует имена вида `MATTERCODEX_*_TEST_DATABASE_DSN`, не печатает значения и завершается явным исходом `PASS`, `FAIL` или `NOT RUN`, а не `t.Skip`;
- `make test-go-all` — последовательно выполняет оба контура и не маскирует отсутствие PostgreSQL;
- временный PostgreSQL в Kubernetes — основной путь удалённого агента; Docker допустим только как локальный резервный вариант.

Тестовый контур применяет миграции с нуля и отдельно обновляет копию предыдущей схемы с репрезентативными данными сессий, ходов и делегирования. Для тестов конкурентности открываются минимум два независимых соединения или транзакции. Промышленный DSN запрещён. Наличие целей не является доказательством их выполнения: PR-1 объявляет PostgreSQL-проверку успешной только после фактического запуска обеих версий на точном SHA.

PR-1 также создаёт обязательную оснастку сценария #51: управляемую недоступность MatterCodex MCP до первых Codex-событий, наблюдение состояния хода, управляемое восстановление зависимости и проверку, продолжил ли тот же ход выполнение. В PR-1 обычная обязательная проверка без `t.Skip`, ожидаемого падения и скрытого повтора фиксирует фактический исход базовой версии: ход завершается как `failed` и после восстановления зависимости автоматически не продолжается. При реализации #51 эта же оснастка и набор обязательных тестов меняют ожидание на целевой восстановительный исход; добавление тихого пропуска вместо изменения ожидания запрещено.

### 5.2 Матрица характеристических и последующих приёмочных проверок

Строка текущего долга #51 входит в обязательный зелёный критерий PR-1. Следующая за ней строка целевого восстановления применяется только к отдельной реализации #51 после первых пяти PR.

| Шлюз | Минимальный набор |
| --- | --- |
| Транспорт и удостоверение | отсутствующая, недействительная, повторно воспроизведённая или просроченная возможность доступа; подменённые субъект, канал или пост; запрещённые источник, IP, DNS и перенаправление для SSRF; `401/403`, ноль побочных эффектов в БД, Kubernetes и Mattermost; снимок публичных маршрутов |
| Маршрутизация | неизвестный канал, человек или бот, `#notrigger`, обсуждение и роль по умолчанию, `N` целей при нескольких упоминаниях |
| Квитанция и разветвление | последовательный и конкурентный повтор, порядок целей, конфликт полезной нагрузки, отказ после каждой локальной стадии |
| Сессия | FIFO, захват двумя исполнителями, потерянный ответ, повторный захват после истечения аренды, устаревшее завершение, перезапуск, CAS остановки и завершения; загрузка и проверка архива, откат транзакции завершения, сверка непривязанной версии и следующий захват с новым снимком |
| Сессия в PR-1: характеристика текущего долга #51 | Обязательный MatterCodex MCP недоступен до появления Codex-событий → фактический исход базовой версии `failed` и отсутствие автоматического продолжения того же хода после восстановления зависимости фиксируются обычной обязательной зелёной проверкой без пропуска или ожидаемого падения; этот исход является известным риском, а не целевым контрактом |
| Надёжность #51: обязательный шлюз реализации и приёмки после PR-0…PR-4 | На оснастке PR-1 ожидание меняется: при недоступности обязательного MatterCodex MCP ход сохраняет `queued` или переходит в `retry_wait`, после восстановления зависимости выполняется и не становится необратимо `failed`; проверка обязательна для #51, не входит в зелёный критерий PR-1 и не может быть заменена тихим пропуском |
| Обратный вызов | конкурентный возврат, отказ и перезапуск, детерминированный ключ обработчика, один результирующий ход |
| Доставка | уникальный локальный `OutboxEvent`, автоматический повтор только доказанно неотправленных `pending\|retry_wait`, очередь `dead_letter`, карантин `ambiguous`, отказ до или после побочного эффекта, чтение результата у поставщика при доказанной поддержке либо ручная сверка, аварийная остановка обработчика без потери намерения |
| Хранение | активная работа, `queued`, согласование, обратный вызов, отсутствие архива, ошибка архива, период отсрочки, неизвестное состояние PostgreSQL или S3 — удаление запрещено |
| Полномочия | снимки имён переменных, подключений, ServiceAccount, RBAC, NetworkPolicy и возможностей поставщика; отказ по умолчанию и отсутствие расширения полномочий |
| Совместимость | миграции с нуля и обновление, `N-1/N`, фактический исполняемый файл `S3b` на данных S4, ограждение обработчика, дренирование, откат и повтор события поставщика без второй команды или хода |

Все отказные тесты проверяют не только состояние HTTP и результат, но и ноль побочных эффектов в БД, Kubernetes и Mattermost.

### 5.3 Матрица синтетических секретов

Используются только уникальные синтетические контрольные значения для классов OpenAI, GitHub, Mattermost, Kubernetes, PostgreSQL DSN и токенов сессии/MCP. Действующие значения из переменных среды, Secret или `.env` не читаются.

Каждый класс контрольных значений пропускается через все релевантные каналы, после чего проверяется отсутствие **значения**, а не имени ключа:

| Канал | Что проверяется |
| --- | --- |
| Сформированные промпт и конфигурация | `AGENTS.md`, Codex `config.toml`, конфигурация поставщика, разрешающий список переменных среды и манифест не содержат контрольное значение |
| Структурированные журналы | обычные журналы и журналы ошибок, поля и контекст стека не содержат контрольное значение |
| Ошибка, итог и состояние | типизированная ошибка, итоговое сообщение, ход работы и карточка состояния не содержат контрольное значение |
| Полезная нагрузка Mattermost | ответ поста, обновления, диалога или действия не содержит контрольное значение |
| Аудит | действие, результат, корреляция и безопасные метаданные не содержат контрольное значение |
| Артефакты и архив | карта артефактов, JSONL, хвост stderr, снимок сессии и метаданные архива не содержат контрольное значение за пределами специально зашифрованного файла поставщика, который не публикуется |
| Отрендерованный YAML | Deployment, Pod, ссылки на Secret и диагностический результат не содержат контрольное значение; допустимы только имена Secret, ключей и переменных среды |

## 6. Эволюционная миграция и совместимость

### 6.1 Правила переключения

- Смена владельца схемы, владельца транспорта и поведения выполняется разными контрольными точками.
- `event_path=legacy|durable`, `delivery_path=legacy|outbox` и `transport_owner=legacy|typed` — отдельные взаимоисключающие переключатели. Одновременно активен ровно один владелец записи и один обработчик каждого эффекта.
- Переключатели и `consumer_epoch` хранятся серверно; промпт и локальная память процесса не являются источником истины.
- Каждый обработчик требует аренду или лидера и эпоху ограждения. Процесс со старой эпохой не создаёт побочный эффект.
- Перед переключением или откатом приём приостанавливается, выполняемая и захваченная работа дренируется либо явно возвращается в `pending`, затем меняется эпоха.
- `contract` выполняется позже, когда нет старых читателей/писателей и принят отдельный план отката.
- `N-1` ниже означает непосредственно предыдущую принятую контрольную точку, а не произвольный старый исполняемый файл. Для S4 это ровно `S3b bridge-ready`, финальная исполняемая контрольная точка PR-3 с совместимым адаптером чтения и устранения повторов; её SHA исходного кода и дайджест образа фиксируются в отчёте приёмки PR-3. Нельзя перескочить эту контрольную точку и затем требовать безопасный откат на более старую версию.
- Решение 5A делает `S3b bridge-ready` обязательным результатом PR-3 и единственной поддерживаемой границей отката S4. `S4-expand-reader` сохраняется только как рассмотренная неактивная альтернатива и не является основной стратегией.
- Решение 4B сохраняется при каждом переключении и откате: `ambiguous` не возвращается в автоматический цикл, а `pending|retry_wait` повторяется автоматически только при сохранённом доказательстве отсутствия отправки.

Перед каждым поведенческим переключением новый исполняемый файл сначала развёртывается с новым путём выключенным. После проверки он становится поддерживаемым `N-1` для следующей контрольной точки.

### 6.2 Состояния

| Состояние | Главное измерение | Внешнее поведение |
| --- | --- | --- |
| `S0 baseline` | текущий код | подтверждённые риски существуют; структурные PR запрещены |
| `S1 containment` | PR-0/PR-1/PR-2: 1A — кластерный обратный вызов и одноразовая возможность доступа; 3A — заморозка новых `cluster-admin` назначений; 6A — проверенный субъект и точка сопряжения допуска без схемы #66; тестовый контур; 2A — разрушительное удаление и quota cleanup выключены | полезная маршрутизация сохранена; опасные обратные вызовы, новые широкие назначения, неопределённый допуск и очистка закрыты по умолчанию |
| `S2 expand` | только добавочная схема для аренды, идемпотентности, намерения обратного вызова, квитанции, исходящего журнала и переключателей | все пути остаются `legacy`; обработчики новых записей выключены |
| `S3a durable behavior` | PR-3: аренда, ограждение, CAS, единая транзакция завершения, намерение обратного вызова, переключение доставки на исходящий журнал и 4B — карантин `ambiguous` с повтором только доказанно неотправленных записей | транспорт и DTO маршрутизации прежние; меняется только надёжность среды выполнения и доставки, неоднозначность не возвращается в автоматический цикл |
| `S3b bridge-ready` | обязательная по 5A финальная контрольная точка PR-3: S3a плюс включённый в унаследованный входной путь адаптер совместимости для чтения и устранения повторов будущих квитанций, команд и результатов S4, точные SHA и дайджест образа | `event_path=legacy`; адаптер только читает чужие записи S4 при откате или повторе и не создаёт их на обычном трафике S3 |
| `S4 inbound durable` | PR-4: конверт, квитанция и переключение команд `1 -> N`; по 6A только области установки, рабочей области и сессии, проверенный субъект и точка сопряжения допуска; по 5A откат только на точный S3b | тот же транспорт WebSocket/HTTP; один долговечный владелец записи и обработчик; схема #66 отсутствует |
| `S5 structural seams` | последующие узкие порты репозиториев, транспорта и среды выполнения | данные и поведение S4 не меняются |
| `S6 contract` | удаление унаследованных путей и возможное физическое выделение | не входит в первые пять PR и требует отдельной приёмки |

### 6.3 Матрица `N-1/N` для S2/S3a/S3b/S4

| Состояние | `N-1` | `N` | Единственный владелец записи/обработчик | Развертывание/переключение | Откат/повтор |
| --- | --- | --- | --- | --- | --- |
| `S2 expand` | Читает и пишет старые таблицы, игнорирует добавочные таблицы и поля, допускающие NULL | Применяет только добавочную миграцию, но работает с переключателями `legacy` | унаследованный владелец записи; новые обработчики выключены; миграции запускает одно задание или процесс | Сначала миграция, затем последовательное обновление до N; новых побочных эффектов нет | Остановить N и вернуть N-1; схема остаётся. Новые долговечные очереди пусты, повтор не нужен |
| `S3a durable behavior` | Предыдущий исполняемый файл с поддержкой расширенной схемы знает переключатели и новые записи и при чужой эпохе не выполняет побочные эффекты обработчика | Поддерживает аренду, CAS, завершение, обратный вызов и исходящий журнал | после атомарного переключения только обработчик среды выполнения и доставки N, защищённый ограждением; владелец записи транспорта прежний | Развернуть N с переключателями `legacy`, убедиться, что все экземпляры их понимают; остановить приём завершений и обратных вызовов, дренировать старые захваты и прямые доставки, выдать новую эпоху, включить `delivery_path=outbox` | Приостановить новые терминальные команды и обратные вызовы, оградить N, дождаться отсутствия захватов; доказанно неотправленные `pending\|retry_wait` доставить либо оставить для безопасного повтора, `ambiguous` карантинировать, вернуть `legacy`, затем N-1. Старые аренды не принимаются, ходы `queued` читаются N-1; локальных дублей и потерь нет |
| `S3b bridge-ready` | образ `S3a` | Добавляет адаптер совместимости для чтения и устранения повторов в унаследованный входной путь; `event_path=legacy`, долговечные владелец записи и обработчик выключены | унаследованный входной владелец записи + адаптер только для чтения; обработчик исходящего журнала доставки S3 остаётся единственным | Развернуть точный финальный исполняемый файл PR-3 с адаптером, выполнить тест фактического исполняемого файла и зафиксировать SHA/дайджест; обычный трафик не пишет квитанции и команды S4 | Можно вернуть S3a только до первого переключения S4. После появления данных S4 поддерживаемым N-1 становится только этот исполняемый файл S3b |
| `S4 inbound durable` | Только точный `S3b bridge-ready` из PR-3; читает идентичность, хеш и допуск квитанции, неизменяемый снимок целей, состояние команды, идентификаторы результирующих ходов и сохранённый результат идемпотентной операции | Пишет одну квитанцию и `N` намерений команд, обработчик материализует существующие строки сессий и ходов | после переключения только входной владелец записи и обработчик команд N, защищённые ограждением; транспорт всё ещё `legacy`; у исходящего журнала доставки есть отдельный единственный обработчик S3/S4 | Сначала развернуть N с `event_path=legacy`; остановить лидера обработчика событий, дренировать обратные вызовы поставщика и захваченные команды, выдать новую эпоху, включить `event_path=durable`, затем открыть приём | Выполнить протокол §6.4 и вернуть только точный S3b. Адаптер для найденной квитанции возвращает сохранённый результат и никогда не вызывает унаследованное создание; идентичность без квитанции идёт в унаследованный путь |

Для S3a/S3b/S4 откат запрещён как простая замена Pod. Если дренирование, ограждение и сверка не завершены, откат считается небезопасным и останавливается. Внешняя доставка Mattermost остаётся `at-least-once`: матрица гарантирует отсутствие второго **локального** намерения, но не скрывает возможный внешний дубль после побочного эффекта до подтверждения.

Владелец транспорта в S2–S4 не меняется. `transport_owner=typed` переключается только в S5 отдельным PR после доказанного паритета, поэтому владение, транспорт и смена поведения не смешиваются.

### 6.4 Реализуемая граница отката S4

По принятому решению 5A адаптер совместимости обязателен уже в PR-3. Поддерживаемый `N-1` для S4 имеет точное логическое имя `S3b bridge-ready`: это финальные принятые SHA исходного кода и дайджест образа PR-3, а не любой совместимый с S3 исполняемый файл. Переключение S4 без принятой контрольной точки S3b запрещено. Вариант `S4-expand-reader` остаётся в разделе 10 только как история рассмотрения и не входит в действующий план.

Адаптер выполняется в существующем унаследованном входном пути до любого унаследованного создания. По `(provider, provider_event_type, provider_event_id)` он читает:

- идентичность квитанции, канонический хеш полезной нагрузки и `admission=admitted|ignored|rejected|duplicate|deferred`;
- неизменяемый снимок целей;
- для каждой команды состояние `pending|claimed|materialized|terminal|retry_wait|failed`, детерминированный ключ и `resulting_turn_id`, если он уже назначен;
- сохранённый результат идемпотентной операции с безопасными ссылками на квитанцию, команды и ходы.

Если квитанция найдена и хеш, область и субъект совпадают, адаптер возвращает сохранённые допуск и результат и не вызывает унаследованное создание независимо от повторной доставки поставщика. Конфликт хеша, области или субъекта отклоняется с нулём побочных эффектов. Если квитанция отсутствует, точный S3b обрабатывает событие обычным унаследованным владельцем записи. Адаптер не становится вторым обработчиком команд и не материализует команды `pending|claimed`.

Протокол отката S4 → S3b:

1. Закрыть новый приём событий поставщика и дождаться завершения уже принятых обратных вызовов HTTP/WebSocket.
2. Оградить входного владельца записи и обработчик команд S4 новой `consumer_epoch`; старый процесс после ограждения не пишет квитанцию, команду, ход или результат.
3. Аренда захваченной команды либо завершается, либо явно возвращается в `pending` после истечения или отзыва ограждения. Затем обработчик S4 материализует все команды `pending|retry_wait` в совместимые с унаследованным путём ходы и сохраняет `resulting_turn_id` и результат. Если остаётся `pending|claimed|retry_wait` без безопасного результата, откат останавливается.
4. Для исходящего журнала доставки: захваченная аренда обработчика S4 завершается или освобождается; только `pending|retry_wait` с сохранённым доказательством `not_sent` продолжит точный обработчик доставки S3b; `ambiguous` остаётся в карантине только для доказанного чтения результата или ручной сверки, `dead_letter` — для оператора. Удалять или объявлять доставленными эти записи при откате запрещено.
5. Проверить отсутствие активных входных захватов, один терминальный ход или ход `queued` на каждую материализованную команду, сохранённые результаты квитанций и единственных владельца записи и обработчика. Зафиксировать снимок шлюза.
6. Развернуть точные SHA/дайджест S3b с `event_path=legacy`, новой эпохой и закрытым приёмом; выполнить пробы повторного воспроизведения на данных S4. Только после успешных проб открыть приём событий поставщика.

Обязательный тест совместимости не подменяется имитацией или заглушкой. CI собирает или берёт подписанный образ точного SHA `S3b bridge-ready`, поднимает его на схеме и данных PostgreSQL, созданных фактическим исполняемым файлом S4, и повторно доставляет событие поставщика для каждой ветки квитанции и результата, включая несколько упоминаний и потерянное подтверждение поставщика. Проверка утверждает: квитанция остаётся одна, новых команд и ходов нет, снимок целей не меняется, сохранённый результат не теряется и возвращается повтору. Отдельно процесс воспроизводит захваченную команду и захваченную доставку исходящего журнала перед ограждением, выполняет протокол дренирования и возврата в очередь и подтверждает отсутствие потери и второго локального намерения.

## 7. Первые пять реализуемых PR

### PR-0. Сдерживание публичной границы Mattermost

**Результат:** по решениям 1A, 3A и 6A вводятся разрешающий список Ingress; внутренние маршруты, MCP и метрики доступны только внутри кластера; `action`/`dialog` идут через кластерный Service; непрозрачная одноразовая серверная возможность доступа хранится только в виде хеша и связывается с операцией, ресурсом, каналом, постом, сроком и погашением; субъект удостоверяется отдельно от серверной проверки допуска `allowed|denied|indeterminate`, где неопределённость означает отказ; исправимая проверка полей dialog предшествует SSRF-подготовке и финальному погашению; действует политика `response_url` из раздела 2.3. Новые назначения `cluster-admin` заморожены. Только точный снимок уже настроенных полей профиля и роли, влияющих на полномочия, может пройти допуск с аудитом: bot identity, учётные записи OpenAI/GitHub и ссылки на Secret учётных данных, привязки репозиториев проекта и чата, переменные среды выполнения и привязка Kubernetes/Secret сессии. Понижение прав, выключение, блокировка поставщиком и удаление монотонны. Миграция `000025` содержит совместимое ограждение на уровне БД, а `000026` закрепляет глобальный монотонный инвентарь frozen `session_key` и запрещает повторное использование или перепривязку ключа под другой роль, проект, чат, канал или сессию. Поэтому точный N-1 runtime login без маркера writer читает профили и роли и выполняет обычный DML, но не может расширить или безусловно восстановить frozen state, а старый `CreateChat` не восстанавливает отозванную привязку. Полные `Organization`, `Membership`, `IntegrationGrant`, права и квоты не создаются.

**Автоматические проверки:** отрицательная матрица хеша возможности доступа, срока, погашения, операции, ресурса, канала, поста, субъекта, допуска, повторного воспроизведения, исправимой field validation и SSRF с нулём побочных эффектов; разрешение только точному снимку уже настроенного `cluster-admin`, отказ новому, изменённому или повторно включённому назначению и аудит исходов; двухсоединенческие barrier-тесты revocation против runtime guard и `CreateChat`; запуск exact N-1 repository/executable на схеме `000026` без расширения frozen state; PostgreSQL-регрессия `freeze -> delete/revoke -> reinsert/rebind` для frozen `session_key`; снимки зарегистрированных и опубликованных маршрутов; отрендерованный Ingress; существующие контрактные тесты `slash`/`action`/`dialog`/GitHub.

**Ручная проверка:** легитимная цепочка `slash -> action -> dialog` работает через кластерный обратный вызов; подделанный или повторный обратный вызов отклоняется; с внешнего адреса доступны только разрешённые пути, а внутренние маршруты, MCP и метрики недоступны; уже настроенный административный профиль проходит серверный допуск, а новое назначение `cluster-admin` отклоняется и видно в аудите.

**Ограничение:** PR не переносит транспортные пакеты, не вводит подписывающий прокси или плагин, не расширяет полномочия и не реализует полную #66. Отключение `action`/`dialog` является только аварийным сдерживанием, а не завершённым результатом PR-0. PR-0 остаётся блокирующим условием любого следующего слияния или развертывания волны.

### PR-1. Обязательный тестовый контур и характеристические доказательства

**Результат:** создаются `make test-go`, `make test-go-postgres`, `make test-go-all`; обязательный режим PostgreSQL с `MATTERCODEX_*_TEST_DATABASE_DSN`; test-only lifecycle принимает только одноразовую database и до `CREATE` сохраняет неизменяемое резервирование identity. При ошибке до exact applied marker target сохраняется без adoption/`DROP`; после marker ограниченный destructive cleanup выполняется только в generated private cluster, а внешний/shared endpoint явно передаёт очистку владельцу. Lifecycle единообразно сериализует глобальную для database установку `vector` в `public`, создаёт отдельные схемы пакетов с `public` в `search_path` и никогда не удаляет extension при schema cleanup; миграции с нуля и обновление копии; минимум два соединения; контур внесения отказов в маршрутизацию, сессии, обратные вызовы и доставку; полная матрица синтетических секретов. Для #51 создаётся обязательная оснастка сценария недоступности MatterCodex MCP до первых Codex-событий. Зелёная характеристическая проверка PR-1 утверждает фактический исход базовой версии: ход завершается как `failed`. Проверка не использует `t.Skip`, ожидаемого падения или скрытого повтора и прямо отмечает текущий долг #51. Оснастка позволяет задаче #51 изменить ожидание на `queued` или `retry_wait`, выполнение после восстановления зависимости и отсутствие необратимого `failed` без тихого пропуска.

**Автоматические проверки:** все цели из раздела 5.1; явный тест, что обязательный режим PostgreSQL не пропускается молча; post-`CREATE` fault matrix для точного/неоднозначного `CREATE`, отсутствующего/применённого `COMMENT`, derive/final admission/cancellation, transient reconnect, ambiguous `DROP`, deadline, concurrent cleanup и proof race; отдельный database-count control на явном loopback/external-style endpoint; fail-closed wrong server/name/owner/OID/marker/registry и replacement test без `DROP`; чистая database без `vector`, конкурентная повторная установка через default target без внешнего `GOFLAGS`, единственный extension в `public` и доступный type `vector`; обязательная характеристическая проверка текущего исхода `failed` для сценария #51 и проверка отсутствия `skip`/ожидаемого падения в этом сценарии; снимок уже настроенных `cluster-admin` профилей и назначений, отказ новым назначениям и наличие аудита без расширения полномочий; проверка `front matter`, уникальности `id`, относительных ссылок и архитектурных импортов.

**Ручная проверка:** владелец видит отдельные исходы «герметичные тесты пройдены», «PostgreSQL пройден» либо явное «не запущен/упал»; успешный общий статус невозможен без обязательного PostgreSQL-контура для PR хранения. На явном ephemeral endpoint post-`CREATE` fault до marker сохраняет exact target, consumed proof и согласованный ledger без helper-owned `DROP`; успешный destroy и компенсация после marker также сохраняют database и сообщают владельцу её точное безопасное имя для очистки. Generated private target ограниченно очищается только при повторном доказательстве private authority. Replacement с exact исходным OID/name/owner и пустым comment сохраняется. Отдельно воспроизводится недоступность обязательного MatterCodex MCP до первых Codex-событий: после восстановления зависимости проверка PR-1 проходит, только если наблюдает фактический `failed` и отсутствие автоматического продолжения того же хода, а отчёт называет этот исход текущим долгом #51, а не восстановлением или допустимым целевым поведением.

**Ограничение:** это новый результат PR, а не описание существующих команд. Прикладные схема и поведение не меняются; повтор, переочередивание и состояние `retry_wait` не реализуются. Целевой восстановительный исход является критерием отдельной реализации и приёмки #51.

### PR-2. Запрещающее по умолчанию сдерживание очистки сессий

**Результат:** по решению 2A автоматическое разрушительное удаление PVC сессии и Secret токена сессии выключено; хранение работает как инвентаризация и предварительный просмотр без удаления для сессионных данных; повтор после ошибки квоты не вызывает разрушительную очистку и возвращает типизированную ошибку ёмкости. PR-2 не реализует полный предикат допустимости и не содержит пути включения удаления: любые факты, включая `unknown`, используются только для диагностики и не разрешают разрушительное действие.

**Автоматические проверки:** отрицательная матрица `active|queued|approval|callback|no_archive|archive_failed|grace|unknown_db|unknown_s3`; отдельный тест квоты; повтор инвентаризации; отсутствие удаления PVC/Secret и записи аудита о разрешённом удалении.

**Ручная проверка:** в изолированной тестовой установке создать старый непривязанный тестовый PVC и вызвать предварительный просмотр и сценарий квоты; ресурс остаётся, система показывает безопасную причину и не утверждает наличие архива.

**Ограничение:** полная проверка допустимости по PostgreSQL и S3 и включение автоматической очистки — отдельный результат после PR-4. Вариант 2B не поглощается PR-2. Метки и возраст никогда не дают права удаления.

### PR-3. Транзакционная надёжность сессии, хода, обратного вызова и доставки

**Результат:** добавочная схема и затем отдельное переключение для защиты сессии, последовательности, аренды, пульса, ограждения, CAS состояния и версии завершения или остановки, резервирования завершения, единой PostgreSQL-транзакции терминального хода + снимка сессии/`codex_session_id`/ссылки и контрольной суммы архива + аренды и ограждения сессии + результата запуска и идемпотентной операции + уникального намерения доставки, атомарного намерения обратного вызова и локального исходящего журнала доставки. По решению 4B `ambiguous` карантинируется и не повторяется автоматически; автоматический повтор разрешён только для доказанно неотправленных `pending|retry_wait`, а аварийная остановка обработчика сохраняет локальное намерение. Загрузка, версия и контрольная сумма архива завершаются до фиксации; ошибка загрузки оставляет ход нетерминальным и доступным для повтора, а ошибка фиксации — только безопасную непривязанную версию для сверки и сборки мусора. В S2 также добавляются неактивные схемы квитанции и команды и шлюз `event_path`. По решению 5A финальная контрольная точка `S3b bridge-ready` обязательно добавляет адаптер совместимости для чтения и устранения повторов в унаследованный входной путь без записи обычного трафика S3 и фиксирует точные SHA и дайджест образа. Работает один обработчик каждого типа, защищённый ограждением; этапы S2/S3a/S3b обязательны.

**Автоматические проверки:** вся матрица разделов 4.3–4.5, включая внесение отказа до и после загрузки и каждой SQL-стадии, потерянное подтверждение, устаревшее завершение, сверку непривязанной версии и следующий захват с новым снимком; два соединения PostgreSQL; отказ и перезапуск; доказательство `not_sent` для автоматического повтора, карантин и запрет автоматического повтора `ambiguous`, чтение результата у поставщика при доказанной поддержке или ручная сверка, остановка и возобновление обработчика без потери намерения; старый обработчик чтения на расширенной схеме; `N-1/N` S2/S3a; оснастка с точным фактическим исполняемым файлом и адаптером `S3b`; проверка запрета `replicas >= 2`.

**Ручная проверка:** два безопасных исполнителя одновременно запрашивают одну сессию; выполняется один ход. Затем имитируются ошибка загрузки архива, ошибка фиксации БД после загрузки, потеря ответа завершения и недоступность Mattermost: до фиксации нет терминального состояния; после фиксации новые снимок, архив, запуск, результат и один локальный `OutboxEvent` видимы вместе; следующий захват получает новый снимок; доказанно неотправленная доставка повторяется, а неоднозначная остаётся в карантине до чтения результата или ручной сверки. Аварийная остановка обработчика не теряет намерение. Отдельно точный образ S3b читает подготовленную квитанцию S4 и возвращает результат без второго хода.

**Ограничение:** транзакция между PostgreSQL и S3 и внешняя семантика Mattermost `exactly-once` не заявляются. Владение транспортом и репозиториями не переносится.

### PR-4. Квитанция, версионированный конверт и детерминированное разветвление по целям

**Результат:** по решениям 5A и 6A вводятся поля конверта из раздела 4.1; проверенный субъект и точка сопряжения допуска с отказом по умолчанию; только области установки, рабочей области и сессии без схемы полной #66; одна уникальная квитанция на событие поставщика; неизменяемый снимок целей; `N` команд и ходов по целям; типизированный результат допуска; идемпотентные обработчики; этап S4 с откатом только на принятую точную контрольную точку `S3b bridge-ready`.

**Автоматические проверки:** последовательный и конкурентный повтор нескольких упоминаний; конфликт полезной нагрузки; стабильные порядок целей и различитель; отказ после квитанции, команды и обработки; все поля корреляции, причины и идемпотентности; отказ без субъекта, области или права; `N-1/N` S4 и протокол §6.4 с фактическими точными кодом и образом S3b, данными S4 и повторной доставкой поставщика.

**Ручная проверка:** одно сообщение с двумя упоминаниями создаёт одну квитанцию и по одному ходу каждой цели. Последовательный и конкурентный повтор не добавляет квитанцию, цель или ход, а изменение списка ролей после первого приёма не меняет сохранённое разветвление. После ограждения и дренирования S4 заменяется точной контрольной точкой S3b; повторная доставка поставщика возвращает сохранённый результат без второй команды или хода.

**Ограничение:** транспорт WebSocket/HTTP остаётся на месте. `Organization`, `Membership`, `IntegrationGrant`, продуктовые права, grants, квоты и минимальная схема #66 не создаются; полная #66 следует отдельными архитектурным, схемным и поведенческими срезами после PR-4.

## 8. Очередь после первых пяти PR

Только после финального OK владельца и слияния этого архитектурного результата менеджер создаёт отдельные Issues, не смешивая типы результата. Первый ручной шлюз и закрепление пакета `1A/2A/3A/4B/5A/6A` не разрешают создавать их заранее:

1. **Волна 1: узкие репозиторные порты и реестр владельцев миграций.** Интерфейсы, определённые потребителями, один `pgxpool`, один владелец схемы, запрет новых импортов `admin.Repository`; зависит от PR-1, PR-3 и PR-4.
2. **Волна 1: транспорт Mattermost и восстановление обработчика событий.** DTO → версионированная команда, прежние URL/карточки, повторяемый `BotUserID`, диагностическое состояние #59; зависит от PR-0 и PR-4.
3. **Волна 1: порт среды выполнения и локальные пакеты `agent-runner`.** Матрица возможностей, типизированный допуск, снимок PodSpec/RBAC/NetworkPolicy, без расширения полномочий; зависит от PR-2–PR-4.
4. **Надёжность #51 — отдельная задача после первых пяти PR и обязательная зависимость до любого физического разделения очереди и `runtime-controller`.** Классы временных ошибок, ограниченные повторы с задержкой, лидер для всех циклов. Реализация использует обязательную оснастку PR-1 и меняет её ожидание с характеристики текущего `failed` на целевой шлюз приёмки: если обязательный MatterCodex MCP недоступен до появления Codex-событий, ход сохраняет `queued` или переходит в `retry_wait`; после восстановления зависимости он выполняется и не становится необратимо `failed`. Тихий пропуск проверки запрещён. Только после принятия #51 и этого шлюза допустимы физическое разделение и `replicas >= 2`.
5. **Хранение: доказанная допустимость по PostgreSQL и S3 и включение очистки.** Полный закрытый при неопределённости предикат, контрольная сумма и ссылка архива, отсрочка, блокировка хранения, аренда, аудит, отчёт предварительного просмотра и ручной шлюз включения; отдельный результат после PR-4, зависит от ADR-MC-006 и PR-2 и не поглощается PR-2.
6. **Безопасность Kubernetes: узкий управляемый MCP для административных операций.** Выбранное целевое состояние решения 3A реализуется отдельным будущим PR: минимальные Role/ClusterRole, серверное право, согласование, идемпотентность, аудит и переключение без одновременной работы прямого и управляемого путей. PR-0…PR-4 сохраняют только замороженные уже настроенные прямые профили и не реализуют этот результат.
7. **Инфраструктура #58: воспроизводимые настройки ботов Mattermost.** Отдельный результат SRE без переноса транспорта.
8. **Инструкции #60:** версии исходных данных, контрольная сумма, расхождение и управляемое обновление без молчаливой перезаписи.
9. **Учётная запись поставщика #61:** безопасное обозначение, неизменяемая привязка сессии и `RuntimeRevision` по ADR-MC-004.
10. **Авторизация #66:** после стабилизации конверта и точки сопряжения допуска отдельные архитектурный, схемный и поведенческие срезы для `Organization`, `Membership`, `IntegrationGrant`, продуктовых прав, grants и квот. Минимальная схема #66 до PR-4 запрещена решением 6A.

Неактивные варианты 1B/1E, 2B/2E, 3B/3E, 4A/4E, 5B/5E и 6B/6E не образуют скрытых элементов очереди. Из выбранного пакета отдельную будущую работу создают только явно отложенные результаты: полный предикат и включение очистки после PR-4 по 2A, узкий управляемый MCP по 3A и срезы #66 по 6A. Изменение остальных активных границ требует нового решения владельца до постановки задачи.

Физическое разделение очереди и `runtime-controller`, включая выделение `runtime-controller` в отдельный процесс, начинается только после задач 1–4. Задача #51 и её целевой шлюз реализации и приёмки блокируют это физическое разделение и `replicas >= 2`, но не блокируют документационную работу или общий dogfooding. `interaction-gateway`, `control-plane`, `integration-gateway` и `automation-scheduler` не создаются в первых пяти PR.

## 9. Основные риски и ограничения

| Риск | Шлюз |
| --- | --- |
| Подделанный обратный вызов/SSRF | PR-0: обязательный кластерный обратный вызов 1A, одноразовая серверная возможность доступа с хешем, сроком, погашением и точными привязками, отдельно проверенные субъект и допуск; отключение маршрутов — только аварийное сдерживание |
| Тихо пропущенные тесты PostgreSQL | обязательная цель PR-1; PR хранения без неё не принимается |
| Потеря состояния PVC или сессии | PR-2 по решению 2A полностью выключает разрушительное удаление и quota cleanup; полный предикат и включение очистки — отдельный результат после PR-4 |
| Два активных хода или устаревшее завершение | PR-3: защита сессии, аренда, ограждение и CAS |
| Двойной промпт обратного вызова | PR-3: атомарное намерение и детерминированный ключ обработчика |
| Потеря или дубль результата Mattermost | уникальный локальный `OutboxEvent` + `at-least-once`; по решению 4B автоматически повторяются только доказанно неотправленные `pending\|retry_wait`, `ambiguous` карантинируется до доказанного чтения результата или ручной сверки |
| Несколько упоминаний потеряны или дублированы | PR-4: `1 квитанция -> N команд/ходов` и неизменяемый снимок целей |
| Несовместимый откат | решение 5A: обязательная точная контрольная точка `S3b bridge-ready`, фактический исполняемый файл, единственные владелец записи и обработчик, эпоха, ограждение, дренирование и повторное воспроизведение; S4 откатывается только на неё |
| Расширение `cluster-admin` | решение 3A: новые назначения заморожены; уже настроенные профили требуют явно заданных на сервере права профиля и допуска с аудитом; узкий управляемый MCP — отдельный будущий PR |
| Расширение иных полномочий | снимки матрицы `direct`/`managed`, отказ по умолчанию, отдельный типизированный допуск и запрет расширения в PR-0…PR-4 |
| Случайное включение #66 | решение 6A: только области установки, рабочей области и сессии, проверенный субъект и точка сопряжения допуска; `Organization`, `Membership`, `IntegrationGrant`, grants и квоты остаются отдельными срезами после PR-4 |

## 10. Принятые на первом ручном шлюзе решения владельца

2026-07-17 владелец на первом ручном шлюзе явно принял пакет `1A/2A/3A/4B/5A/6A`. Варианты A, B и аварийный/переходный вариант сохранены ниже как история рассмотрения и основание последствий, но активен только вариант в колонке «Активный выбор». Это предварительное принятие решений не меняет общий статус документа `proposed`, не подтверждает выполнение будущих PR или их проверок и не заменяет следующий полный проход рецензента и финальный OK владельца.

| Решение | Вариант A | Вариант B | Аварийный/переходный вариант | Активный выбор | Последствия для PR-0…PR-4 | Безопасность и эксплуатация | Миграция и откат | Будущая #66 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Механизм обратного вызова `action`/`dialog` | Кластерный обратный вызов + непрозрачная одноразовая возможность доступа с привязками из §2.2 | Доверенный прокси или плагин Mattermost подписывает обратный вызов; `bot-service` проверяет подпись, одноразовое значение, срок действия, субъекта и те же привязки | Выключить маршруты `action`/`dialog` и оставить `slash` или текстовый резервный путь; это сдерживание, а не завершение PR-0 | **1A — принят 2026-07-17 на первом ручном шлюзе:** минимальный механизм под контролем MatterCodex, без новой зависимости | A — PR-0 добавляет хранение и погашение возможности доступа, PR-1 — тесты; PR-2…PR-4 без перестановки.<br>B — PR-0 включает контракт и развертывание плагина или прокси и может разделиться на два PR; остальные ждут его.<br>E — PR-0 может временно закрыть доступ, но функциональный PR-0 остаётся незавершённым и блокирует развертывание PR-1…PR-4 | A — повторное воспроизведение и субъект контролируются сервером, нужна очистка просроченных записей.<br>B — добавляются граница доверия, ротация ключей и эксплуатация прокси или плагина.<br>E — минимальная поверхность, но потеря сценария UI | A — добавочная таблица, простой откат с отключением маршрута.<br>B — двухкомпонентный откат и совместимость версий ключей и подписей.<br>E — обратное включение только после A/B | A/B сохраняют точку сопряжения проверенного субъекта без таблиц `Organization`; права развивает #66.<br>E не решает модель субъекта и прав |
| Сдерживание хранения | Выключить разрушительное удаление PVC/Secret сессии и очистку по квоте; оставить инвентаризацию и предварительный просмотр до доказанной допустимости по PostgreSQL и S3 | Реализовать полный закрытый при неопределённости предикат БД и S3, контрольную сумму и ссылку архива, отсрочку, блокировку хранения, аренду, аудит и ручное включение уже в PR-2 | Полностью остановить циклы хранения и вытеснения по квоте и возвращать ошибку ёмкости до отдельного PR хранения | **2A — принят 2026-07-17 на первом ручном шлюзе:** минимально прекращает потерю данных и сохраняет PR-2 ограниченным | A — PR-2 остаётся сдерживанием; полное включение после PR-4 отдельным PR.<br>B — PR-2 поглощает часть хранения и ADR-MC-006 и требует PostgreSQL-контура PR-1; PR-3/PR-4 сдвигаются.<br>E — малый срочный патч до PR-2, но не заменяет его инвентаризацию и диагностику | A — рост хранилища контролируется инвентаризацией и предупреждениями.<br>B — меньше ручной работы, но выше риск ошибки разрушительного предиката.<br>E — быстрый рост PVC и раннее исчерпание квоты | A/E — откат прост: удаление остаётся выключенным; миграций удаления нет.<br>B — включение только после сравнения предварительных отчётов и возможность отката в выключенное состояние; удалённые данные нельзя восстановить без резервной копии | Не зависит от схемы `Organization`; будущая политика может добавить область организации. B преждевременно связывает хранение с ещё не принятой политикой #66 |
| Прямой `cluster-admin` | Заморозить новые назначения; оставить только уже настроенные профили, добавить явно заданные на сервере право профиля и допуск с аудитом до управляемой миграции | Удалить прямой `cluster-admin` из Pod агента; операции записи Kubernetes выполнять только через `managed` MCP с узкими Role/ClusterRole и согласованием | Отключить все профили `cluster-admin` и `ClusterRoleBinding`; административные операции вручную выполняет владелец вне агента | **3A — принят 2026-07-17 на первом ручном шлюзе:** заморозить прямой путь в волне 1; узкий `managed` MCP является выбранным целевым состоянием отдельного будущего PR | A — PR-0 вводит заморозку, явно заданные на сервере право профиля и допуск с аудитом; PR-1 фиксирует снимок и отрицательные тесты; PR-2…PR-4 не расширяют привязку; отдельный будущий PR готовит управляемый путь.<br>B — нужны шлюз интеграции, RBAC и согласование раньше порта среды выполнения, что расширяет и переставляет PR-3/PR-4.<br>E — отдельный PR сдерживания до PR-0 возможен, административный процесс временно недоступен | A — сохраняется большой радиус поражения, требуются ручной шлюз и аудит; промпт не является контролем.<br>B — учётные данные остаются в шлюзе, действуют минимально необходимые права и согласование.<br>E — наименьший риск платформы, наибольшая ручная эксплуатационная нагрузка | A — откат к прежнему PodSpec только с тем же замороженным субъектом; новые назначения запрещены.<br>B — одновременная работа двух путей запрещена; переключение после доказательства паритета, откат возвращает только замороженный профиль A.<br>E — возврат требует отдельного OK владельца | A использует временное право установки и профиля без полной схемы.<br>B естественно подключается к `IntegrationGrant` #66.<br>E откладывает политику, но не определяет её |
| Неоднозначная доставка Mattermost | Для `ambiguous` разрешить автоматический повтор по принятой политике риска, признавая возможный внешний дубль | Карантинировать `ambiguous`; автоматически повторять только доказанно неотправленные `pending\|retry_wait`, а `ambiguous` решать чтением результата или ручной сверкой | Остановить обработчик доставки, сохранить `OutboxEvent` и временно публиковать оператором после сверки | **4B — принят 2026-07-17 на первом ручном шлюзе:** карантин `ambiguous` и запрет автоматического повтора до доказанного чтения результата или ручной сверки | A — PR-3 реализует переключатель политики и предупреждение о дубле; PR-4 без перестановки.<br>B — PR-3 реализует очередь `ambiguous` и путь оператора; PR-4 без перестановки.<br>E — допустимый режим инцидента PR-3, но не приёмочное значение по умолчанию | A — меньше задержек, выше вероятность дубля.<br>B — нет автоматического дубля из окна неопределённости, но возможна задержка и ручная работа.<br>E — накапливается очередь и нужна наблюдаемость | Во всех вариантах уникальное локальное намерение сохраняется.<br>A↔B меняет только политику повтора; переход на старую версию не удаляет состояние.<br>E возобновляет обработчик с теми же арендой и ограждением | Не зависит от #66; будущая политика может задаваться по организации, каналу и классу риска |
| Граница отката S4 | PR-3 завершается точной контрольной точкой `S3b bridge-ready` с адаптером совместимости для чтения и устранения повторов; S4 поддерживает откат только на этот SHA/дайджест | Не добавлять адаптер в PR-3; внутри PR-4 сначала выпустить отдельный исполняемый файл `S4-expand-reader` с чтением S4, устранением повторов и `event_path=legacy` и поддерживать откат S4 только на него | Закрыть приём и выполнять исправление вперёд на S4; переход на старую версию запрещён до сверки | **5A — принят 2026-07-17 на первом ручном шлюзе:** точный `S3b bridge-ready` является обязательным непосредственно предыдущим N-1 и единственной границей отката S4 | A — расширяет PR-3 адаптером и тестом фактического исполняемого файла; PR-4 переключается после его шлюза.<br>B — PR-3 меньше, но PR-4 делится на контрольную точку чтения и долговечное переключение; порядок PR-0…PR-3 прежний, PR-4 длиннее.<br>E — не меняет область, но не даёт приёмочного отката | A/B требуют хранения точного образа, инструкции для эпохи, ограждения и дренирования и теста повторной доставки поставщика.<br>E требует эксплуатационного окна и способности безопасно держать приём закрытым | A — только S4 → точный S3b по §6.4.<br>B — только долговечный S4 → точный `S4-expand-reader`; PR-3 и исходное состояние после появления данных S4 не поддерживаются.<br>E — только исправление вперёд, состояния `pending\|claimed` сохраняются | Не зависит от #66, но субъект и область квитанции проверяются существующей точкой сопряжения установки; будущая миграция не может менять идентичность события у поставщика |
| Область и очерёдность #66 | В PR-0/PR-4 оставить только области установки, рабочей области и сессии, проверенного субъекта и допуск с отказом по умолчанию; `Organization`, `Membership`, `IntegrationGrant`, права, grants и квоты делать отдельными срезами после PR-4 | До PR-4 добавить минимальные схемы `Organization`, `Membership`, `IntegrationGrant` и вычислитель политик, затем строить на них квитанцию и допуск | Сохранить статическую ссылку на организацию одной установки и запретить новые внешние и административные возможности; PR-4 допускает только явно разрешённые текущие сценарии | **6A — принят 2026-07-17 на первом ручном шлюзе:** стабилизировать конверт и идемпотентность до продуктовой авторизации; минимальная схема #66 до PR-4 запрещена | A — PR-0 создаёт точку сопряжения, PR-1 — тесты, PR-4 — поля конверта; #66 следует после первых пяти PR.<br>B — новый архитектурный, схемный и поведенческий PR между PR-1 и PR-4, PR-4 зависит от него; область волны шире.<br>E — PR-4 ограничен текущей установкой, расширение возможностей блокируется до #66 | A — точка сопряжения с закрытым отказом без полной политики, меньшая поверхность миграции.<br>B — раньше появляется полноценное право, но возрастает риск смешать удостоверение, транспорт и надёжность.<br>E — безопасное ограничение, но нет пользовательского управления правами и квотами | A/E — добавочная будущая миграция и стабильное переназначение ссылки установки.<br>B — нужны заполнение членства и прав и совместимый откат политики и схемы до S4 | A — #66 идёт отдельной последовательностью «архитектура → схема → поведение» после PR-4.<br>B — часть #66 входит в волну и меняет её порядок.<br>E — вся #66 позже, новые возможности запрещены |

## 11. Критерии следующего ручного шлюза и цель полного повторного рецензирования

Первый ручной шлюз завершил выбор вариантов, но не сделал документ финально принятым и не разрешил создание задач реализации. Предложение можно передать владельцу для финального OK и только после слияния — к созданию отдельных задач реализации, когда одновременно выполнены условия:

- рецензент выполнил полный повторный проход нового SHA после закрепления пакета `1A/2A/3A/4B/5A/6A` и подтвердил отсутствие блокирующих замечаний;
- все шесть активных решений согласованно отражены в кратком выводе, PR-0…PR-4, миграции, рисках, последующей очереди и критериях приёмки, а A/B/аварийные варианты однозначно остаются историей рассмотрения;
- PR-0…PR-4 остаются пятью отдельно проверяемыми результатами с обязательными контрактами раздела 7, а структурные порты начинаются только после PR-4 и задач 1–4 раздела 8;
- снимок Role подтверждает точные Secret verbs `create,get,list,update,patch,delete`, причём принятая оговорка разрешает только `patch` для UID/resourceVersion-fenced снятия управляемого finalizer без wildcard, нового ресурса, `apiGroup` или namespace;
- границы `PR-1 current-characterization`, `#51 target-recovery` и `physical split gate`, 10+1 долговечных контрактов, единая транзакция завершения, S3b/S4 и владение данными не ослаблены;
- отсутствие реализации, миграций, развертывания и будущих CI-доказательств указано явно; фактические локальные проверки, состояние GitHub checks и обсуждений относятся к тому же SHA;
- владелец дал финальный OK точной версии `0.6.1` и SHA после полного повторного прохода рецензента.

До финального OK и слияния новые Issues реализации не создаются. Принятие архитектурного контракта не означает принятие будущего PR-0 либо любого другого PR реализации: каждый из них сохраняет собственные автоматические и ручные шлюзы.

Точная цель полного повторного прохода рецензента на новом SHA:

1. повторно проверить все факты по указанным ссылкам на код и SQL;
2. подтвердить, что PR-1 без изменения схемы и поведения обязательной зелёной проверкой фиксирует фактический `failed` как известный долг #51, а не целевой контракт; отдельная #51 после первых пяти PR меняет ожидание оснастки на `queued|retry_wait`, выполнение после восстановления и отсутствие необратимого `failed`; #51 остаётся обязательной зависимостью до любого физического разделения очереди и `runtime-controller`, условие остаётся задачами 1–4, а блокировка не распространяется на документацию и общий dogfooding;
3. проверить решение 1A во всех границах PR-0: кластерный обратный вызов, непрозрачную одноразовую возможность доступа с хешем, сроком, погашением и точными привязками, отдельно удостоверенные субъект и допуск, отклонённый прокси/плагин и аварийное отключение маршрутов только как незавершённое сдерживание;
4. повторно проверить 10+1 контрактов: PR-0, квитанцию и разветвление в `N`, конверт субъекта, захват и CAS, обратный вызов, доставку `at-least-once`, хранение, PostgreSQL-контур, синтетические секреты, полномочия и реплики, единственное владение внешними идентификаторами;
5. проверить сериализацию сессии, аренду, пульс, ограждение, CAS завершения и остановки, единую транзакцию завершения после загрузки архива и атомарное намерение обратного вызова;
6. по решению 5A доказать `N-1/N` для S2/S3a/S3b/S4, обязательный совместимый адаптер на фактическом исполняемом файле и точные SHA/дайджест, единственных владельца записи и обработчика, переключатели, эпоху, ограждение, дренирование, откат только S4 → S3b и повторное воспроизведение; убедиться, что `S4-expand-reader` не стал основной стратегией;
7. проверить решения 2A и 4B: PR-2 не содержит разрушительного удаления, quota cleanup, полного предиката или включения очистки; PR-3 автоматически повторяет только доказанно неотправленные `pending|retry_wait`, карантинирует `ambiguous` и сохраняет намерение при аварийной остановке;
8. проверить решения 3A и 6A: новые `cluster-admin` назначения заморожены, уже настроенные проходят явно заданные на сервере право профиля и допуск с аудитом без расширения; PR-0/PR-4 содержат только области установки, рабочей области и сессии, проверенного субъекта и точку сопряжения допуска, а `Organization`, `Membership`, `IntegrationGrant`, grants и квоты остаются отдельными срезами после PR-4;
9. подтвердить одного владельца каждой таблицы и мутации, отсутствие разделённого владения внешними идентификаторами и отсутствие скрытой реализации будущего управляемого MCP, полной очистки или #66;
10. убедиться, что все шесть решений помечены единственными активными выборами первого ручного шлюза от 2026-07-17, утверждений об их открытости или необходимости повторного выбора нет, а общий статус остаётся `proposed` до финального OK;
11. проверить русскую проектную прозу и убедиться, что точные идентификаторы, пути, команды, API, протоколы, переменные, Secret и канонические состояния сохранены без искажения.

Интеграционные проверки PostgreSQL, отрендерованный Kubernetes и проверки действующей среды не считаются выполненными данным документационным PR. Их наличие здесь — критерии будущих PR реализации, а не заявление об уже полученном доказательстве.
