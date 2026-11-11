---
id: OPS-OPENCODE-NATIVE-2108
title: OpenCode native adapter и границы квалификации
type: runbook
status: approved
owner: developer
version: 1.4.0
updated: 2026-11-12
---

# OpenCode: нативные сессии и восстановление

Работа: [#2108](https://github.com/codex-k8s/kodex/issues/2108), дочерняя
[#1982](https://github.com/codex-k8s/kodex/issues/1982).
Точка передачи: native adapter квалифицирован локально. Полный переход платформы
остаётся в работе в #2108; локальные тесты не подтверждают установленный результат.

## Решение об upstream

Используется официальный приватный headless server OpenCode **1.19.5**,
V1 HTTP API. Один процесс обслуживает одну текущую execution generation.
Он слушает только `127.0.0.1` со случайной локальной авторизацией; общий
публичный multi-tenant OpenCode server не создаётся.

Проверенные исходники релиза:
`810c0eba1a7348036f8bdf61e6632d0c36c6876c`.
Архив `opencode-linux-x64-baseline.tar.gz`:
`sha256:17c200fd4b2289954cc01552559f9b6d03ec157b2ddfb5eb54144f491656333d`.
Извлечённый Linux amd64 executable:
`sha256:7df4e4136301f16589b0f30bf91a61380fca5f8ef22dd36570f5ebe1d4ec802c`.

Проверены Context7, [V1 server](https://opencode.ai/v1/docs/server/) и
[точные исходники](https://github.com/anomalyco/opencode/tree/810c0eba1a7348036f8bdf61e6632d0c36c6876c).
Схемы V2 (`/api/session`, `compaction.keep`) не применяются к этому бинарнику.
Наличие более новой страницы документации не является доказательством
поддержки параметра. Health требует exact version перед первым запросом.

OpenCode владеет transcript, model loop и автоматической compaction.
Kodex не реализует второй цикл шагов и не вызывает собственный summarizer.
Тонкий HTTP-клиент отправляет только новый ввод и текущую утверждённую
system-материализацию; summary не является итогом бизнес-хода или authority.

CLI `run --format json` заменяется API: его поток не предоставляет удобной
границы свежей system-материализации, точного native binding и terminal reply.
Штатные CLI `export <exact-id>` / `import <local-file>` остаются offline
средствами переноса native state; Kodex не читает и не воспроизводит схему DB.

## Граница сохранности

У каждой сессии отдельный приватный HOME/XDG. Config и credentials выдаются
заново процессу и не входят в checkpoint. Пользовательский repository config,
default plugins, native shell/file/network tools и native subagents не должны
расширять управляемый каталог инструментов Kodex.

После остановки provider process требуется положительный join всей process
group. Затем официальный export читает согласованную native DB. Экспортёр
также должен завершиться и освободить writer до публикации.
Копирование живых SQLite/WAL файлов не используется.

Большой stdout pipe **не подходит** для CLI export 1.19.5: локальная
квалификация обнаружила обрезанный JSON размером 196608 байт при exit 0.
Экспорт направляется прямо в новый приватный regular file, затем проверяется
полный JSON, exact native ID/version, message/part identity, размер и SHA256.
Один успешный exit не подтверждает checkpoint.

Официальный import получает уже открытый descriptor проверенного локального
файла в **новый** HOME. Инициализированная или неопределённая native DB
не объединяется и не очищается автоматически. Повреждённый, отсутствующий,
чужой либо несовместимый export не заменяется пустой сессией.
Незавершённый native tool требует reconciliation, а не автоматического replay.

Это механизм native state. Сквозной checkpoint также обязан связать его с
workspace generation и принятыми effect receipts. Без owner commit этой
связи локальный export не объявляется успешным восстановлением платформы.

## Карта владельцев и переходов

| Сценарий | Владелец / проверка | Результат и потребитель |
| --- | --- | --- |
| Новый ход | CP: current initiator, grants, account, immutable RuntimeRevision, lease/fence | Controller проверяет exact signed input; broker запускает приватный OpenCode |
| Создание native session | Только broker после owner claim, один POST `/session` | Полученный upstream ID связывается с exact Kodex session; prompt не выбирает ID |
| Следующий ход | CP выбирает прежний подтверждённый binding и совместимость; права/credentials разрешаются заново | Official import + exact session; только новый ввод и текущие инструкции |
| Native tool | Управляемый MCP → прежний owner/gateway | Текущие grants, Human Gate и effect reconciliation остаются обязательными |
| Compaction | Штатный OpenCode threshold и разрешённая модель | Наблюдение/usage; не новый business turn и не SUCCESS |
| Обычный final | Completed ordinary assistant message + согласованный SSE terminal | Broker публикует итог; summary/error/length stop не проходят |
| Stop/timeout | Owner прекращает admission effects; native abort/process join | Частичный результат сохраняется только с доказанной границей; неизвестный tool не повторяется |
| Потеря SSE/POST | Нет автоматического повторного POST | Неопределённый результат, остановка provider; дальнейшее действие после reconciliation |
| Lease expiry | CP отзывает прежнюю lease/grants и возвращает допустимую работу в очередь; незавершённый native writer блокирует claim | Только точный остановленный writer может записать checkpoint и usage через прежний fence/CAS; исход run не меняется, graph event отсутствует; readback session_storage/run |
| Повторный claim после expiry | Закреплённый checkpoint, отсутствие неопределённых effects, свежие права и RuntimeRevision | Новая lease/generation; прежние полномочия не возвращаются |
| Поздний чужой или заменённый writer | Exact lease/fence/generation + current storage writer CAS | Отказ до изменения checkpoint, usage или состояния; событий нет |
| Повтор checkpoint receipt | Точный idempotency key и текущая owner boundary | Прежний receipt; повторного учёта usage, event или tool effect нет |
| Export | Join native process → официальный export → join exporter | Private file/hash; ещё не owner checkpoint receipt |
| Restore | Exact owner scope/digest/version + согласованный workspace checkpoint | Новый приватный HOME, текущий runtime input; отсутствие state закрывает запуск |
| Cutover | Drain старых executions и явно выбранная оператором очистка старых сессий | Нет legacy decoder или неявного преобразования auth; подключения и внешние объекты не удаляются |

Bindings, checkpoint и terminal receipts принадлежат существующему control
plane. Их read/claim/complete paths должны использовать прежние owner
транзакции, idempotency, OCC, terminal closure и authoritative readback;
native API не становится новым источником прикладных полномочий.

Для `DELETE_OBJECT` отдельного native archive применяется format3 и прежняя
owner-граница: session без workspace, exact tenant/session/archive/object
version, завершённая retention и отсутствие активной задачи того же объекта.
Инициатор — materializer control-plane; исполнитель — workload `session-archive`
через специализированные claim/renew/complete/fail RPC. Полномочия следуют из
проверенного workload и owner lease/fence/generation, а не ключа объекта в payload.
Префикс `session-archive/v3` согласован у producer, claim, owner-проверки
удаления проекта, worker удаления и конфигурации backup-controller. Перенос
SYSTEM-сессии в проект сохраняет immutable ключ `/_system/`; право удалить его
доказывается актуальным owner lineage в PostgreSQL, а не переписыванием ключа.
Worker использует отдельное архивное хранилище, проверяет exact version и
отсутствие этой версии после Delete; одинаковый ключ в хранилище артефактов
не затрагивается. Старый префикс не допускается как запасной вариант.

| Переход native `DELETE_OBJECT` | Результат владельца / наблюдение |
| --- | --- |
| Materialize | `SUPERSEDED` либо `AVAILABLE` закрытой session → новая `READY` task с immutable object/version и input digest; отдельного события нет, авторитетны task и archive |
| Claim | `READY` → `CLAIMED`, новая attempt/generation/fence и ограниченный lease; отдельного события нет, исполнитель читает exact claim |
| Renew | Продлевается лишь та же активная claim; stale/foreign отклоняется, нового события нет |
| Complete | Exact receipt → `SUCCEEDED`, archive `DELETED`, claim закрыта; если это current archive закрытой session, storage `PURGED` и штатный `RUN_CHANGED` для связанных run, иначе отдельного события нет |
| Fail / expiry | Claim закрывается; bounded backoff → `READY` либо `DEAD_LETTER`; физическое удаление не объявляется выполненным, отдельного события нет, состояние читается владельцем из task/receipt |
| Project purge | Owner lifecycle отменяет ещё не начатую task; активные работы проверяются перед удалением полного графа, GC не создаёт новые задачи для `PURGE_PENDING` |
| Replay / неопределённый ответ | Прежняя receipt при текущей authority; нового эффекта или события нет; отсутствие ответа не подтверждает физическое удаление |

Миграция041 включает две новые FK writer в полный purge graph только после
проверки прежних 111 узлов/306 рёбер без точной новой пары. Итог — 111/308,
с прежним алгоритмом, owner ACL и `DEFERRABLE INITIALLY IMMEDIATE` для новых
связей. Неизвестная дополнительная FK закрыто отклоняет cutover. Исторические
schema proofs не расширяются; текущие проверки явно отделяют полный граф от
inverse-проекций. Проверены через Context7 документы PostgreSQL18
[`CREATE TABLE`](https://www.postgresql.org/docs/18/sql-createtable.html),
[`SET CONSTRAINTS`](https://www.postgresql.org/docs/18/sql-set-constraints.html) и
[`pg_constraint`](https://www.postgresql.org/docs/18/catalog-pg-constraint.html).

Первый native writer получает `INITIALIZING` intent в той же транзакции,
что lease и immutable RuntimeRevision. Последующий writer захватывает только
подтверждённый `LIVE` checkpoint без прежнего незавершённого writer.
Потерянный initial create не превращается в разрешение создать другую пустую
сессию. Completion связывает exact lease/revision, следующее native generation,
сохранённый native ID и текущую workspace generation. Успешный native outcome
без checkpoint отклоняется и исполнителем, и owner.

Архиватор использует формат 3 для официального native export как непрозрачного
файла. Его SHA256, размер, ID, версия и owner metadata закреплены до передачи
worker. Он не декодирует историю и не восстанавливает JSONL старого runtime.
Формат 2 для workspace сохраняется. Миграция отказывается работать при наличии
старых сессий и сама пользовательские данные не удаляет.

Image-owned `tool.execute.before` hook подписывает native session/call identity,
точные arguments и текущую RuntimeRevision отдельной процессной capability.
Loopback bridge проверяет её до передачи managed tool, удаляет envelope и
связывает owner receipt с native call вместо нестабильного SDK request ID.
SDK получает обратно свой correlation ID. Это не grant: текущие полномочия и
input/version checks остаются у владельца. Незавершённые внешние эффекты всего
root graph блокируют дальнейший native resume до сверки authoritative receipts.

После cancellation/timeout точный stopped writer может прислать поздний
checkpoint через тот же owner command. Проверяются исходные fence/generation,
immutable revision и текущий native writer CAS; metadata и подтверждённый usage
сохраняются без изменения terminal verdict и без нового graph event.
Authoritative read path — `session_storage` и обычный readback run/graph.
Точный receipt replay идемпотентен, а поздний writer не перезаписывает более новый.
Само продолжение требует отдельного разрешённого пользовательского перехода.

Hook не имеет npm dependencies. Read-only config directory предотвращает
автоматический npm bootstrap во время turn. Его подпись может оставаться в
закрытом native transcript как неавторитетная transport metadata; секретный
ключ туда не попадает. Штатный messages-transform hook удаляет только эту
metadata перед обычным inference и compaction, не меняя сообщения или результаты
инструментов.

## Доказательства текущего среза

Тесты запускают **настоящий** закреплённый binary. Синтетические HTTP endpoints
заменяют только модель и внешний MCP effect, а не OpenCode или его compactor.

```sh
cd services/jobs/agent-runner
KODEX_TEST_OPENCODE_BINARY=/absolute/path/to/qualified/opencode \
  GOTOOLCHAIN=go1.26.10 go test -count=1 -race -v ./internal/opencode
```

Без явного binary native-тесты дают `SKIP`; это не PASS квалификации.
Обычные unit-тесты не требуют скачивания runtime или model credentials.

| Проверка | Локальный результат |
| --- | --- |
| Exact native ID после замены процесса, прежний private XDG | PASS |
| 12 managed tool calls, 3 штатные compaction, обычный final | PASS |
| 144 tool calls, 36 compaction, 256 сохранённых messages | PASS |
| Official export/import в новый HOME, последующий business turn | PASS |
| Обновлённый system input, без повторения прежних tools | PASS |
| Полная история ~804 KB, максимальный фактический model HTTP body ~18 KB | PASS, synthetic forced threshold; это не tokenizer count |
| Duplicate JSON, чужие IDs, незавершённый tool, подмена digest/links | Unit-контур |
| Установленный Kodex / Pod replacement / S3 / WORKFLOW | NOT RUN для нового candidate |
| Полный cutover / UI / owner lifecycle / установка | Ветка продолжения, не готова к merge: общий PostgreSQL-контур FAIL |

Синтетический token pressure ускоряет достижение штатного порога, а не
имитирует многочасовую нагрузку или реальные пределы облачной модели.
Нормализованные counters OpenCode учитывают uncached input отдельно от
cache read/write, visible output отдельно от reasoning. Accounting не
доказывает физический reuse KV и не подтверждает отсутствие retention.

## Ограничения точки передачи

Изолированный native adapter можно квалифицировать независимо от включения в
платформу. WIP перехода owner/controller/installer на native contract нельзя
считать установленной функцией по тестам этого пакета. Полный PostgreSQL-прогон
обнаружил незавершённые bootstrap fixtures, старые archive paths, зависимости
purge graph и сценарии terminal/retry. Их необходимо устранить до cutover.
Миграция041, новые образы и RoleImage в тестовый кластер не установлены.

Точка воспроизведения native harness: `make test-opencode-native`, с явным
`KODEX_TEST_OPENCODE_BINARY`. Опциональный `KODEX_TEST_LITELLM_REDIS=1` запускает
изолированные LiteLLM1.104.2/Redis7.2 по digest, без доступа к внешней модели.
Полный native пакет с race после исправления SSE-учёта прошёл за210.074s,
включая managed effects, compaction, pruning, official export/import и
изолированный LiteLLM/Redis. Это локальный synthetic model
контур, не квалификация реального облачного account либо аппаратного inference.

В source `810c0eba1a7348036f8bdf61e6632d0c36c6876c` очередь V1 SSE сохраняет
ссылку на payload до сериализации (`session/processor.ts`,
`server/routes/instance/httpapi/handlers/event.ts`). Поэтому первое
`message.updated` может уже содержать `time.completed`, хотя `step-finish`
ещё не доставлен. Наблюдатель теперь ждёт обе части и сохраняет ограниченное
окно последних завершений для дедупликации. Регрессия воспроизводит раннее
завершение, повторные события и 4097 последовательных сообщений без накопления
всего transcript. Бюджет полного пакета — 300s, внешнего запуска — 360s;
индивидуальные ограничения native-сценариев сохранены.

## Каталог моделей после удаления Codex runtime

Срез #2108 оставляет один источник наблюдений `REMOTE_API`. CP выбирает только
аккаунт exact настроенного OpenCode definition, действующий credential и API_KEY.
Отсутствующая конфигурация не создаёт задач опроса. Число и имя удалённого enum
REMOTE_CODEX резервируются в Proto; decoder прежнего источника отсутствует.

| Путь | Authority, состояние и потребитель |
| --- | --- |
| PWA → gateway → ListModelCapabilities | Проверенные OIDC/внутренние полномочия, прежняя account eligibility; READY только по текущему REMOTE_API receipt; public endpoint и RPC не меняются |
| CP worker → ClaimProviderModelCatalogTasks | CP владеет account/credential/version и immutable task; отсутствующий или чужой definition не получает claim; API_KEY назначен из проверенного состояния |
| Generated client → ObserveProviderModelCatalog → secret-broker | Exact task/request digest, workload/method, account/credential pins, deadline; configured definition и API_KEY проверяются **до** чтения секрета |
| Broker → configured provider /models | Прежние exact TLS/egress/credential boundary и allowlist; URL из task/payload не принимается |
| Complete → CP receipt → gateway/PWA | Lease/fence/generation и текущие owner pins; одна транзакция сохраняет observation и закрывает task; REMOTE_CODEX/unknown не становятся READY |

| Lifecycle задачи каталога | Результат и read path |
| --- | --- |
| Create/claim | Прежние атомарные task/fence/request digest; без domain event, owner ledger CP |
| Renew | Не поддерживается; фиксированный deadline не продлевается |
| Complete | Прежний exact receipt, идемпотентность и один атомарный PROVIDER_ACCOUNT_CHANGED при новом completion; consumer — текущая platform projection, повтор события не создаёт |
| Cancel/delete/revoke/config drift | Прежние owner guards закрывают применение; удалённый/чужой definition не получает новую задачу; authority не восстанавливается по receipt |
| Expiry/retry | Прежний terminal expiry; повторный опрос создаёт новую task с новыми pins, без переноса device authority |
| Dead-letter/WAITING_OWNER/CHANGES_REQUESTED | Не являются состояниями этого task kind; не преобразуются в успех |

Новых deployment, listener, transport permission или secret key нет. Источник
контракта проверен через Context7 `/protocolbuffers/protocolbuffers.github.io`:
[резервирование удалённых enum](https://protobuf.dev/programming-guides/proto3/#reserved).
Это карта изменения; результаты проверок фиксируются отдельно по SHA.

## Явная проверка API-подключения

`POST /api/v1/provider-accounts/{providerAccountRef}/verification` заменяет
снятый device-only endpoint без alias. PWA отправляет прежние If-Match,
idempotency key и CSRF; gateway использует generated
`VerifyProviderAccountAuthorization`, operation
`platform.command.provider-accounts.authorization.verify`. Actor приходит из
проверенной OIDC session/service context; accountRef остаётся locator.
CP сначала разрешает account в tenant/owner boundary, затем OCC и receipt.

| Переход проверки подключения | Состояние, событие, потребитель |
| --- | --- |
| Create verification | Exact enabled AUTHORIZED account, current credential, configured provider и API_KEY; owner transaction создаёт PENDING с account version/credential revision, audit/receipt и PROVIDER_ACCOUNT_CHANGED; PWA получает PENDING, не успешный probe |
| Claim/renew | Сам verification не имеет lease; привязанный catalog task использует прежний claim/fence/deadline; renewal отсутствует |
| Complete | Один exact REMOTE_API observation до deadline → VERIFIED или FAILED; атомарный PROVIDER_ACCOUNT_CHANGED, PWA/realtime readback; identity credential не меняется |
| Repeat/lost response | Свежая owner eligibility до receipt; прежний verification ref и текущий readback, без второго probe intent/event |
| Disable/revoke/delete/config drift | Owner guards и отмена catalog task закрывают применение; verification STALE/FAILED по прежнему owner sweep, событие на изменённый account; поздний complete не восстанавливает доступ |
| Deadline/expiry | Прежний bounded sweep переводит PENDING в FAILED; PROVIDER_ACCOUNT_CHANGED и authoritative account verification read |
| Retry | Новая явная owner-команда с новым key создаёт новый verification и новую catalog task; credentials не обновляются автоматически |
| Dead-letter/WAITING_OWNER/CHANGES_REQUESTED | Не поддерживаются этим видом; нет преобразования в VERIFIED |

Bootstrap API credential может не иметь interactive authorization attempt:
допуск требует exact настроенный definition/current credential, а брокер перед
remote GET декодирует только его typed API material. Любая сохранённая
AUTHORIZED попытка другого метода закрывает проверку. Новых workloads, keys,
listener или сетевых направлений нет; меняются существующие generated RPC,
operation profile, authority registry и target-owned service policy.

Материализация API key также требует обязательный owner-derived definition
во всём пути reservation → domain port → generated RPC → broker port → typed
credential. Пустой либо старый definition не подменяется локальным default.
Reservation принимает только API_KEY и exact настроенный route после account
eligibility; публикация, ambiguous commit, reference readback и compensation
сохраняют прежние task/account/attempt/UID/version/digest границы.

## Bootstrap новой установки и начальные egress-проекции

Release и локальный render получают одну конфигурацию через
`tools/install/model-provider.py`: Git-owned exact definition/model, typed
credential из приватного API-key файла и отдельный cache namespace key.
Локальные `codex login`, автоматический импорт старых auth-файлов и SQL default
`openai-codex` удалены. `provider-bootstrap.py` принимает только canonical
credential либо новый seed с точным именем, namespace epoch, labels, annotations
и digest; прежний неполный snapshot не читается как вход миграции. Seed создаётся
лишь до появления owner state; повторная установка разрешает current credential
через authoritative owner readback/CAS. Потерянный ответ create не разрешает
повторить внешний эффект без чтения deterministic identity.

Изменение provider меняет digest базовой egress policy. Оба renderer вызывают
`egress_bootstrap_projection.py`, который использует существующие Go-команды
`mail-projection` и `integration-projection` и связывает пустые начальные
ConfigMap, mounts, digest env и email runtime с фактической базовой policy.
Этот offline producer отклоняет непустые или опубликованные поколения; он не
подменяет CP publisher и не переписывает live-конфигурацию. Локальная почтовая
проекция затем материализует текущий typed source также по итоговой policy,
а не по исходному файлу из `deploy/k8s/base`.

Для браузерной проверки использован Context7 `/microsoft/playwright`:
[pageerror и pageErrors](https://github.com/microsoft/playwright/blob/main/docs/src/api/class-page.md).
Synthetic provider fixture использует API_KEY, обязательное bootstrap-поле
speechTranscription и проверяет отсутствие исключений страницы. Прогон проверяет
verification через новый endpoint, замену ключа, realtime readback и UNKNOWN
replay. Он не является доказательством доступности реального провайдера или
кластерной приёмки.


Квалификация полной установки выявила регрессию predecessor optional antivirus:
полная замена `runtime_file_source_visible` потеряла чтение `artifact_history` и
ограничение SERVER_DAG по `workflow_runtime_artifact_pins`. Исправление входит в
ещё не поставленную миграцию041, которая сверяет точный predecessor перед заменой.
Публичные metadata readback и миграционный verifier требуют новый source digest;
возврат прежней функции закрыто отклоняется. Обычные файлы сохраняют явный
`NOT_SCANNED`; skills и структурированные workflow evidence по-прежнему требуют
`CLEAN`. Это не новый grant и не перенос данных прежней установки.

### Допуск reasoning и независимый путь распознавания речи

Путь настройки reasoning: опубликованный overlay и выбранная модель → проверенный
каталог возможностей аккаунта → effective effort в immutable RuntimeRevision →
RunnerInput → проверка точного provider/model → model options OpenCode → штатный
SDK Responses либо Chat Completions. Разрешены `low`, `medium`, `high`; Messages
не переводит их неявно в другой протокол. Исходные overlay-поля прежнего runtime
для personality, web search, history и login shell отклоняются. Exact retry
использует закреплённый effective effort; настройки не назначают полномочий.

Для закреплённого source `810c0eba1a7348036f8bdf61e6632d0c36c6876c` проверены
[session/llm.ts](https://github.com/anomalyco/opencode/blob/810c0eba1a7348036f8bdf61e6632d0c36c6876c/packages/opencode/src/session/llm.ts)
и [provider/transform.ts](https://github.com/anomalyco/opencode/blob/810c0eba1a7348036f8bdf61e6632d0c36c6876c/packages/opencode/src/provider/transform.ts).
Context7 `/anomalyco/opencode` использован для поиска V1 model/agent options;
совместимость подтверждает закреплённый source и настоящий бинарник, а не latest
документация. Native wire-тест проверяет отсутствие параметра и `low`/`high`
для обоих API. Он не доказывает поддержку конкретной облачной моделью.

STT сохраняет отдельный фиксированный транспорт OpenAI и собственные полномочия:
владелец организации публикует SYSTEM_STT → версия/digest привязаны к STT_SERVICE →
caller получает текущий `platform.stt.use` → владелец состояния повторно проверяет
account, immutable credential generation, config revision/digest и прямой маршрут
`opencode-openai` на официальный OpenAI endpoint → secret-broker читает точные
Secret UID/resourceVersion/content digest и декодирует только новый owner-bound
credential envelope → STT получает ключ на ограниченное время для своего adapter.
Чужой gateway, LiteLLM и старый auth envelope не допускаются. Идентификатор account
из запроса не является полномочием. Ошибка допуска возвращает отказ без вызова
провайдера; успешная проекция не меняет состояние и не публикует domain event.
Авторитетный read path — текущая SYSTEM_STT configuration и credential projection.
Результат распознавания не проходит через native transcript OpenCode; это отдельная
возможность платформы. Без допущенного прямого OpenAI account STT показывает
отсутствие готовности, не использует ключ произвольной модели.

Пока native resume квалифицирован для неизменной пары model/runtime profile и
точного provider account. Несовместимая смена отклоняется владельцем уже при
запросе продолжения; она не создаёт бесконечно ожидающий run и не подменяет
сессию пустой историей. Новый явно запрошенный run может открыть новую сессию
с другой разрешённой моделью. Исходные native checkpoint и audit сохраняются.
Это не автоматический fork/перенос transcript между моделями.

Bootstrap и warm reconciliation используют одинаковое правило: включённый
AUTHORIZED account с текущей credential revision и способом API_KEY. Отсутствие
authorization attempt допускается только как установленная bootstrap credential;
при наличии attempt устаревший способ не выбирается. DEVICE_CODE не имеет
приоритета, а REAUTHORIZATION_REQUIRED исключается из нового исполняемого пула.
Новая immutable конфигурация фиксирует только прошедшие актуальную проверку
catalog/model кандидаты; исходная сессия не получает скрытый fallback аккаунта.

Полный PostgreSQL baseline имеет явный общий лимит Go `15m`; deadlines отдельных
сценариев остаются прежними. Первый широкий повтор завершился `FAIL` по прежнему
неявному лимиту `10m` во время штатного ожидания lease expiry, без предшествующих
ошибок assertions. Этот исход не считается PASS. Расширенный набор квалифицируется
повторно целиком; отдельные адресные проверки его не заменяют.

Повтор на исходном коде `33f37cc21` завершён: полный PostgreSQL18.3 component-набор
PASS648,946с, завершающие read-only/owner проверки PASS. Native harness этого кода
с настоящими OpenCode1.19.5/LiteLLM1.104.2/Redis и synthetic inference — PASS270,439с:
144managed effects,36compactions,256retained messages, export804136bytes,
максимальный model HTTP body17736bytes. Эти локальные результаты не подтверждают
CI, установленный кластер или аппаратный/облачный cache profile.

## Настройки исполнения через существующий overlay

Для #2108 предпочтения context/cache принадлежат опубликованной revision overlay
агента. Git-owned provider descriptor задаёт endpoint/model ceilings,
qualification и обязательные ограничения маршрута. Overlay не изменяет descriptor,
qualification, credentials, cache namespace key или источник authority. UI-owned
предпочтения не могут ослабить обязательную route policy. Служебный config digest
остаётся дайджестом исходного Git descriptor; overlay имеет собственные
source/version/digest в прежней RuntimeRevision.

| Сценарий | Сквозной путь и инвариант |
| --- | --- |
| Создать/изменить draft | Проверенный actor → прежний agent overlay endpoint → generated RPC → owner boundary агента, затем version/idempotency → immutable draft. Произвольные provider options, salt, endpoint и qualification не входят в editable schema |
| Validate/publish | Прежние специализированные команды overlay; CP проверяет exact provider/model и cache enforcement до публикации. Ошибка не меняет active revision; обязательные ограничения Git нельзя отменить предпочтением UI |
| Git-owned конфигурация | Действуют прежние source ownership и отдельный copy/detach. UI не переписывает Git-owned объект; настройки разных владельцев не используют одно поле хранения |
| Readback | Прежний runtime configuration view показывает requested overlay и effective context/cache с UNKNOWN/UNSUPPORTED и причиной. Чтение не меняет состояние и не создаёт события |
| Claim/retry/continuation | CP разрешает свежие права и опубликованный overlay, закрепляет revision. Exact retry сохраняет immutable snapshot; continuation получает новое разрешённое состояние |
| Runner | Проверяет signed input и ту же typed policy, передаёт только штатные compaction/prune и квалифицированные cache controls native adapter. Qualification не пересчитывается из ответа модели |
| Terminal/cancel/delete/expiry | Прежние owner transitions закрывают leases/grants; изменение overlay не оживляет run и не продлевает срок исполнения |
| Ошибка cache enforcement | Обязательная неподдержанная политика отклоняется до model HTTP request. NO_REUSE и NO_STORE остаются независимыми; transport не выбирает более слабый fallback |

Изменения используют прежние RPC registration, clients, readiness и deploy
ownership CP/gateway/runner. Отдельных фоновых задач, событий или второго
policy owner не добавляется; audit/idempotency/event cardinality существующих
команд overlay сохраняются. Матрица задаёт проверяемый путь реализации, а не
свидетельствует об установленной приёмке.

Редактор агента предлагает наследование маршрута, auto-compaction/prune,
optimization/reuse scope и строгие no-reuse/no-store/required через ту же TOML
схему, что проверяет CP. Настройки проходят draft → validate → publish;
несовместимый строгий запрос остаётся INVALID и не меняет published revision.
Фактическая конфигурация показывает запрошенные и действующие native policies.
Старые Codex overlay поля и hosted-search selector удалены, включая managed
assistant schema и неполный legacy read projection. Поиск через managed tools
остаётся отдельным разрешением, не параметром модели.

Локальная проверка редактора: unit/runtimecontract с race, CP unit, PostgreSQL
publication/history/rollback и SYSTEM/PROJECT assistants, gateway HTTP и
controller callback race, producer/consumer managed MCP, UI unit и Chromium
на 390/1440/2900 px. Настоящий OpenCode + LiteLLM/Redis с synthetic model
повторно прошёл native harness: 144 tools, 36 compactions, 256 retained messages,
export 804318 bytes, максимальный model HTTP body 17738 bytes; 246.813 s.
Это forced-threshold квалификация, не многочасовая нагрузка и не cluster E2E.
Полная установка, несколько provider definitions и установленная приёмка
по-прежнему не подтверждены этим срезом.

## Несколько маршрутов: сквозная карта установки

Каталог установки — один Git-owned immutable документ с закрытым набором
provider definitions и точным default provider/model. Это требования текущего
продолжения #2108; наличие этой карты не означает завершённую реализацию.
В RunnerInput по-прежнему входит только выбранный descriptor, не весь каталог.
Учётные записи и API keys создаются через прежний owner lifecycle; наличие
маршрута в каталоге не выдаёт право использовать чужой account.

| Сценарий | Полный путь и граница состояния |
|---|---|
| Чистая установка | Проверенный owner input → installer closed catalog → immutable ConfigMap/digest → CP registration и broker/egress configuration. Default model должен принадлежать exact default definition; пустая/дублирующаяся/неоднозначная конфигурация отклоняется до apply |
| API key account | Проверенный actor и tenant → прежний provider-account endpoint/RPC → owner definition eligibility → exact attempt/account credential command → broker выбирает один descriptor по owner definition. Чужая definition не разрешает чтение секрета; credential envelope закрепляет definition |
| Каталог моделей | Owner task/claim/request digest → проверенный service principal → exact account/version/credential/definition → broker HTTP по одному маршруту → ограниченное наблюдение → owner completion CAS. Model alias другого маршрута и payload definition не назначают authority |
| Выбор в UI | Каталог definitions/accounts/models → существующий agent runtime publication с exact catalog pins → owner transaction/version/idempotency → immutable runtime configuration. Один model ID у двух провайдеров не делает их взаимозаменяемыми |
| Claim/continuation/retry | Текущие grants + выбранный account/definition/model → свежая RuntimeRevision → один descriptor в signed RunnerInput → broker → OpenCode adapter. Exact retry сохраняет прежний snapshot; несовместимый native binding блокируется, не очищается |
| Egress | Signed configuration digest/base URL/addresses → exact registered route → TLS hostname/CA и destination pins. Несколько definitions на одном host не разрешают выбирать произвольный descriptor только по имени; runtime сравнивает полный подписанный route |
| Изменение Git-каталога | Установочный drain и положительный join активных writers предшествуют смене immutable конфигурации. Existing UI selections не переписываются. Удалённая definition закрывается, её queued/catalog tasks истекают по прежнему owner lifecycle; новый claim/renew не получает старый маршрут |
| Stop/delete/terminal/expiry | Прежние owner transactions закрывают весь execution graph и grants. Каталог не создаёт альтернативный переход, retry или «оживление» терминального run; events/read paths и cardinality прежних task kinds сохраняются |
| Отказ/default | Неполный default, неизвестный provider/model, неподтверждённая строгая policy либо отсутствующий egress route дают закрытый отказ. Другой account/provider не выбирается как неявный fallback |

Producer/client registration, RPC, inbox/effects и deploy ownership остаются у
существующих CP, broker, controller, gateway и runner. Qualification относится к
exact API/model/proxy/route, а не к факту наличия нескольких definitions.

Каталог имеет формат `kodex.model-provider-catalog/v1` и общий source revision,
явные default provider/model, не более 16 независимых descriptors. Старый
одиночный installer-файл не читается. Bootstrap key создаёт account только
для default provider; дополнительные accounts проходят обычную авторизацию
API key и удалённую проверку моделей. Один alias не выбирает provider.

Для прямого Messages catalog используется `GET <base_url>/models` с
`x-api-key` и закреплённым `anthropic-version: 2023-06-01`; для LiteLLM catalog
используется Bearer credential proxy. Неполная pagination даёт UNVERIFIED,
не выдуманный полный список. Через Context7 проверены официальный
`/anthropics/anthropic-sdk-typescript`, `models.ts` и `client.ts`; текущий SDK
не считается доказательством поддержки установленным OpenCode. Synthetic
broker-тест проверяет конкретные headers и cross-definition отказ; реальная
доступность account подтверждается только установленной проверкой.

Контракт каталога зарегистрирован как `model-provider-catalog-v1`; egress schema
ссылается на него, а он — на единственную схему runtime descriptor v10. Проверки
разрешают эти ссылки из закрытого локального реестра без сетевой загрузки.
Для `github.com/google/jsonschema-go` через Context7 проверен `ResolveOptions.Loader`,
его сигнатура сверена с исходником закреплённого v0.3.0.

## Измерения model requests: сквозной путь

Требование #2108 разделяет обычные шаги, штатную compaction и auxiliary calls.
Карта была составлена до реализации. Локальные проверки и кластерная приёмка
отчётно разделяются; наличие схемы пути не подтверждает установленный результат.
Native `step-finish` даёт нормализованный расход, но время получения SSE event
не является TTFT фактического HTTP request. В закреплённом upstream
`session/llm/request.ts` вызывает `chat.headers` с agent/session/model и
передаёт headers в SDK; image-owned fetch hook уже охватывает реальные запросы,
включая SDK retries. Эти seams квалифицируются на настоящем бинарнике.

| Сценарий | Полномочия, переход и потребитель |
|---|---|
| Начало request | RunnerInput и admitted image назначают route; native hook передаёт только закрытую классификацию обычного/summary/auxiliary вызова. Внутренняя metadata удаляется fetch hook до upstream; model ID и payload не назначают authority |
| TTFT/завершение HTTP | Тот же fetch wrapper измеряет каждый фактический request и первый содержательный streaming output по закрытому API. Timing начинается непосредственно перед send, не на начале бизнес-хода. Неполный/неизвестный ответ остаётся UNKNOWN/PARTIAL; отсутствующий token не становится нулевым TTFT |
| Native usage | Observer получает `step-finish` в exact native session, отдельно классифицирует summary и обычное сообщение и валидирует counters. Нормализованный native input уже включает cache reads/writes в общей сумме; подмножества не прибавляются повторно. Cost без квалифицированного источника остаётся UNKNOWN |
| Доставка | Приватный loopback hook path привязан к свежему bearer и текущему execution; ограниченный callback идёт через существующий runner progress → runtime-controller → CP ReportExecutionProgress. CP разрешает tenant/root/node/session/turn/attempt и lease/fence по авторитетному состоянию, а не по metadata измерения |
| Устойчивый результат | CP сохраняет закрытую безопасную measurement projection в существующем run event и outbox одной owner transaction. Stable receipt не допускает двойного учёта повторной доставки. Model request и native step — разные единицы: SDK retry не выдаётся за новый бизнес-Turn или новый native step |
| Stop/terminal/revoke | Прежние owner transitions закрывают lease/grant; новые observations от stale writer отклоняются. Уже подтверждённые измерения сохраняются в истории. Неизмеренный хвост не реконструируется как нулевой расход; metrics не разрешают повтор внешнего effect |
| UI/метрики | Защищённый event read/realtime показывает request/step phase и completeness; Prometheus использует только закрытые source/phase/outcome/completeness и token-kind labels. Prompt, reasoning, salt, credential, session ID и model alias не попадают в общие labels. Measurement не меняет RUNNING/COMPACTING phase и не объявляет SUCCESS |

Общий native OpenTelemetry в этом срезе не включается автоматически:
проверенный `session/llm.ts` не отключает запись inputs/outputs в SDK telemetry.
Нужен узкий secret-free measurement path без экспорта prompt/transcript.

### Исполняемые consumers и границы доказательств

`ModelMeasurement` — закрытая проекция максимум4096байт. Native `step-finish`
сохраняет PARTIAL usage, отдельно cache-read/cache-write/reasoning; HTTP wrapper
не выдумывает usage из latency. Время первого содержательного delta (включая
reasoning либо аргументы tool) измеряется от фактического fetch. Это не точное
время генерации первого токена внутри провайдера. Неполный SSE, отсутствие
измерения либо прекращение потока сохраняют неполноту. Parser держит ограниченную
часть frame, не создаёт `tee`, не меняет bytes и уважает backpressure.

Приватный `/model-measurements` доступен только на прежнем provider loopback
bridge с fresh bearer и exact RuntimeRevision. Model ID, session ID и tenant
в payload отсутствуют. Broker сериализует native и HTTP observations; после
positive native join выполняет bounded HTTP shutdown до terminal frame.
Consumer callback доставляет одну immutable receipt с ограниченными retries.
CP перед чтением квитанции повторяет active lease/fence/root/deadline guards.
Ключ назначается из owner lease ID, generation и measurement ref; повтор не
добавляет audit/outbox/metrics. Lookup использует прежний indexed idempotency
store, а не сканирует всю историю при каждом шаге.

Новое событие — прежний `TURN_PROGRESS`, cardinality один на принятую квитанцию,
origin exact execution. Оно не меняет progress summary узла. Run outbox сохраняет
прежние sequence/root pins; gateway после wake читает exact authoritative event
через защищённый `ListRunEvents`, затем общий HTTP/WebSocket mapper сохраняет
measurement. Структура в Proto/OpenAPI/browser AsyncAPI одинакова. Отдельная
schema migration не нужна: typed projection хранится в существующем `safe_delta`.
Новый decoder закрыто проверяет её и не вводит reader старого runtime.

PWA показывает сводку только **загруженной** истории, отдельно обычные шаги,
сжатие и auxiliary requests. Receipt повторно не складывается, измерение не
получает индикатор текущей работы. Неизвестный TTFT и стоимость отображаются
явно. Измеренные cache tokens не доказывают физическую изоляцию, отсутствие
retention или ускорение.

После подтверждённого owner commit CP обновляет process-local operational
Prometheus counters/histograms. Crash или потерянный COMMIT acknowledgement
могут пропустить такое обновление; эти counters не являются durable billing.
Авторитетный readback — сохранённые run events. Dashboard содержит отдельные
phase/usage/TTFT panels; служебные идентификаторы и значения модели в labels
не включаются. `/metrics` размещён на существующем technical listener, без
нового публичного endpoint или изменения сетевой authority.

Проверены официальные исходники закреплённого OpenCode
`810c0eba1a7348036f8bdf61e6632d0c36c6876c` (`session/llm/request.ts`,
`packages/plugin/src/index.ts`) и через Context7 Node.js24.22.2 Web Streams
`pull`/`cancel`/`highWaterMark`. Node docs объясняют API, а совместимость с Bun
в OpenCode подтверждается отдельным real-binary harness. Штатный
`make test-opencode-native` также запускает bounded transport tests: все три
API, fragmentation, отмена, отдельные HTTP attempts и oversized frame.
