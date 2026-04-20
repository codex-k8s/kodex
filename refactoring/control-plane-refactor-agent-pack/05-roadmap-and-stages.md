# Дорожная карта и этапы

## Как использовать этот план

Этот план - стартовая версия. До начала кодовых изменений агент обязан скорректировать его по результатам аудита GitHub Issues и активных PR.

Если аудит покажет, что часть работы уже идет или находится ближе к завершению, порядок этапов можно поменять. Но измененный порядок должен быть зафиксирован в документации и Issues.

## Общая стратегия

Идем не по горизонтали, а по вертикальным bounded contexts. Каждый этап должен давать законченный результат:
- свой сервис;
- свой proto;
- свою БД;
- cutover всех вызовов;
- удаление старого пути;
- обновление docs и Issues.

## Этап 0. Аудит репозитория и GitHub Issues

### Цель
Получить реальную стартовую картину до начала рефакторинга.

### Обязательные действия
- собрать список open Issues, draft PRs, active PRs и недавно закрытых архитектурных задач;
- сопоставить каждый элемент одному из bounded contexts;
- выявить конфликты, дубли и устаревшие задачи;
- отметить задачи, которые уже частично реализуют нужный split;
- обновить дорожную карту и создать эпики по этапам.

### Выходы этапа
- актуализированный backlog map;
- таблица "issue -> domain -> phase";
- обновленная версия этого файла;
- обновленный `06-issues-audit-and-sync.md`.

### PR правила этапа
- если аудит оформлен PR'ом, он должен содержать только документацию и Issues updates, без смешивания с большими кодовыми изменениями.

## Этап 1. Подготовка каркаса выноса сервисов

### Цель
Создать технический каркас, который позволит выносить домены без хаоса.

### Задачи
- определить новый layout репозитория для сервисов;
- завести шаблон сервиса: cmd, internal, proto, migrations, README;
- завести правила для gRPC клиентов и proto packages;
- ввести линтеры или guardrails против прямого импорта чужих repository packages;
- описать policy по DB credentials и миграциям;
- обновить общую архитектурную документацию.

### Что нельзя делать
- не создавать долгоживущий shared framework с доменной логикой;
- не откладывать service layout "на потом".

### DoD
- есть шаблон нового сервиса;
- есть документированные правила структуры;
- есть явный список доменов и назначенные owners;
- есть issue tree для последующих этапов.

## Этап 2. Вынос Runtime Deploy Service

### Почему первым
Это изолируемый heavy operational domain со своей очередью, leases и Kubernetes-логикой.

### Scope
- `runtime_deploy_tasks`
- все use cases подготовки окружения
- runtime reuse evaluation
- cancel/stop
- Kubernetes и registry adapters, относящиеся к runtime

### Подзадачи
- создать новый сервис и его БД;
- перенести миграции runtime deploy;
- выделить runtime-specific proto;
- перевести worker и другие потребители на новый gRPC;
- удалить legacy runtime deploy use cases из Control Plane;
- обновить docs и Issues.

### Специальная проверка
В PR должен быть явный список удаленных legacy-файлов и выключенных старых путей вызова.

### DoD
- новый сервис владеет `runtime_deploy_tasks`;
- старый Control Plane не содержит runtime deploy orchestration;
- все вызовы идут в новый сервис;
- документация по runtime потоку обновлена.

## Этап 3. Вынос Interaction Service

### Scope
- `interaction_*`
- callback processing
- expiry
- Telegram adapter и другие каналы

### Подзадачи
- создать новый сервис и его БД;
- перенести interaction state machine;
- перевести worker dispatch и callback path;
- удалить legacy interaction repositories и handlers;
- обновить markdown-документы продукта и эксплуатации.

### DoD
- human-in-the-loop workflow полностью принадлежит новому сервису;
- старый код dispatch/callback удален;
- внешние callback endpoints документированы;
- все Issues по interaction domain синхронизированы.

## Этап 4. Вынос Mission Control Service

### Scope
- `mission_control_*`
- command lease logic
- workspace/timeline/graph
- `change_governance_*` как часть этого этапа, если аудит не покажет отдельную устойчивую границу

### Подзадачи
- создать сервис и БД;
- выделить read API и command API;
- перевести UI/staff consumers и worker command execution;
- удалить старые projections и handlers из Control Plane;
- обновить продуктовую документацию по workspace/governance.

### DoD
- Mission Control и governance projections живут вне Control Plane;
- read-heavy нагрузка больше не смешана с run orchestration;
- все измененные product docs актуальны.

## Этап 5. Вынос Run Orchestrator Service

### Scope
- webhook ingest
- `agent_runs`
- `flow_events`
- `agent_sessions`
- GitHub rate limit waits
- run wait and resume semantics
- runtime errors, если подтверждено их место здесь

### Почему не первым
Это самый связный и центральный контур. Его лучше выносить после того, как тяжелые дочерние operational domains уже отделены.

### Подзадачи
- создать новый сервис и БД;
- перенести lifecycle run;
- перевести worker, agent-runner и edge consumers;
- удалить legacy run orchestration из старого Control Plane;
- обновить docs по run lifecycle.

### DoD
- Control Plane больше не является owner жизненного цикла run;
- run events и sessions принадлежат новому сервису;
- rate limit waits оформлены там же или в явно выбранном owner-сервисе;
- старые RPC и handlers удалены.

## Этап 6. Вынос Platform Admin & IAM и Project Catalog

### Почему последним крупным этапом
Эти домены затрагивают почти все остальные сервисы. Когда остальная карта уже разнесена, проще провести окончательный split административного и каталожного доменов.

### Scope Platform Admin & IAM
- users
- auth
- access checks
- system settings
- административные сценарии

### Scope Project Catalog
- projects
- repositories
- config entries
- tokens
- webhook/preflight repo-level metadata

### Подзадачи
- решить ownership `project_members` через ADR;
- создать обе БД и оба proto;
- перевести все межсервисные запросы на новые APIs;
- удалить staff/project/repository management logic из старого Control Plane;
- обновить admin/product docs.

### DoD
- осталось не больше тонкого edge adapter или ничего;
- административный и каталожный ownership формализован;
- все сервисы получают доступ к users/projects/repos только через gRPC.

## Этап 7. Финальная зачистка

### Цель
Убедиться, что старый Control Plane больше не является скрытым доменным центром.

### Задачи
- удалить остаточные legacy handlers, repositories и configs;
- убрать неиспользуемые proto и env vars;
- удалить устаревшие docs;
- привести индекс архитектуры и продуктовые docs в консистентное состояние;
- закрыть или перепривязать оставшиеся Issues.

### DoD
- нет доменной логики в legacy Control Plane;
- нет shared DB ownership для старых доменов;
- нет устаревших документов, противоречащих новой архитектуре.

## Правило для каждого этапа

В каждом этапе обязательно присутствуют подзадачи четырех типов:
- код и инфраструктура;
- миграция данных и cutover;
- документация;
- актуализация Issues.

Если какого-то из четырех типов нет, этап описан недостаточно и должен быть доработан.
