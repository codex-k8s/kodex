---
doc_id: ALT-CK8S-REPOSITORY-ONBOARDING-0001
type: alternatives
title: kodex — варианты bootstrap/adoption репозитория
status: active
owner_role: SA
created_at: 2026-05-14
updated_at: 2026-06-02
related_issues: [281, 282, 761, 794, 810, 818, 840, 864, 865, 881, 883, 917, 1011]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-14-repository-onboarding-choice"
  decision: "option-c-hybrid"
---

# Варианты bootstrap/adoption репозитория

## TL;DR

- Рассмотрены три варианта работы с `services.yaml`, внешней документацией, шаблонами репозиториев и пакетами при первичной инициализации пустого репозитория и подключении существующего репозитория.
- Владелец выбрал вариант C: гибридная модель.
- Суть варианта C: `services.yaml` остаётся версионируемой декларацией проекта в репозитории, `project-catalog` хранит проверенную проекцию, `package-hub` хранит установки пакетов, workspace собирает платформа, а Git submodule используется только по явному решению владельца.
- Bootstrap/adoption может выполняться двумя способами: агентом с профильной ролью или детерминированным кодовым исполнителем по выбранному шаблону репозитория.
- Этот документ фиксирует архитектурный выбор, но не закрывает реализацию bootstrap/adoption.

## Контекст

Платформа должна уметь:

- создать проект и пустой репозиторий с базовым `services.yaml`;
- подключить существующий репозиторий, просканировать его и подготовить отчёт о подключении;
- подключить проектную документацию из одного или нескольких репозиториев;
- подключить руководящие пакеты и пакеты из магазина без копирования их текстов и исходников в БД;
- подключить системный или пользовательский шаблон репозитория: Go-сервис, Python-сервис, фронтенд на Vue, монорепозиторий, документация и будущие специализированные шаблоны;
- дать агенту локальный рабочий контур с кодом, проектной документацией, документацией зависимых сервисов и руководящими пакетами;
- сохранить управление проектной политикой через PR, а не через неявное редактирование БД.

Согласованные границы сервисов:

| Сервис | Ответственность в bootstrap/adoption |
|---|---|
| `project-catalog` | Владеет проектом, repository binding, `services.yaml`, проектной политикой и проверенной проекцией. Не является Git-клиентом. |
| `provider-hub` | Выполняет provider-native операции: читать репозиторий, снять lightweight scan snapshot без содержимого файлов, создать репозиторий, создать или обновить ветку, Issue, PR/MR, комментарий, связь и зеркало; отдаёт safe scan snapshots и merge signals через gRPC read surface. Создание самого репозитория остаётся отдельной командой и не смешивается с генерацией `services.yaml`, выбором шаблона, deep adoption scan/report и bootstrap branch/PR. |
| `package-hub` | Владеет пакетами, шаблонами репозиториев, источниками, установками, manifest и состоянием магазина пакетов. Не выполняет checkout файлов пакета. |
| `agent-manager` | Запускает агентные роли и детерминированные bootstrap/adoption-запуски, которые готовят PR, отчёт подключения, проверки и рекомендации. |
| `runtime-manager` | Материализует workspace по политике, `source_ref`, шаблонам и установкам пакетов. Не владеет проектной политикой. |

## Неподвижные правила

- `services.yaml` — версионируемая декларация проекта в репозитории.
- `project-catalog` хранит проверенную проекцию `services.yaml` и не становится Git-клиентом.
- Provider-native запись идёт через `provider-hub`.
- Тексты внешних документов, руководящих пакетов и файлов пакетов не дублируются в БД.
- Workspace собирается из `services.yaml`, установленных пакетов, `source_ref` и разрешений.
- Git submodule не является обязательным механизмом для всех внешних источников.
- Изменение декларативной политики штатно проходит через PR.
- После слияния PR с изменением `services.yaml` webhook или сверка передаёт новую версию в `project-catalog`, который валидирует и импортирует проверенную проекцию.
- После merge/push в `main` для собственного репозитория платформы safe provider/project signal может породить `agent-manager` self-deploy plan: pending state с project/repository refs, source/merge commit refs, affected service keys, path categories, `services.yaml` ref/digest, expected runtime job types и governance refs. Такой план не запускает build/deploy автоматически и ждёт owner/governance approval.
- Шаблон репозитория является пакетом особого вида: его manifest описывает входные параметры, набор файлов, допустимые операции копирования/рендера, начальные секции `services.yaml`, инструкции для агентов и правила конфликтов.
- Системные шаблоны поставляются платформой, пользовательские шаблоны подключаются через `package-hub` и проходят те же правила версий, прав, manifest и верификации.
- Детерминированный исполнитель по шаблону не должен молча перезаписывать существующие файлы: конфликт фиксируется в отчёте, а изменение продолжается только после выбора владельца или через агентную роль.

## Режимы выполнения bootstrap/adoption

### Агентный режим

Агентный режим нужен, когда репозиторий уже содержит неоднозначную структуру, требуется анализ старого кода, нужно объяснить риски или подготовить нестандартный PR.

1. `agent-manager` запускает профильную роль: например `repository-bootstrap`, `repository-adoption` или будущую проектную роль владельца.
2. `runtime-manager` собирает workspace из целевого репозитория, шаблонов, руководящих пакетов и доступных источников документации.
3. Агент анализирует репозиторий, формирует отчёт, предлагает структуру `services.yaml`, выбирает или уточняет шаблон, добавляет инструкции и готовит PR через provider-контур.
4. `provider-hub` создаёт ветку, коммиты, PR/MR, комментарии и связи у провайдера.
5. Владелец принимает PR; после merge `project-catalog` импортирует проверенную проекцию.

### Детерминированный режим по шаблону

Детерминированный режим нужен для быстрых и повторяемых случаев, где не требуется анализ агентом: новый Go-сервис, Python-сервис, фронтенд на Vue, монорепозиторий или репозиторий документации по заранее заданному шаблону.

1. Пользователь выбирает шаблон репозитория и задаёт параметры: имя сервиса, язык, путь, набор руководящих пакетов, внешние документы, `package_ref` и режим доступа.
2. `package-hub` проверяет доступность шаблона, его версию, manifest, права, секреты и зависимости.
3. `project-catalog` формирует черновик проектной политики и ожидаемый фрагмент `services.yaml`.
4. `runtime-manager` материализует временный workspace с исходным шаблоном и целевым репозиторием.
5. Кодовый исполнитель применяет manifest шаблона: копирует файлы, рендерит переменные, создаёт `services.yaml`, добавляет локальные `AGENTS.md`/`README.md` и подключает нужные `source_ref` или `package_ref`.
6. Перед записью исполнитель проверяет конфликты путей, запрет на перезапись, несоответствие существующего `services.yaml`, недоступные источники и несовместимые версии.
7. Если конфликтов нет, `provider-hub` создаёт PR с результатом. Если конфликты есть, создаётся отчёт подключения и пользователь выбирает: изменить параметры, разрешить перезапись там, где это допустимо, или перейти в агентный режим.
8. После merge `project-catalog` импортирует проверенную проекцию из commit.

Оба режима сходятся в одном результате: provider-native PR с изменениями и последующий импорт `services.yaml` в `project-catalog`.

## Сценарии

### Пустой репозиторий

1. Пользователь или оператор создаёт проект и выбирает режим bootstrap.
2. `project-catalog` создаёт проект и repository binding в состоянии ожидания provider-операции.
3. Если репозиторий ещё не создан у провайдера, проектный или агентный контур вызывает project-side команду `project-catalog CreateProviderRepository`. `project-catalog` резервирует project/repository binding, делегирует provider-native создание в `provider-hub CreateRepository`, сохраняет только безопасные provider refs и provider default branch как `base_branch` в binding.
4. Пользователь выбирает способ подготовки:
   - агентный режим, если нужно проектирование структуры или нестандартный bootstrap;
   - детерминированный режим по шаблону, если достаточно системного или пользовательского шаблона.
5. Для агентного режима `agent-manager` запускает bootstrap-роль, которая формирует начальный `services.yaml`, базовую структуру документации и описание ожидаемого состава проекта.
6. Для детерминированного режима `package-hub` отдаёт manifest шаблона, а кодовый исполнитель создаёт файлы, `services.yaml` и локальные инструкции по шаблону без анализа агентом.
7. `project-catalog` принимает project-side bootstrap-команду по существующему repository binding: проверяет provider target, `base_branch`, подготовленные файлы, watermark и связь с проверенной политикой `services.yaml`.
8. `project-catalog` вызывает `provider-hub CreateBootstrapPullRequest` с готовым provider target, refs, файлами и policy context. `provider-hub` использует уже созданный репозиторий, создаёт или обновляет bootstrap branch и PR с подготовленным bootstrap-набором файлов. Bootstrap-команда допускает пустой base branch или `README.md`, созданный GitHub при `auto_init`.
9. Владелец проверяет PR у провайдера и подтверждает переход через merge.
10. Webhook или сверка провайдера фиксирует merge; `provider-hub` сохраняет safe merge signal для bootstrap/adoption `PR/MR`, связывает merged projection с `project_id` и `repository_id`, публикует `provider.repository.bootstrap_merged` или `provider.repository.adoption_merged` и отдаёт этот provider-owned факт через gRPC read surface. Consumer `project-catalog` читает эти события через общий `platform-event-log` runtime, проверяет тип/версию события и восстанавливает только safe merge signal. Для bootstrap он вызывает `ReconcileBootstrapMergeSignal`, для adoption — `ReconcileAdoptionMergeSignal`, если событие содержит checked artifact metadata, normalized `validated_payload_json` и watermark payload. Оба reconcile-use-case доступны как публичные gRPC команды `project-catalog`, поэтому внешний Go integration runner может использовать product API, а не внутренние consumer или БД. Если checked artifact metadata ещё не переданы в event-driven контур, consumer записывает `OnboardingSignalReconciliation(needs_review)` с безопасной причиной и не импортирует `services.yaml` по неполным данным.
11. Когда внутренний контур передаёт в `project-catalog ReconcileBootstrapMergeSignal` safe bootstrap signal и checked artifact metadata — provider target, `signal_key`, `base_branch`, `source_ref`, commit, artifact ref/digest/version, `content_hash`, watermark digest/payload и нормализованный `services.yaml` — `project-catalog` сверяет signal/artifact с project/repository binding, проверяет ожидаемую версию pending binding, фиксирует project-side `OnboardingSignalReconciliation` со safe fingerprint/status/error summary, вызывает `ImportBootstrapServicesPolicy`, импортирует проверенную политику штатным контуром и переводит repository binding в `active`. Повтор того же signal/commit/source ref возвращает уже сохранённую проекцию и обновляет тот же статус обработки, а другой commit/ref или другой fingerprint по тому же signal key считается конфликтом bootstrap-завершения.

В реализованном project-side контуре пустого репозитория покрыты пять шагов модели C: создание provider-native репозитория с фиксацией `base_branch` в project-owned binding, создание bootstrap PR по уже подготовленному payload, event consumer для safe merge signal, event-driven вызов `ReconcileBootstrapMergeSignal` при наличии checked artifact/payload, импорт проверенной `services.yaml` после merge с активацией binding и project-side журнал результата обработки provider signal. Для существующего репозитория добавлен симметричный project-side import path после adoption merge: lightweight scan остаётся planning-сигналом, а checked artifact/payload из `provider.repository.adoption_merged` запускает `ReconcileAdoptionMergeSignal`, импорт checked projection и активацию или обновление binding. Provider-side контур фиксирует safe merge signal и lightweight snapshot существующего репозитория, отдаёт эти provider-owned данные через gRPC read surface, но выбор и применение шаблона, deep workspace scan/report и adoption decision остаются отдельными шагами модели C.

### Проверочный Go runner через product API

Минимальный проверочный контур onboarding живёт в `cmd/onboarding-runner`. Это отдельный Go runner, а не `shell` smoke и не скрытый consumer. Он использует только публичные gRPC product API `project-catalog` и `provider-hub`, не читает внутренние таблицы, не вызывает GitHub/GitLab напрямую и не собирает сырой `services.yaml`.

Режим по умолчанию — dry-run/plan. Runner проверяет доступность `project-catalog`, `provider-hub`, project/repository binding, provider-owned merge signal read surface, adoption scan read surface и наличие checked input для bootstrap/adoption reconciliation. В этом режиме не выполняются mutating RPC.

Режим apply включается только явным `--apply` или `KODEX_ONBOARDING_RUNNER_APPLY=true` и требует safe target policy: разрешённый provider owner и префикс тестового repository name. Apply вызывает `ReconcileBootstrapMergeSignal` и `ReconcileAdoptionMergeSignal` только при наличии проверенного входа сценария: safe provider merge refs, artifact ref/digest/version, `content_hash`, watermark payload и нормализованный checked `validated_payload_json`. Эти значения передаются в product API как typed input, но runner не печатает сырой YAML, webhook body, provider response, diff, token, DSN, private URL или полный checked payload.

Runner умеет подготовить checked artifact input из уже нормализованного `validated_payload_json`: проверяет, что вход является JSON-объектом, вычисляет `content_hash` как обычный SHA-256 от байтов этого payload, использует совместимый `artifact_digest = content_hash`, формирует artifact ref/version, связывает версию с merge commit provider signal или явно переданной версией и добавляет watermark payload из сценария или отдельного безопасного JSON-файла. Этот producer path не валидирует сырой `services.yaml`, не читает файлы репозитория и не печатает checked payload; он нужен только для воспроизводимой проверки typed `ReconcileBootstrapMergeSignal`/`ReconcileAdoptionMergeSignal` через product API.

Для пустого репозитория runner поддерживает этап `bootstrap_setup` в сценарии: dry-run проверяет готовность `CreateProviderRepository` и `CreateRepositoryBootstrapPullRequest`, а apply по той же safe target policy вызывает эти product API через `project-catalog`. Если repository binding ещё не известен, apply сначала создаёт provider repository через `CreateProviderRepository`, получает `repository_id` и безопасные provider refs из ответа, затем вызывает bootstrap PR команду с подготовленными файлами, watermark и проверенной `RepositoryBootstrapServicesPolicy`. Runner не ходит напрямую в GitHub/GitLab и не читает БД; содержимое подготовленных файлов передаётся только в typed product API для provider write, не печатается в выводе и не используется как checked artifact для импорта. Checked artifact остаётся отдельным безопасным входом: refs, digests, version, `content_hash`, watermark payload и нормализованный `validated_payload_json`.

После создания bootstrap PR runner не имитирует merge и не подменяет webhook/consumer path. Если safe provider merge signal ещё не появился, apply завершает setup и помечает reconcile как ожидающий merge signal/checked input. Полный пользовательский сценарий #281/#282 остаётся открытым до операторского запуска, подготовки adoption/deep scan/report и штатного слияния владельцем.

### Существующий репозиторий

1. Пользователь или оператор указывает provider ref существующего репозитория.
2. Проектный или агентный контур вызывает `provider-hub ScanRepositoryForAdoption` с provider target refs, выбранным внешним аккаунтом, branch/ref policy и bounded scan options.
3. `provider-hub` читает только provider metadata/ref/tree, фиксирует safe snapshot: default/scanned ref, head sha, marker path refs/digests/counts, bounded warnings и snapshot digest; содержимое файлов, diff/archive и provider response не сохраняются. Snapshot доступен соседним сервисам через `GetRepositoryAdoptionScanSnapshot`/`ListRepositoryAdoptionScanSnapshots` с safe refs/status/timestamps/version/etag.
4. `project-catalog` использует snapshot как вход planning: проверяет состояние repository binding, ожидаемый `source_ref`, наличие безопасных маркеров и необходимость deep scan, но не читает provider напрямую. Lightweight scan snapshot не содержит checked `services.yaml` payload, поэтому сам по себе не создаёт `ServicesPolicy`; импорт выполняется только после checked artifact/import сигнала.
5. Если репозиторий подходит под выбранный шаблон и конфликтов нет, детерминированный исполнитель готовит payload для PR: добавляет или обновляет `services.yaml`, локальные инструкции, скелет документации и ссылки на руководящие пакеты.
6. Если структура неоднозначна или есть конфликты, `agent-manager` запускает adoption-роль в read-only workspace.
7. `runtime-manager` материализует исходный репозиторий, выбранные шаблоны и доступные руководящие пакеты без изменения целевого репозитория.
8. Adoption-роль сканирует структуру, языки, сервисы, документацию, риски, наличие `services.yaml`, веточные правила и возможные конфликты.
9. Результатом является отчёт подключения и, если нужно, готовый набор файлов, refs, заголовок и тело PR с добавлением или исправлением `services.yaml`, документационных ссылок и минимальных политик.
10. `provider-hub` создаёт или обновляет adoption branch и reviewable PR/MR по этому готовому payload, не выполняя deep scan и не принимая проектное решение.
11. Владелец принимает решение по отчёту и подтверждает переход через merge bootstrap/adoption PR.
12. После merge `provider-hub` публикует `provider.repository.adoption_merged` с safe merge refs. Если событие содержит checked artifact metadata, normalized `validated_payload_json` и watermark payload для `repository_adoption`, consumer `project-catalog` вызывает `ReconcileAdoptionMergeSignal`; тот же use-case доступен через публичный gRPC RPC с typed `RepositoryAdoptionMergeSignal` и `CheckedAdoptionServicesPolicyArtifact`. Команда сверяет provider refs, binding, `base_branch`, merge commit, artifact digest/version, watermark digest и fingerprint, импортирует checked projection штатным контуром `ServicesPolicy` и активирует или обновляет repository binding. Если событие содержит только scan/merge refs без checked artifact input, `project-catalog` записывает `needs_review` и не импортирует lightweight scan snapshot как политику.

### Подключение внешней документации и пакетов

1. Владелец проекта добавляет в `services.yaml` источник проектной документации или ссылку на руководящий пакет.
2. Если источник является установкой пакета, его состояние и версия принадлежат `package-hub`.
3. Если источник является шаблоном репозитория, его manifest задаёт, какие файлы можно копировать, какие переменные рендерить и какие инструкции должны попасть в целевой репозиторий.
4. Если источник является provider-native репозиторием, доступ и чтение идут через provider-контур и runtime-материализацию, а не через прямой Git-код в `project-catalog`.
5. `project-catalog` валидирует декларацию, безопасные локальные пути, режим доступа и область применения источника.
6. `agent-manager` получает проектную политику и список установленных руководящих пакетов.
7. `runtime-manager` собирает локальный workspace: код, проектные документы, документы сервисов, документы зависимостей, шаблоны и руководящие пакеты.
8. Самоулучшение документации выполняется в репозитории-источнике этой документации через отдельную provider-native ветку и PR, а не через запись текста в БД платформы.

## Вариант A: всё подключать как Git submodule в продуктовый репозиторий

### Форма `services.yaml`

`services.yaml` хранит локальные пути, которые предполагают наличие submodule:

```yaml
project:
  repositories:
    - key: product
      root: .
  documentation:
    - scope: project
      local_path: docs/product
    - scope: guidance
      local_path: docs/external/guidelines/common
  packages:
    - key: telegram-approval
      local_path: packages/telegram-approval
```

### Документация и пакеты

- Продуктовая документация живёт в основном репозитории или подключённых submodule.
- Руководящие пакеты подключаются как submodule в `docs/external/**`.
- Пакеты из магазина подключаются как submodule в `packages/**`.

### Получение файлов агентом

`runtime-manager` делает checkout основного репозитория и всех submodule, затем отдаёт агенту готовые локальные пути.

### Данные в БД

- `project-catalog` хранит проверенную проекцию `services.yaml` и локальные пути.
- `package-hub` всё равно хранит установки пакетов и manifest, иначе теряется пакетная модель.
- `provider-hub` хранит provider-проекции основного репозитория и submodule-репозиториев, если они участвуют в операциях.

### Изменения через PR

Изменение внешнего источника выражается PR с обновлением `.gitmodules`, gitlink или локального пути в `services.yaml`.

### Самоулучшение документации

Агент должен открыть PR в репозитории руководства, затем отдельный PR в продуктовый репозиторий для обновления gitlink, если нужна новая версия.

### Плюсы

- Максимальная Git-воспроизводимость в одном checkout.
- Владелец видит точные gitlink-версии в PR.
- Подходит для проектов, которые сознательно хотят vendor-подход к внешним источникам.

### Минусы и риски

- Платные и приватные пакеты сложно безопасно отдавать производный клонам.
- Submodule создают высокий операционный шум: конфликт gitlink, доступы, вложенные checkout, поломанные clone у пользователей без прав.
- Пакетная модель дублируется: источник есть и в Git submodule, и в `package-hub`.
- Магазин пакетов становится похож на набор Git-ссылок, а не на управляемые установки с проверкой manifest, секретов, прав и коммерческого статуса.
- Adoption существующего репозитория становится более рискованным: PR меняет не только `services.yaml`, но и структуру submodule.

### Влияние на домены

| Домен | Влияние |
|---|---|
| `project-catalog` | Читает декларацию с локальными путями, но вынужден валидировать много submodule-специфики. |
| `provider-hub` | Должен чаще работать с несколькими репозиториями и PR для gitlink. |
| `package-hub` | Рискует потерять роль владельца установки, если submodule начинают считаться истиной пакета. |
| `agent-manager` | Получает простой локальный путь, но сложнее объясняет, какая версия руководства была использована. |
| `runtime-manager` | Усложняется checkout и обработка частично недоступных submodule. |

## Вариант B: `services.yaml` хранит `source_ref`, платформа материализует внешние источники только в workspace

### Форма `services.yaml`

`services.yaml` хранит переносимые ссылки на источники:

```yaml
project:
  repositories:
    - key: product
      provider: github
      owner: example
      name: product
      default_branch: main
  documentation:
    - scope: project
      source_ref:
        provider: github
        owner: example
        name: product-docs
        ref: main
        path: docs
      local_path: workspace/docs/project
      access_mode: read
  guidance:
    - package_ref: guidance.common
      version: 1.4.0
      local_path: workspace/guidance/common
```

### Документация и пакеты

- Продуктовая документация может жить в отдельном репозитории и описывается `source_ref`.
- Руководящие документы выбираются через установку пакета и manifest.
- Пакеты из магазина не становятся submodule: их доступность и версия живут в `package-hub`.

### Получение файлов агентом

`agent-manager` собирает намерение запуска, `project-catalog` отдаёт политику workspace, `package-hub` отдаёт установки и manifest, а `runtime-manager` checkout-ит нужные `source_ref` только в slot workspace.

### Данные в БД

- `project-catalog` хранит проверенную проекцию `source_ref`, локальные пути, режим доступа и digest политики.
- `package-hub` хранит установки и manifest.
- `provider-hub` хранит проекции provider-native источников и выполняет операции при необходимости.
- Тексты файлов не хранятся в БД.

### Изменения через PR

Изменения источников проектной документации проходят через PR к `services.yaml`. Изменение установки пакета проходит через `package-hub` и фиксируется в его аудите; при необходимости ссылка на установленный пакет также отражается в `services.yaml`.

### Самоулучшение документации

Агент открывает PR напрямую в репозиторий-источник документации или руководящего пакета. Продуктовый репозиторий не обязан получать отдельный PR, если ссылка в `services.yaml` указывает на канал версии, принятый политикой.

### Плюсы

- Нет обязательных submodule и связанных с ними конфликтов.
- Платные и приватные пакеты не протекают в производный клон.
- Workspace можно собирать точечно под задачу, не раздувая основной репозиторий.
- Лучше соответствует разделению ответственности `project-catalog`, `package-hub`, `provider-hub` и `runtime-manager`.

### Минусы и риски

- В продуктовой Git-истории хуже видно точный набор файлов, который получил агент.
- Воспроизводимость требует хранить digest материализации и `source_ref` в runtime/agent-состоянии.
- Владелец может хуже понимать, что реально попадёт в workspace, если UI не показывает итоговую раскладку.
- Нужна дисциплина: `project-catalog` не должен превращаться в сервис checkout, а `runtime-manager` не должен становиться владельцем политики.

### Влияние на домены

| Домен | Влияние |
|---|---|
| `project-catalog` | Хранит переносимые `source_ref` и проверенную проекцию без Git-клиента. |
| `provider-hub` | Даёт операции и проекции источников, но не владеет политикой. |
| `package-hub` | Остаётся владельцем установок и manifest; checkout не выполняет. |
| `agent-manager` | Фиксирует версии политики, пакетов и prompt в `Run`. |
| `runtime-manager` | Материализует workspace по `source_ref` и фиксирует digest результата. |

## Вариант C: гибридная модель

### Форма `services.yaml`

`services.yaml` остаётся версионируемой декларацией проекта, но не заставляет все внешние источники быть submodule:

```yaml
project:
  repositories:
    - key: product
      provider: github
      owner: example
      name: product
      default_branch: main
  documentation:
    - key: product-docs
      scope: project
      source_ref:
        provider: github
        owner: example
        name: product-docs
        ref: main
        path: docs
      local_path: docs/project
      access_mode: read
    - key: local-adr
      scope: project
      source_ref:
        kind: repository_path
        repository_key: product
        path: docs/adr
      local_path: docs/adr
      access_mode: write
  guidance:
    - package_ref: guidance.common
      installation_scope: project
      local_path: guidance/common
  repository_templates:
    - template_ref: system.go-service
      version: 1.0.0
      target_path: services/api/orders
      mode: render
  packages:
    - package_ref: telegram-approval
      installation_scope: project
      desired_state: active
  source_checkout:
    submodules:
      mode: explicit
```

### Документация и пакеты

- Продуктовая документация может жить в основном репозитории, отдельном проектном репозитории или явно подключённом submodule.
- Руководящие пакеты и пакеты из магазина выбираются через `package-hub`.
- Шаблоны репозиториев выбираются через `package-hub` и могут быть системными, пользовательскими или опубликованными в магазине.
- Git submodule разрешён только там, где владелец осознанно хочет зафиксировать внешний source прямо в продуктовой Git-истории.

### Получение файлов агентом

1. `agent-manager` выбирает flow, роль и руководящие пакеты.
2. `project-catalog` отдаёт проверенную политику workspace из `services.yaml`.
3. `package-hub` отдаёт установки, manifest пакетов и manifest выбранных шаблонов.
4. `runtime-manager` собирает workspace из репозиториев, `source_ref`, шаблонов, установленных пакетов и явных submodule, если они разрешены политикой.
5. `agent-manager` фиксирует в `Run` версии политики, пакетов, prompt и digest материализации workspace.

### Данные в БД

| Сервис | Что хранит |
|---|---|
| `project-catalog` | Проверенную проекцию `services.yaml`, repository binding, documentation source, политику workspace, `source_ref`, локальные пути и режимы доступа. |
| `provider-hub` | Provider-native проекции, операции, PR/MR, связи, webhook, курсоры и лимиты. |
| `package-hub` | Источники пакетов, шаблоны репозиториев, доступный каталог, версии, manifest, установки, статусы секретов и верификации. |
| `agent-manager` | Версии flow, role, prompt, `Run`, acceptance, выбранные ссылки на пакеты и digest контекста. |
| `runtime-manager` | Slot, job, материализация workspace, source digest, ошибки checkout и локальные пути. |

Файлы документации, исходники пакетов и тексты руководящих пакетов в БД не копируются.

### Изменения через PR

- Изменение проектной декларации, `source_ref`, локальных путей и режима доступа идёт через PR к `services.yaml`.
- Создание или изменение установки пакета идёт через `package-hub` с аудитом и правами; если установка является частью проектной декларации, PR к `services.yaml` ссылается на package ref, а не копирует manifest.
- Применение шаблона репозитория создаёт PR с файлами, которые manifest шаблона разрешил скопировать или отрендерить; сам шаблон остаётся в своём источнике и не копируется в БД.
- Изменение внешней документации идёт PR в репозиторий-источник этой документации.
- Изменение submodule допускается только при явном решении владельца и фиксируется как обычный PR с gitlink.

### Самоулучшение документации

Самоулучшение запускается против репозитория-источника:

- проектная документация меняется в проектном или документационном репозитории;
- руководящий пакет меняется в репозитории руководящего пакета;
- пакет из магазина меняется в репозитории пакета;
- после merge источник публикует новую версию, а `package-hub` или `project-catalog` обновляет проверенную проекцию по своему контракту.

### Плюсы

- Сохраняет Git-управление проектной политикой и ревью через PR.
- Не заставляет все внешние источники быть submodule.
- Поддерживает быстрый bootstrap по системным шаблонам без запуска агента.
- Не размывает границы: `project-catalog` хранит проектную политику, `package-hub` хранит установки пакетов, `runtime-manager` материализует workspace.
- Поддерживает платные, приватные и ограниченные для производных установок пакеты.
- Позволяет владельцу выбрать submodule точечно, когда нужна жёсткая Git-воспроизводимость.

### Минусы и риски

- Модель сложнее, чем чистый submodule-подход.
- Нужен хороший UI для объяснения итогового workspace: какие источники, версии, доступы и локальные пути получит агент.
- Нужно хранить и показывать digest материализации, иначе владелец будет видеть только декларацию, но не фактический workspace.
- Требуется междоменная дисциплина: нельзя переносить checkout в `project-catalog` или истину об источнике пакета в `runtime-manager`.

### Влияние на домены

| Домен | Влияние |
|---|---|
| `project-catalog` | Владеет `services.yaml`, проверенной проекцией, `source_ref` и политикой workspace; не делает checkout. |
| `provider-hub` | Выполняет создание репозитория, PR, комментарии, связи, provider-native чтения и ускоряющую сверку. |
| `package-hub` | Владеет установками, версиями и manifest; не хранит файлы и не выполняет checkout. |
| `agent-manager` | Запускает bootstrap/adoption роли, фиксирует выбранные версии и отчёт приёмки. |
| `runtime-manager` | Материализует workspace из нескольких источников и фиксирует digest результата. |

## Сравнение вариантов

| Критерий | A: все submodule | B: только `source_ref` | C: гибрид |
|---|---:|---:|---:|
| Простота Git-ревью | высокая | средняя | высокая для проектной политики, точечная для источников |
| Безопасность платных и приватных пакетов | низкая | высокая | высокая |
| Производные клоны без приватного доступа | слабые | хорошие | хорошие |
| Граница `package-hub` | размыта | чистая | чистая |
| Воспроизводимость workspace | высокая через Git | требует digest | высокая через политику и digest |
| Операционная сложность checkout | высокая | средняя | средняя |
| Гибкость для multi-repo проекта | средняя | высокая | высокая |
| Риск конфликтов в PR | высокий | низкий | средний только при явном submodule |

## Выбранное решение

Владелец выбрал вариант C как целевую модель bootstrap/adoption.

Обоснование:

- он сохраняет `services.yaml` как переносимую и ревьюируемую декларацию проекта;
- он не превращает `project-catalog` в Git-клиент;
- он оставляет пакетную истину в `package-hub`;
- он позволяет использовать системные и пользовательские шаблоны репозиториев без обязательного запуска агента;
- он позволяет собрать workspace агента из нескольких источников без копирования файлов в БД;
- он не ломает платные, приватные и производные сценарии;
- он допускает submodule там, где владелец осознанно выбирает Git-воспроизводимость вместо меньшей операционной сложности.

Открытые проектные уточнения для следующих срезов:

1. Решить, должна ли установка руководящего пакета всегда отражаться в `services.yaml` или `services.yaml` может ссылаться на package scope, который полностью управляется `package-hub`.
2. Уточнить минимальный manifest системного шаблона репозитория: параметры, операции копирования/рендера, секции `services.yaml`, локальные инструкции и правила конфликтов.
3. Решить, какие `source_ref` требуют обязательного подтверждения владельца: приватные репозитории, платные пакеты, write-доступ к документации, submodule.

## Что делать после выбора

- Обновить доменные документы `projects-and-repositories`, `provider-native-work-items`, `package-platform`, `agent-orchestration` и `runtime-and-fleet` под выбранное решение C.
- Разложить bootstrap/adoption на отдельные малые срезы: проектная политика, provider-операции, агентный отчёт подключения, материализация workspace и приёмка.
- Выделить отдельный срез на системные шаблоны репозиториев: Go-сервис, Python-сервис, фронтенд на Vue, монорепозиторий и документационный репозиторий.
- Зафиксировать минимальный контракт между `project-catalog`, `provider-hub`, `package-hub`, `agent-manager` и `runtime-manager`.
- Только после этого начинать реализацию bootstrap/adoption.

## Апрув

- request_id: `owner-2026-05-14-repository-onboarding-choice`
- Решение: выбран вариант C.
- Комментарий: документ фиксирует согласованную гибридную модель bootstrap/adoption; реализация сценариев подключения репозиториев остаётся отдельными срезами.
