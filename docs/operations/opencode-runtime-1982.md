---
id: OPS-OPENCODE-1982
title: Опциональный OpenCode runtime
type: operation
status: approved
owner: developer
version: 0.2.0
updated: 2026-11-10
---

# Опциональный OpenCode runtime

## Совместимость MCP-каталога

`runtime-controller` публикует `required: []` для object schemas без обязательных
полей. Значение `null` недопустимо: строгий MCP-клиент OpenCode отвергает весь
каталог, хотя командная проверка `opencode mcp list` может завершиться с кодом0.
Готовность проверяется по статусу подключения и реальному вызову разрешённого
инструмента с результатом, а не только по успешному текстовому ответу модели.

Синтетическая регрессия вызывает настоящий JSON-RPC `tools/list` для agent,
SYSTEM и PROJECT профилей, проверяет вложенные input/output schemas и компилирует
их. Пустой optional input остаётся допустимым; обязательные поля, ограничения
длины и запрет неизвестных аргументов проверяются отдельно. Изменение формы
каталога сохраняет прежнюю проверку прав при вызове инструментов.

Для адресного обновления этого unit существующий опциональный
`--component-image-profile` принимает `component: runtime-controller`. Профиль
закрепляет полный прежний release lock, обе source SHA и прежний/новый immutable
image в том же registry. Меняется только выбранный контейнер с provenance
annotations; sidecars, service account, volumes и replica count сохраняются.
Обычный render без профиля сохраняет прежние images. Допуск workload resume
с прежними lifecycle/idle и authority-проверками остаётся обязательным; профиль
сам по себе не разрешает перезапуск активного выполнения.

Unit #1982 добавляет engine к существующей job `agent-runner`, сохраняя
обычный Codex при отсутствии опции. Это полный путь конфигурации, account,
immutable runtime input, broker, управляемых tools, callback и deployment;
отдельный автономный orchestration graph не создаётся. Описание контракта
не заменяет acceptance receipt конкретного установленного source/image/profile.

## Поддержанный профиль

Первая версия поддерживает `engine=opencode`, OpenCode1.19.5 и
`tool_mode=managed_mcp`. Chat Completions идёт через явно заданный HTTPS
gateway. Собственные shell/file-write/task/web/skill/plugin tools не доступны.
Remote MCP `kodex` обслуживается существующим scoped bridge и gateway.
Явная опция рабочей копии ниже добавляет управляемые локальные tools.
Native tools провайдера, Codex overlay/effort/session несовместимы с профилем
и должен закрыто отклоняться до запуска. Это ограничение профиля, не обход
допуска workspace или внешних действий. Codex сохраняет собственный профиль.

Провайдер использует отдельный ephemeral HOME/XDG внутри защищённой границы.
Project config не читается; native state удаляется после остановки процесса.
Continuation использует owner-pinned `SessionContext` Kodex. OpenCode session
ID не записывается как Codex UUID, JSONL rollout не имитируется. Native resume
OpenCode и перенос его внутренних SQLite-файлов не заявляются поддержанными.
Сохранность канонической истории подтверждается отдельным turn/restart тестом.

## Данные и ownership

`ModelProviderConfiguration` —закрытый несекретный контракт Git-managed
deployment: source revision, engine/version/tool mode, provider ID, HTTPS
base URL, exact /32 или /128 address pins, allowlist моделей и ограничений
запроса. Пределы владельца не выдаются за доказанные возможности модели.
Дубликаты, неизвестные поля, credentials, wildcard/loopback/metadata адреса,
неоднозначные URL и пустые пределы отклоняются.

Новая опция не разрешает UI молча перезаписывать Git-managed endpoint.
Account выбирается прежней provider policy и сохраняет session affinity.
Gateway credential имеет отдельный `auth_mode=gateway-api-key` и привязку
к exact provider definition. Legacy Codex decoder его не принимает: старый
компонент не должен переслать gateway key внешнему провайдеру при смешанном
rollout. Секретный codec используется только backend materialization и broker;
его payload не является runtime configuration и не включается в RunnerInput.

Provider получает ключ из защищённой credential projection, проверенной по
account/revision/UID/resourceVersion/content digest, и передаёт его только
процессу OpenCode через отдельную переменную. Generated configuration содержит
environment references. Remote MCP не создаёт credential-bearing shell child.
Другие role secrets/обычное окружение не наследуются процессом провайдера.

## Сквозная карта

| Шаг | Владелец и граница |
|---|---|
| Deployment config | Renderer проверяет typed descriptor и создаёт immutable configuration revision; default Codex остаётся прежним без опции |
| Registration/default | CP bootstrap регистрирует Git-owned definition/profiles; выбор default явный, существующие UI-owned agent configurations не затираются |
| API-key account | Existing owner command/reservation → secret-broker → canonical gateway credential, без ChatGPT login |
| Model catalog | Owner task несёт exact definition key; секретный observer выбирает только соответствующий configured HTTPS endpoint, без redirect или fallback |
| Launch/claim | CP проверяет profile/model/account/environment, фиксирует descriptor в свежей RuntimeRevision и digest |
| Materialization | RPC → runtime-controller → canonical RunnerInput; unknown/invalid descriptor и несовместимый image/profile закрыто отклоняются |
| Transport | Controller подписывает малую gateway route projection; proxy требует exact host/path/address pins, TLS и server-owned grant |
| Execution | UID-isolated broker проверяет input/context/auth pins, запускает pinned OpenCode с managed-only configuration |
| Tool call | OpenCode → local bridge → existing MCP gateway → fresh lease/capability/integration grant/Human Gate → effect receipt/result/audit |
| Result | Completed messages, partial usage и terminal verdict идут прежним callback; provider state не становится authority |

## Lifecycle

| Переход | Результат и источник истины |
|---|---|
| Create/claim | Только прежняя owner transaction создаёт Run/lease/attempt; конфигурация входит в immutable digest |
| Streaming/tool | Published items bounded; reasoning/raw errors не публикуются. Gateway владеет внешним effect audit; локальный workspace пишет один redacted native lifecycle без raw argv/файлов |
| Model error/auth reject | Один существующий failed callback; явная ошибка учётной записи не переключает provider/account/model |
| Timeout/cancel | Завершается process group и ожидается writer stop; gateway effect может остаться UNKNOWN, повтор не выдаётся за безопасный |
| Completion | Только реальный terminal stop, process join и workspace проверки; no fabricated archive и no inferred external success |
| Retry/delegation | Свежая RuntimeRevision через прежний owner graph; root initiator и immutable policy не берутся из prompt |
| Continuation/restart | История от CP, новые native process/state; прежние side effects не воспроизводятся startup-ом |
| Configuration update | Новый descriptor/revision; старый активный snapshot не переписывается. Совместимость proxy/profile/image проверяется до apply |
| Disable | Известно неподдержанный engine/profile блокируется; автоматического fallback нет |

OpenCode1.19.5 нормализует отсутствующие usage values в0; поэтому численные
наблюдения adapter отмечает PARTIAL, отсутствие наблюдения —UNKNOWN. Эти
показатели не являются billing, балансом или разрешением по квоте.

## Проверки и поставка

Обязательны closed descriptor/secret codec, wrong engine/account/model,
MCP-only permissions, egress/SSRF/redirect/address pins, bounded JSONL,
terminal failure/truncation, model auth errors, cancellation, tool/result/final,
continuation/restart, отсутствие утечки и Codex regression. Живой CLI smoke не
заменяет установленный role→gateway путь. Workspace V2 остаётся под #1953/#1964.

Все компоненты публикуются по exact source/image/schema pins. OpenCode образ
не объявляется штатным без qualification #1977; local build и green Pod не
являются приёмкой. Configuration/SQL changes не требуют новых бизнесовых
состояний Run: graph/events/lifecycle остаются у прежних владельцев.

Проверены Context7 `/anomalyco/opencode/v1.18.29` и официальный source
OpenCode tagv1.19.5, commit810c0eba1a7348036f8bdf61e6632d0c36c6876c.
В executable этой версии bundled `@ai-sdk/openai-compatible`2.0.41.
Квалификация выполняется именно на1.19.5; соседняя версия Context7 не является
доказательством совместимости. Источники:
[providers](https://opencode.ai/docs/providers/),
[permissions](https://opencode.ai/docs/permissions/),
[pinned CLI source](https://github.com/anomalyco/opencode/blob/v1.19.5/packages/opencode/src/cli/cmd/run.ts).

## Включение

Обычный renderer без новых аргументов сохраняет Codex. Для OpenCode передать
`--model-provider-configuration /operator/provider.json`; только явный
`--model-provider-default-model` выбирает его для новых сотрудников и
помощников. Existing UI selections не перезаписываются. Provider definition
доступен в существующем каталоге, поддерживает только API key. Конфигурация
имеет `schema=kodex.model-provider/v1`, `managed_by=git`, exact source SHA,
`engine=opencode`, `engine_version=1.19.5`, `tool_mode=managed_mcp`,
`provider_id`, HTTPS `base_url`, exact `/32` или `/128` `address_cidrs` и
allowlist `models` с `id`, `context_window_tokens`, `maximum_output_tokens`.
Ceilings задаёт оператор; это не утверждение о полном контексте модели.

Descriptor должен создаваться из закреплённого deployment source, а не
содержать SHA собственного файла. Renderer создаёт immutable ConfigMap,
проецирует его CP/SB и включает тот же descriptor в egress policy. Gateway
использует operator-approved literal addresses без ambient DNS. До apply
оператор сверяет DNS и доступность адресов; смена адресов требует нового
Git descriptor. HTTPS hostname и CA проверяются всегда. Endpoint нельзя
поменять под существующим provider ID/credential: нужен новый ID и account.

`python3 tools/install/model-provider.py prepare-auth --configuration
/operator/provider.json --api-key-file /operator/gateway-key --output
/operator/auth.json` создаёт secret-only канонический файл. Каталог0700,
вход/выход0600; существующий выход не перезаписывается. Ключ передаётся
существующему `materialize-secrets.sh --provider-auth-file` и сохраняется в
поддержанном Kubernetes backend, без фиктивной ChatGPT авторизации.
Повторный bootstrap сохраняет authoritative current credential.
Производный config OpenCode содержит только environment references.

Optional `agent-runner-opencode` собирается через закрытый target
`opencode-runtime` и включает прежний Codex. `agent-runner` по умолчанию не
содержит новый binary. В первой версии optional installation использует
standard RoleImage; documents environment закрыт до отдельной квалификации
этого варианта. Сетевой CA bundle/прокси предоставляет существующий защищённый
runtime; отдельный сетевой обход для модели не создаётся.

## Опциональная рабочая копия

Оператор публикует новую версию окружения с `policy.managedWorkspace=true`;
в UI это «Рабочая копия и команды (OpenCode)». По умолчанию поле отсутствует
или равно `false`: Codex и прежний OpenCode MCP-only сохраняют поведение
и прежние policy digests. Переключатель хранится у владельца версии окружения,
проходит черновик, OpenAPI/Proto, историю, повторный SSO и immutable input.
Generated config не является вторым источником настройки.

Профиль предназначен для обычного сотрудника OpenCode с явным
`platform.artifact.manage`, `workspace-write` и действующим execution.
SYSTEM/PROJECT помощники не получают этот доступ неявно. Внешние write grants,
персональные credentials и Human Gate остаются самостоятельными проверками.
Например, ревьювер может менять свою локальную копию для теста, имея во внешнем
репозитории только чтение и согласуемые комментарии к назначенному MR.

Нужен совместимый RoleImage из того же квалифицированного candidate с OpenCode
и выбранными SDK tools. Обновления одного PWA/control-plane недостаточно.
Основной MCP-каталог получает `get_workspace_policy`; старый runner отвергает
несовпадение каталога до запуска модели. Отдельный локальный каталог обслуживает
`/workspace/mcp`. Native `bash`, `edit`, `task`, plugins и произвольные MCP
endpoints провайдера по-прежнему запрещены.

| Инструмент | Возможность и ограничение |
|---|---|
| `list_workspace_files` | Одна директория `work` либо плоский `outbox`, страницы до200 записей |
| `read_workspace_file` | UTF-8 chunk до32768байт, полный SHA-256 файла до1МиБ для последующего CAS |
| `write_workspace_file` | Atomic replace точного предыдущего SHA-256 либо создание с `ABSENT`; это ещё не внешний commit/публикация |
| `run_workspace_tool` | Только выбранные команды окружения из inventory образа; executable и argv раздельны |
| `checkout_repository` | Полный ограниченный GitLab API snapshot на точном commit SHA, без передачи PAT в shell |

Лексические пути не дают authority. Каждый вызов требует свежую owner-проверку
lease/fence/generation, действующих прикладных прав и root initiator до работы
и перед выдачей результата. Локальный audit содержит имя операции, число полей,
state/duration; argv, код и file bodies туда не копируются. Повтор RPC ID
не разрешает повторить effect после потерянного ответа.

Файловые операции используют fd-relative `openat2` с запретом symlinks,
magic links, выхода из корня и смены mount. Единственный work PVC crossing
проверяется прежним owner-bound mount proof. Обычные файлы требуют одного
hardlink и допустимых UID/GID. Логические file/byte quotas проверяются перед
записью и публикацией; это не обещание мгновенной kernel quota для произвольного
компилятора. Физические CPU/memory/ephemeral-storage limits принадлежат Pod.

### Команды без секретов

При непустом списке tools появляется один контейнер `workspace-executor`
внутри execution Pod, а не постоянный сервис. Он получает только свою рабочую
копию, отдельные tmp/cache и управляющий UDS. Runtime input, provider HOME,
model/API keys, callback identities, integration credentials, входные VFS
volumes и Kubernetes token туда не монтируются. Env дочерней команды задаётся
закрытым списком; role/provider environment не наследуется.

Для этого контейнера Pod явно задаёт `command=/usr/local/bin/kodex-agent-runner`:
штатный `kodex-init` остальных контейнеров сделал бы daemon дочерним процессом.
Здесь protected binary сам проверяет точный invocation и реализует PID1 cleanup;
допуск образа и проверка root-owned executable сохраняются.

Контейнер работает как непривилегированный PID1 в отдельном PID namespace,
без host/shared PID, capabilities и записи в root filesystem. Дочерний процесс
перед exec ставит Landlock ABI>=3: mutation разрешена только в work/tmp и
`/dev/null`, включая все последующие fork/exec. Репозиторий не может удалить
и подменить управляющий UDS. Готовность требует реального child sandbox probe;
недоступный kernel primitive закрывает запуск до модели, без insecure fallback.

Команды сериализованы. Предел45с оставляет место owner-проверкам в существующем
MCP budget60с; объединённый stdout/stderr ограничен64КиБ и помечает truncation.
SDK timeout задаётся поддержанным `mcp.<name>.timeout` в миллисекундах.
Каждая команда заканчивается остановкой и reaping всех descendants, включая
отделившиеся process groups. Это не выполняется в host/unit-test PID namespace.
`UNKNOWN`, потерянный ответ или непроверенный cleanup закрывают последующие
файловые операции; автоматического повторения команды нет.

MCP cancellation отменяет только связанный собственный request. Завершение
turn закрывает admission, отменяет ожидающие запросы и получает положительную
quiescence до capture/публикации workspace. V2 Pod/PVC observer учитывает
четвёртый writer; его отсутствие в status либо running state не доказывает join.

Общий resource budget окружения делится между provider и executor с точной
суммой requests/limits. Новый full-size container не добавляется поверх
старого envelope; owner queue продолжает учитывать прежнюю общую заявку.
При пустом списке commands работают файлы/snapshot без executor и требования
Landlock. Отдельные runtime budgets и restart/cancel deadlines не отменяются.

### GitLab snapshot

Инструмент выбирает три согласованных READ grants одного Connection:
`gitlab.commit.read`, `gitlab.repository.tree.list`,
`gitlab.repository.file.read`. При каждом чтении gateway заново проверяет
текущую личную credential, project ID/path, PROJECT/GROUP/INSTANCE scope,
capability и approval policy. Connection из prompt не становится разрешением.

Принимается полный lowercase SHA-1/SHA-256 commit ID. Проверяются все страницы
дерева, paths/modes, точный ref и Git blob hashes. Пределы:128 файлов,
128 директорий,384 remote reads,8МиБ суммарно,45с. Файлы дополнительно ограничены
контрактом существующего gateway. Symlinks, submodules, unsafe paths,
неполные/повреждённые ответы и неподтверждённые read approvals закрываются.
Результат сначала собирается полностью, затем атомарно публикуется в новую
не существующую директорию под `work`; прежняя рабочая копия не заменяется.

`representation=API_SNAPSHOT` сообщает source commit SHA и manifest SHA-256.
Это не full git clone, локальный HEAD с remote SHA не выдумывается. Git history,
LFS materialization, submodules и credential-bearing git transport не входят
в профиль. Commit/MR/comment/merge выполняются отдельными типизированными
инструментами gateway с их правами и Human Gate. Сетевой allowlist ради checkout
не расширяется.

## Миграция и проверка

Профиль `managed-workspace` закрепляет переход038→039:
`20261110000200_managed_workspace.sql`. Это одна NOT NULL boolean колонка
`runtime_environment_versions.managed_workspace` с default false. Существующие
окружения не включаются, строки и ACL не переписываются, dump/bootstrap не нужны.
Pre/postcondition проверяют фактическую схему и no-op replay. Изменение runtime
images всё равно требует штатного rollout/ownership и точных immutable pins.

После публикации true-версий старый binary может не понимать их digest.
Отключение — новая false-версия через поддержанный owner command и новый turn;
возврат старого image не объявляется универсальным DB rollback. `goose down`
для этого пути не применяется.

Точки воспроизводимых проверок:

- `go test ./...` в runtimecontract, agent-runner, runtime-controller,
  control-plane и control-api-gateway;
- `go test -race ./internal/managedworkspace ./internal/security` в agent-runner:
  file CAS/quotas, traversal/link/FIFO, current-owner refusal, request cancellation,
  unknown command, exact snapshot/hash и настоящий Landlock в disposable child;
- `scripts/tests/control-plane-postgres-test.sh --managed-workspace-upgrade`:
  реальный038→039, postcondition и replay;
- PostgreSQL `TestOrganizationRuntimeEnvironmentComponent`: true-версия,
  owner publication/history и прежнее false-окружение;
- `scripts/tests/managed-workspace-executor-test.sh`: отдельные PID namespaces,
  отсутствие credential env, запрет подмены UDS, detached-child cleanup, timeout
  и quiescence на локальном Docker с закреплённым публичным образом;
- frontend unit/typecheck/lint/build, contract/codegen и actual Pod render;
- immutable image: реальный executor UID/PID/UDS, detached-child cleanup,
  pinned CLI MCP initialization/catalog и role→gateway→API;
- установленная команда: изменения в разрешённой ветке, настоящий pipeline,
  независимый reviewer checkout/test, scoped comment и отдельный merge gate.

Проверки библиотек: Context7 Go1.26.7 и Vue Composition API; закреплённые
Go1.26.10/Vue3.5.41 проверяются сборкой. Для x/sysv0.48.0 Context7 не дал
Landlock API: использованы точный source и системные вызовы установленной версии.
Официальные источники:
[Landlock](https://docs.kernel.org/userspace-api/landlock.html),
[x/sysv0.48.0](https://github.com/golang/sys/blob/v0.48.0/unix/ztypes_linux.go),
[MCP lifecycle2025-06-18](https://modelcontextprotocol.io/specification/2025-06-18/basic/lifecycle),
[OpenCode1.19.5 MCP config](https://github.com/anomalyco/opencode/blob/v1.19.5/packages/core/src/v1/config/mcp.ts).

Локальные tests/render и green Pods не означают установленную приёмку.
Результаты, source/image/schema pins и NOT RUN записываются в конкретный PR
и deployment receipt. Независимое review нельзя заменять авторским self-review.

## Число шагов обычного turn

Адаптер не задаёт скрытый `agent.build.steps`: в OpenCode1.19.5 отсутствие
этого поля позволяет завершить работу с несколькими инструментами без
принудительного промежуточного отчёта после восьми итераций. Ограничения
Kodex по времени запуска и workflow, отмене, lease, объёму событий и выводу
сохраняются. Новый ввод не разрешает повторять уже принятый внешний эффект;
после неизвестного ответа требуется readback. Это не бесконечный автоматический
retry и не изменение полномочий, модели или режима Codex.

Проверен закреплённый исходник OpenCode1.19.5:
`packages/opencode/src/session/prompt.ts` (`agent.steps ?? Infinity`).

## Допуск Pod управляемой среды

Публикация `managedWorkspace` проходит существующий owner environment/version и
закрепляется в RuntimeRevision вместе с capability и ресурсным бюджетом.
`runtime-controller` остаётся единственным actor создания Pod. Kubernetes policy
проверяет фактическую форму: рабочая копия provider read-only, при выбранных
командах — ровно один `workspace-executor` на том же допущенном образе.
Без команд сохраняются три контейнера; без опции сохраняется прежний профиль.

Executor получает только свою рабочую копию, приватный socket роли и bounded
scratch. Provider/relay/init не получают этот socket; credentials, ticket,
provider state и произвольные mounts/env/commands ему запрещены. UID/GID,
PID isolation, seccomp и прямой защищённый entrypoint закреплены точно.
Сумма ресурсов provider/executor остаётся в прежнем диапазоне, а разница
половин допускает только округление одного milliCPU или byte; новый контейнер
не удваивает опубликованный бюджет. V2 получает только точное `generation/tree`.

`make test-workspace-policy-contract` проверяет **все** CEL validations по Pods
реального producer: прежний профиль, файлы, команды, минимальные/нечётные
ресурсы и V2. Отдельные отрицательные мутации проверяют credential mounts/env,
другой образ/entrypoint, PID/UID/GID, socket, scratch и ресурсный бюджет.
Положительный observer или проверка нескольких выбранных выражений этого
не заменяют. Native допуск и реальный turn после установки остаются отдельной
приёмкой. Для квалифицированного policy-only обновления immutable application
images и RoleImage не пересобираются; source policy фиксируется отдельно.
