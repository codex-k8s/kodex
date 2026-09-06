---
id: OPS-PROVIDER-USAGE-1077
title: Авторитетная доступность provider account
type: implementation
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-06
---

# Доступность provider account

Refs #1046, #1077; исходный MVP-UI-19 из #1018. Existing List/GetProviderAccounts
получают optional usageContext; все строки используют одну owner transaction.
Purpose выбирает проверяемый сценарий, но не назначает actor или permission.

| Инициатор и сценарий | Внешний путь / RPC | Authority и источник | Результат / переход |
| --- | --- | --- | --- |
| Администратор читает каталог/учётку | GET provider-accounts / List/GetProviderAccounts | signed actor/org, existing provider.account.view | lifecycle, credential, health, capacity; model/actor-use NOT_EVALUATED |
| Редактор выбирает account/model | те же чтения, CONFIGURE + Agent/model/effort | exact Agent visibility; actual agent.manage, system assistant organization.manage | шесть dimensions, безопасные причины; никаких grants |
| Пользователь проверяет будущий запуск | те же чтения, LAUNCH + Agent | exact Agent visibility и agent.launch; model/overlay/policy из owner runtime configuration | account admission отдельно от operational status |
| Публикация runtime configuration | existing PublishAgentRuntimeConfiguration | existing owner/OCC/idempotency; catalog pins повторно проверяются | immutable configuration; read projection не разрешает mutation |
| Создание Session | existing LaunchRun | existing Agent/Workflow launch; policy selection и catalog binding | queued Session/Run; capacity и UNKNOWN provider health не подменяют submission admission |
| Claim/renew/terminal/expiry | existing runtime worker lifecycle | exact lease/fence/runtime lineage | занятые capacity slots считаются по actual CLAIMED nonexpired leases; query ничего не резервирует |
| Revoke/reauthorize/catalog refresh | existing provider commands/tasks | existing owner credential/observation proof | следующая query использует новую credential/observation; прежний cursor закрыто отвергается |

Read query не создаёт событие, audit mutation, lease либо idempotency receipt.
Авторитетный rejoin — повторный List/Get с тем же business context. Cursor
связан с actor, tenant, filters, context и source snapshot; срок проекции bounded.
Credential refs, secret coordinates и observation input не возвращаются:
только безопасные состояния, timestamps и digest серверного происхождения.

Provider health имеет типизированную область CREDENTIALED_CATALOG_REACHABILITY:
свежий успешный REMOTE_API/REMOTE_CODEX observation текущей версии account и
credential подтверждает только доступность этого авторизованного пути. Это не
provider-wide SLA. Проекция возвращает точные observedAt/expiresAt этого
наблюдения; смена credential, истечение срока или неклассифицированный отказ
дают UNKNOWN без объявления provider outage. Пустой подтверждённый каталог
сохраняет health READY, но совместимость с моделью остаётся BLOCKED.

CONFIGURE принимает candidate runtimeProfileRef и читает его текущие enabled,
provider, model и runtime_revision тем же условием, что Save runtime configuration.
Этот snapshot входит в contextDigest; профиль формы не подменяется сохранённым.
LAUNCH запрещает candidate overrides и использует сохранённые config/policy pins.
Внутренний batch helper для Workflow не требует отдельного agent.launch: его
вызывающая сторона проверяет workflow.launch и серверное делегирование.

Локальные проверки contribution:

- PASS: полный disposable PostgreSQL Bootstrap 33.432 s и Avatar 0.410 s;
  включает шесть dimensions, single/list parity, actor/context cursor, раздельные
  manage/launch права и отзыв, system-assistant boundary, candidate profile,
  отменённое чтение и фактический concurrent claim с исчерпанной capacity.
- PASS: race repository/platform, transport/grpc и domain/service/platform;
  полный CP vet/build; Proto lint/build/canonical replay; SQL boundary.
- Первые scoped PG проверки завершались FAIL: migration не назначала owner
  role; затем fixture обнаружила, что existing access evaluator скрывает отказ
  через NotFound. Исправлены migration role и классификация отказа только после
  успешной проверки visibility. Повтор scoped PG PASS 0.555 s, затем полный
  набор выше проверил итоговые health/profile изменения.
- Context7: документация jackc/pgx по transaction options и StrictNamedArgs.
- HTTP/SDK/PWA и live/provider/staging acceptance: NOT RUN в этом CP contribution.

Ручная проверка после подключения HTTP: открыть CONFIGURE с точным Agent и
candidate profile; сравнить пустую модель и поддержанную модель. Отозвать только
agent.launch: CONFIGURE сохраняется, LAUNCH закрывается. Занять последний slot:
capacity показывает ожидание, допустимый queued submission остаётся разрешён.
После reauthorization до свежего observation health UNKNOWN; после успешного
verified observation health READY только в указанной области. Секреты,
credential coordinates и сведения о чужих leases не раскрываются.

Заключительная проверка конкурентного отзыва: внешний SELECT FOR UPDATE
повторно проверяет enabled, AUTHORIZED и current credential на заблокированной
строке, даже если selection function вычислялась до ожидания. Targeted
PostgreSQL regression PASS 0.292 s (сам сценарий 0.06 s): pg_blocking_pids
доказывает ожидание reader на writer, после commit DISABLED actual selection
возвращает ErrConflict. Первый запуск FAIL 0.650 s был ошибкой fixture:
enabled=false без state=DISABLED нарушал существующий constraint. Исправлена
только оснастка; state constraint и production guard не ослаблялись.

Повтор полного baseline на интеграции `45a6d3cfa` выявил зависимость этой
оснастки от общего изменяемого аккаунта: последний сценарий ожидал блокировку,
но не проверял ранний результат выбора. Этот запуск — FAIL, несмотря на
исторический targeted PASS выше. Теперь сценарий создаёт отдельный аккаунт и
credential fixture, публикует через команду политику FIXED с единственным
кандидатом и проверяет actual selection до конкуренции. Ожидание по-прежнему
подтверждается `pg_blocking_pids`, после отзыва требуется `ErrConflict`;
раннее завершение reader выводится отдельной диагностикой. Production SQL и
ограничения состояния не изменялись. Полный CP PostgreSQL на исправленной
оснастке прошёл за 33.595 s, сценарий блокировки — за 0.09 s.

В том же baseline отдельный публичный scheduler subset завершился FAIL:
три сценария зависели от каталога, подготовленного соседними тестами полного
набора. Каждый из трёх helper теперь самостоятельно подготавливает observation
через существующую синтетическую оснастку. Каталог провайдера и живая среда
для этого не вызываются.

Повтор `make test-automation-scheduler` — PASS: все три PostgreSQL сценария
за 3.388 s, unit/race scheduler и CP, оба environment render. Repository race
прошёл за 1.883 s; `make check-sql-boundary` — PASS. Эти результаты относятся
к исправленной оснастке поверх `bebb3e04c`, до фиксации contribution commit.
