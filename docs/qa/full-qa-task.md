---
id: QA-DOC-SELFDEV-001
title: Полный сценарий самонастройки и dogfooding Kodex
type: qa
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-05
---

# Актуальная редакция задания

Этот документ сохраняет все 65 разделов согласованного полного QA-задания.
Исходный PR #1790 уже слит; актуальные bootstrap Issue [#1797](https://github.com/codex-k8s/kodex/issues/1797) и Draft PR [#1798](https://github.com/codex-k8s/kodex/pull/1798).
Ссылки на bootstrap PR ниже актуализированы; последовательность и критерии приёмки сохранены.

Checklist, уточнения владельца и точный журнал проверок: [самонастройка и разработка](../operations/self-development-dogfooding.md).
Краткая точка продолжения: [состояние на паузе](../operations/self-development-handoff.md).
Bootstrap ведётся одним сквозным PR как явно согласованное исключение. Внешние доработки проверяются адресными быстрыми тестами и сквозными сценариями без отдельного внешнего review; внутренние Documentation/Security/Lexical reviews Workflow обязательны. Живое доказательство не заменяется unit-тестом, зелёным Pod или скриншотом.
Внешний управляющий агент не подменяет команду платформы. Итоговый dogfooding PR остаётся для ручной приёмки, без merge/auto-merge/owner approve.

05.10.2026 владелец явно возобновил работу после проверки переноса хранилища.
Текущая точка продолжения и результаты проверок фиксируются в связанном журнале.

# Главная цель

Нужно провести полноценный dogfooding платформы Kodex на разработке самого Kodex.

Работа должна идти в несколько последовательных уровней:

1. Ты как внешний host-агент сначала заканчиваешь необходимые изменения самой платформы в текущем PR `#1798`.
2. После появления необходимых возможностей используешь ОБЩЕСИСТЕМНОГО помощника Kodex для настройки самого общесистемного помощника.
3. Проверяешь его фактический runtime, prompt, инструменты, Context7 и сеть.
4. Через общесистемного помощника создаёшь проект `Kodex | Dev`.
5. Через общесистемного помощника создаёшь и полностью настраиваешь отдельного ПОМОЩНИКА ПРОЕКТА `Kodex | Dev`.
6. Проверяешь фактический runtime и prompt уже проектного помощника.
7. После этого работа по настройке команды проекта должна выполняться преимущественно через помощника проекта `Kodex | Dev`.
8. Помощник проекта создаёт ИИ-сотрудников, окружения, grants, workflow и остальные проектные ресурсы.
9. После полной верификации bootstrap-конфигурации текущий PR `#1798` сливается в `main`, тестовый стенд обновляется на свежий `main`.
10. На свежем `main` выполняется полноценный end-to-end Workflow по реальной GitHub Issue.
11. Результатом должен стать Pull Request, полностью прошедший внутренний многоагентный review и готовый к моему человеческому review.
12. Итоговый dogfooding Pull Request НЕ MERGE.

Главный критерий:

```text
GitHub Issue
→ Project Manager
→ System Analyst & Architect
→ Developer
→ Documentation Review
→ Security Review
→ Lexical Review
→ Developer fixes
→ повторные Reviews
→ Project Manager final check
→ Pull Request
→ READY_FOR_HUMAN_REVIEW
```

Но важнейшая часть эксперимента — проверить не только конечный PR, а весь механизм настройки самой платформы через её собственных системного и проектного помощников.

---

# 1. Твоя роль

Ты — внешний управляющий агент, работающий непосредственно на Ubuntu-хосте.

У тебя есть:

- repository:
  `/home/s/projects/kodex`;
- доступ к Kubernetes-кластеру тестового Kodex;
- `kubectl`;
- GitHub;
- локальная разработка;
- Google Chrome через MCP;
- возможность изменять исходники Kodex;
- возможность обновлять тестовый стенд;
- Context7;
- GitHub credentials.

Ты НЕ являешься одним из ИИ-сотрудников будущего проекта.

Твоя обязанность:

- доработать платформу;
- управлять bootstrap;
- пользоваться системным/проектным помощником;
- проверять каждое действие помощников;
- исправлять платформу или prompts помощников, если они работают неправильно;
- не подменять сотрудников во время финального end-to-end Workflow.

---

# 2. Защита учётных данных

Учётные данные передаются только через защищённые механизмы платформы, с минимальными правами для каждой роли. Значения не публикуются в переписке, prompts, логах, GitHub, аргументах, URL, screenshots и artifacts. Внешний управляющий агент не передаёт сотрудникам административные полномочия владельца.

---

# 3. Эталонный профиль Context7

Перед настройкой согласовать рабочий MCP profile Context7: transport, command/url, имена необходимых env bindings и MCP parameters. Зафиксировать безопасные metadata и immutable revision. Значения передавать только через protected Secret binding доверенному MCP adapter/server; не включать их в prompts и shell окружение агента.

---

# 4. Context7 должен быть доступен ВСЕМ ИИ-агентам

Не ограничивай Context7 только Developer/Architect.

Context7 должен быть доступен:

- общесистемному помощнику;
- помощнику проекта `Kodex | Dev`;
- Project Manager;
- System Analyst & Architect;
- Developer;
- Documentation Reviewer;
- Security Reviewer;
- Lexical Guardian.

Для этого нужно штатным механизмом Kodex передать каждому runtime эквивалент конфигурации:

```toml
[mcp_servers.context7]
...

[mcp_servers.context7.env]
...
```

на основании согласованного управляемого MCP profile Context7.

Не разрешается просто записать Context7 config вручную в каждый контейнер в обход платформы.

Это должно быть выражено через штатные:

- настройки помощника;
- настройки сотрудника;
- environment;
- secret/env binding;
- MCP/tool configuration;

которые разрабатываются в PR `#1798`.

Если текущая реализация не позволяет корректно передать MCP configuration и secret env каждому Agent — это platform defect, который нужно сначала исправить.

---

# 5. Начальная проверка repository

Перед работой:

```bash
cd /home/s/projects/kodex
git status
git remote -v
git fetch --all --prune
```

Не раскрывать credential из remote.

Через GitHub получить актуальные:

- `main`;
- открытые PR;
- открытые Issues;
- состояние PR `#1798`;
- его HEAD;
- mergeability;
- checks;
- branch protections.

Работать с фактическим HEAD.

---

# 6. Текущий PR #1798

Основные изменения bootstrap должны продолжаться В ТОМ ЖЕ PR `#1798`.

Не создавать новый PR без объективной необходимости.

Ты уже работаешь над тем, чтобы помощник существовал на двух уровнях:

1. системный помощник организации;
2. отдельный помощник каждого проекта.

Эту работу необходимо довести до законченного состояния.

---

# 7. Два уровня помощника — обязательный контракт

Должны существовать независимые:

## Общесистемный помощник

Organization-scoped.

Имеет собственные:

- instructions;
- model;
- reasoning;
- RoleImage;
- tools;
- Environment;
- env;
- Secret bindings;
- MCP configuration;
- network policy;
- resources.

Не принадлежит фиктивному Project.

## Помощник проекта

Project-scoped.

Каждый Project может иметь собственного помощника со своими:

- instructions;
- model;
- reasoning;
- RoleImage;
- tools;
- Environment;
- env;
- Secrets;
- MCP;
- network;
- resources.

Project assistant не получает автоматически system Secrets/grants.

---

# 8. Самонастройка помощников

После реализации двух уровней первым реальным тестом должна стать настройка помощника самим помощником.

Не настраивай всё вручную через API, если соответствующий scenario должен поддерживаться помощником.

Нужный bootstrap flow:

```text
Host agent
   ↓
System Assistant
   ↓
настройка System Assistant самого себя
   ↓
Host verification
   ↓
System Assistant
   ↓
создание Project Kodex | Dev
   ↓
System Assistant
   ↓
создание Project Assistant Kodex | Dev
   ↓
Host verification
   ↓
Project Assistant Kodex | Dev
   ↓
настройка Project / Agents / Workflow
```

---

# 9. Сначала общесистемный помощник должен настроить СЕБЯ

После появления необходимых возможностей:

Открой общесистемного помощника через штатный UI.

Попроси его подготовить собственную конфигурацию.

Он должен штатным Configuration Plan настроить себе:

- подходящий RoleImage;
- инструменты;
- Context7 MCP;
- Context7 через protected Secret binding доверенному MCP adapter/server;
- Internet access;
- Web Search;
- Runtime Environment;
- resources;
- workspace;
- instructions.

Не должен просто объяснить, как это сделать.

Он должен подготовить реально применимый typed plan.

Ты как owner подтверждаешь bootstrap plan.

---

# 10. Сеть системного помощника

System Assistant должен иметь:

- Context7;
- native Web Search;
- HTTPS Internet read access, достаточный для исследования документации;
- GitHub public repository read access.

Минимально:

```text
mcp.context7.com
github.com
api.github.com
raw.githubusercontent.com
codeload.github.com
objects.githubusercontent.com
release-assets.githubusercontent.com
registry.npmjs.org
proxy.golang.org
sum.golang.org
```

Добавляй hosts только по реальному trace.

Не использовать `*`.

---

# 11. Проверка общесистемного помощника

После публикации новой revision НЕ переходи сразу дальше.

Запусти несколько тестовых сообщений.

Минимум:

1. попросить проверить актуальную документацию библиотеки через Context7;
2. попросить исследовать публичный код Kodex/GitHub;
3. попросить сделать небольшой web research;
4. попросить определить текущий проектный контекст.

Проверить:

- Context7 MCP реально вызывается;
- secret доступен только MCP server;
- web работает;
- GitHub read работает;
- Console без ошибок;
- runtime Pod использует ожидаемый Environment revision;
- network policy соответствует ожидаемой;
- tools соответствуют Environment.

---

# 12. КРИТИЧЕСКАЯ проверка prompt materialization

На каждом тестовом запуске помощника проверяй не только результат.

Тебе нужно доказать, какой prompt реально получил Codex runtime.

Проверить materialized prompt безопасным штатным механизмом observability/debugging.

Не раскрывать secrets.

Сверить:

1. system instructions;
2. rendered template;
3. подставленные template variables;
4. integrations block;
5. tools;
6. Project/organization/agent identity;
7. runtime descriptors;
8. входной user message;
9. files/context;
10. model/reasoning.

В частности убедиться, что исходный текст сообщения пользователя действительно вошёл в нужный slot/input, а не потерялся.

Не считай ответ модели доказательством правильного prompt.

---

# 13. Если prompt помощника плохой — исправить

На каждом bootstrap этапе оценивай:

- правильно ли помощник понял роль;
- правильно ли сформировал Configuration Plan;
- использовал ли exact schemas;
- не пытался ли передавать secrets через prompt;
- использовал ли template variables;
- правильно ли назначил capabilities;
- правильно ли создаёт Agent instructions;
- использует ли динамический `.integrations.items` block;
- не копирует ли runtime-only variables в persistent prompt;
- не выдумывает ли недоступные переменные.

Если результат систематически неверный:

НЕ компенсируй это вручную.

Найди root cause в:

- system assistant prompt;
- schema;
- configuration catalog;
- template catalog;
- documentation;
- runtime materialization.

Исправь его В PR `#1798`.

После этого повтори scenario.

---

# 14. Template variables при создании Agents

Особое внимание уделить создаваемым помощниками Agent instructions.

Они должны использовать разработанный механизм Go-template variables.

Persistent Agent instructions должны использовать допустимые stable variables, например:

```text
{{ .organization.name }}
{{ .project.name }}
{{ .agent.name }}
```

и серверный dynamic integration block.

Не вставлять статически информацию, которую runtime должен materialize динамически.

Не использовать runtime-only variables в persistent template, если каталог их не разрешает.

Выполнить:

- validate;
- preview;
- publish;
- runtime materialization check.

---

# 15. System Assistant создаёт Project

После того как system assistant реально доказан:

попроси ЕГО создать:

`Kodex | Dev`

Назначение:

разработка платформы Kodex на репозитории:

`codex-k8s/kodex`

Project должен быть создан штатным Configuration Plan.

Host не создаёт Project напрямую, если assistant scenario работает.

---

# 16. Проверить создание Project

После выполнения system assistant plan:

authoritative readback должен подтвердить:

- Project name = `Kodex | Dev`;
- корректный Project ref;
- current revision/version;
- organization ownership;
- audit actor;
- assistant involvement;
- отсутствие лишних resources.

Chrome:

- Project виден;
- navigation работает;
- Console clean;
- Network clean.

---

# 17. System Assistant создаёт помощника проекта

Следующий запрос системному помощнику:

создать и полностью настроить Project Assistant проекта:

`Kodex | Dev`

Это НЕ обычный сотрудник.

Это project-level assistant.

Он должен получить собственные:

- instructions;
- Environment;
- RoleImage;
- tools;
- MCP configuration;
- Context7;
- Internet;
- Project-scoped secrets;
- network.

---

# 18. Project Assistant — назначение

Помощник `Kodex | Dev` должен быть координатором настройки Project.

Он должен уметь:

- читать Project;
- создавать сотрудников;
- создавать Workflow;
- создавать/редактировать Runtime Environment drafts;
- выбирать RoleImage;
- управлять capabilities;
- управлять integration grants через Configuration Plans;
- запускать тестовых Agents;
- анализировать Project resources.

Он НЕ должен иметь raw GitHub write token.

---

# 19. GitHub доступ Project Assistant

Project Assistant получает read-only доступ к `codex-k8s/kodex`.

Через штатный managed GitHub Integration после его создания либо, если integration ещё не создана на bootstrap шаге, через public repository read/network.

После GitHub connection ему выдать только read grants:

```text
github.repository.metadata.read
github.repository.content.list
github.repository.content.read
github.branch.list
github.branch.read
github.commit.list
github.commit.read
github.issue.list
github.issue.read
github.pull_request.list
github.pull_request.read
github.pull_request.file.list
github.pull_request.review.list
github.pull_request.review.read
github.check_run.list
github.check_run.read
github.actions.run.list
github.actions.run.read
github.actions.job.list
github.actions.job.read
```

Никаких WRITE capabilities.

---

# 20. Project Assistant + Context7

Project Assistant получает тот же управляемый профиль Context7, с собственной привязкой в проектной области. После настройки выполнить фактический Context7 smoke и проверить scope/revision/binding.

---

# 21. Проверить Project Assistant так же тщательно

Выполнить несколько тестовых запросов.

Например:

- проанализировать текущую архитектуру IntegrationGrant;
- найти Issue;
- через Context7 проверить актуальную библиотеку;
- предложить создание тестового Agent.

После каждого:

проверить materialized prompt.

Особенно:

```text
.organization.name
.project.name
.agent.name / assistant identity
.integrations.items
tools
MCP
files
task/input message
```

Убедиться, что project context не подменён system context.

---

# 22. Только после проверки Project Assistant настраивает Project

Дальнейшая настройка должна выполняться преимущественно через Project Assistant.

Он должен с твоим контролем создать:

1. общий RoleImage;
2. Runtime Environments;
3. Project Secrets;
4. GitHub connection/grants;
5. сотрудников;
6. их capabilities;
7. Workflow.

Host agent проверяет каждый plan и authoritative result.

---

# 23. GitHub Integration approval policy

До полноценного Project bootstrap платформа должна поддерживать configurable Human Gate policy для конкретного grant.

Существующий механизм:

```text
NONE
HUMAN_EACH_EFFECT
HUMAN_SCOPED
```

должен использоваться.

Не создавать конкурирующую модель.

IntegrationPackage задаёт:

- default;
- допустимые policies.

Grant выбирает одну из допустимых.

---

# 24. GitHub package modification

Shipped GitHub IntegrationPackage должен позволять для collaborative write operations выбирать policy.

Например:

```text
github.issue.comment.create
github.issue.comment.update
github.pull_request.create
github.pull_request.update
github.pull_request.review.create
```

должны допускать `NONE`, если это безопасно для данной операции.

Destructive:

```text
github.pull_request.merge
github.branch.delete
github.repository.content.delete
github.actions.run.cancel
```

не должны становиться autonomous.

---

# 25. Grant security invariant

Grant-selected policy:

- durable;
- versioned;
- audited;
- входит в runtime snapshot;
- проверяется CP;
- проверяется gateway;
- проверяется adapter;
- ограничена package allowed policies.

Нельзя просто убрать equality-check package policy.

Нужно проверять:

```text
selected policy ∈ allowed policies
```

и exact grant snapshot.

---

# 26. Project Assistant создаёт GitHub connection

GitHub connection:

```text
owner: codex-k8s
repository: kodex
```

Доступ подключается через protected credential form/API, никогда через conversation. Connection должен пройти authoritative test.

---

# 27. Project Assistant создаёт ИИ-команду

Создать:

1. `Project Manager`
2. `System Analyst & Architect`
3. `Developer`
4. `Documentation Reviewer`
5. `Security Reviewer`
6. `Lexical Guardian`

Каждый создаётся через typed Configuration Plan.

Host agent должен проверять каждый созданный Agent.

---

# 28. Platform capabilities Agents

Всем:

```text
platform.artifact.manage
```

Manager дополнительно:

```text
platform.run.delegate
platform.run.launch
```

Manager — root Workflow coordinator.

---

# 29. Один общий RoleImage

Создать:

`kodex-selfdev`

Использовать для всех шести Agents.

Разграничивать права через:

- Environment;
- Secrets;
- Integration grants;
- Agent capabilities;
- network;

а не разными базовыми images.

---

# 30. Git обязателен всем

В общем image обязательно:

```text
git
```

Он нужен Manager/Architect/Reviewers для:

- log;
- diff;
- blame;
- show;
- history;
- fetch;
- checkout;
- inspection.

Push credential получает только Developer.

---

# 31. Toolchain общего image

Проверить наличие:

```text
bash
curl
git
gh
jq
yq
ripgrep
make
just

Go
goimports
gofumpt
golangci-lint
staticcheck
goose
sqlc
buf
protoc
protoc-gen-go
protoc-gen-go-grpc
grpcurl
mockgen
oapi-codegen

Node
npm
pnpm
yarn
TypeScript
ESLint
Prettier
Vite
vue-tsc
Vitest
Playwright
Chromium
Playwright MCP
wscat

Codex CLI
```

Желательно дополнительно:

```text
shellcheck
hadolint
govulncheck
gitleaks
```

если корректно проходят admission.

---

# 32. Environment для Developer

Создать:

`selfdev-write`

Developer получает:

- общий image;
- writable workspace;
- Project Files/artifacts;
- Context7;
- Web Search;
- GitHub managed grants;
- Git push Secret.

Учётные данные для git push создать как Project Secret.

Bind только Developer.

---

# 33. Environment остальных Agents

Создать:

`selfdev-review`

Используют:

- Manager;
- Architect;
- Documentation Reviewer;
- Security Reviewer;
- Lexical Guardian.

Raw GitHub PAT отсутствует.

При этом:

- git доступен;
- public repository fetch доступен;
- Context7 доступен;
- Web Search доступен;
- Project Files доступны.

---

# 34. Project Files и runtime workspace

Не использовать общий mutable PVC.

Каждый runtime имеет свой execution-scoped workspace.

Долговечный handoff через:

- Project Files;
- Artifacts;
- VFS.

Минимальные artifacts:

```text
manager-plan.md
architecture-review.md
documentation-review.md
security-review.md
lexical-review.md
final-readiness.md
```

Developer source of truth:

Git branch.

---

# 35. Проверять prompt каждого Agent после создания

После создания каждого Agent не считать настройку законченной.

Сделать тестовый Run.

Проверить фактический materialized prompt.

В нём должны корректно оказаться:

- organization;
- project = `Kodex | Dev`;
- agent identity;
- instructions template;
- rendered variables;
- integrations;
- tools;
- files;
- workflow metadata, если запуск из Workflow;
- входная задача/message.

Не должно быть unresolved:

```text
{{ .project.name }}
{{ .agent.name }}
```

если они должны были materialize.

Не должно быть посторонних переменных.

---

# 36. Если Agent prompt некорректен

Определить источник:

- плохой template;
- неправильный assistant-generated instructions;
- configuration schema;
- materializer;
- variable catalog;
- workflow context;
- runtime snapshot.

Если Project Assistant создаёт сотрудников с плохими instructions систематически:

это defect Project Assistant prompt.

Исправить helper/system prompt в платформе, а не вручную исправлять все Agents.

После исправления повторить creation scenario.

---

# 37. GitHub grants Manager

READ:

```text
github.repository.metadata.read
github.issue.list
github.issue.read
github.issue.comment.list
github.issue.comment.read
github.pull_request.list
github.pull_request.read
github.pull_request.file.list
github.pull_request.review.list
github.pull_request.review.read
github.check_run.list
github.check_run.read
github.actions.run.list
github.actions.run.read
github.actions.job.list
github.actions.job.read
```

WRITE:

```text
github.issue.comment.create
```

Policy:

```text
NONE
```

---

# 38. Architect

READ:

```text
github.repository.metadata.read
github.repository.content.list
github.repository.content.read
github.branch.list
github.branch.read
github.commit.list
github.commit.read
github.issue.list
github.issue.read
github.pull_request.list
github.pull_request.read
github.pull_request.file.list
github.pull_request.review.list
github.pull_request.review.read
github.check_run.list
github.check_run.read
```

При необходимости:

```text
github.issue.comment.create
```

Policy:

`NONE`.

---

# 39. Developer

READ:

```text
github.repository.metadata.read
github.repository.content.list
github.repository.content.read
github.branch.list
github.branch.read
github.commit.list
github.commit.read
github.issue.read
github.issue.comment.list
github.issue.comment.read
github.pull_request.list
github.pull_request.read
github.pull_request.file.list
github.pull_request.review.list
github.pull_request.review.read
github.check_run.list
github.check_run.read
github.actions.run.list
github.actions.run.read
github.actions.job.list
github.actions.job.read
```

WRITE:

```text
github.pull_request.create
github.pull_request.update
github.issue.comment.create
```

Policy:

```text
NONE
```

Developer должен без Human Gate:

- создать PR;
- обновлять PR;
- отвечать reviewers.

---

# 40. Reviewers

Каждый Reviewer получает необходимые repository/PR READ grants.

WRITE:

```text
github.pull_request.review.create
github.issue.comment.create
```

Policy:

```text
NONE
```

Это необходимо для автономного review cycle.

---

# 41. GitHub identity

Все Agents работают GitHub identity:

`kodex-agent`.

Поэтому Reviewer не должен пытаться создать независимый GitHub APPROVE собственного PR той же identity.

Использовать COMMENT review.

Internal verdict хранить отдельно:

```text
PASS
CHANGES_REQUESTED
BLOCKED
```

---

# 42. Workflow

Создать:

`SOFTWARE_CHANGE`

Структура:

```text
INTAKE
  Manager

ANALYSIS_AND_ARCHITECTURE
  Architect

IMPLEMENTATION
  Developer

parallel:
  DOCUMENTATION_REVIEW
  SECURITY_REVIEW
  LEXICAL_REVIEW

REVIEW_AGGREGATION
  Manager

если findings:
  DEVELOPER_FIX
  Developer

потом repeat affected reviews

FINAL_MANAGER_REVIEW
  Manager

READY_FOR_HUMAN_REVIEW
```

Manager реально использует delegation.

---

# 43. Проверить delegation до настоящего Workflow

Сделать небольшой disposable workflow/run.

Manager должен самостоятельно:

- delegate Architect;
- получить результат;
- delegate Developer или reviewer;
- получить handoff.

Проверить runtime graph.

Не считать capability `platform.run.delegate` достаточным доказательством.

---

# 44. Merge #1798 только после bootstrap acceptance

До merge должно быть доказано:

```text
System Assistant self-configuration PASS

System Assistant Context7 PASS
System Assistant web PASS
System Assistant prompt materialization PASS

Project Kodex | Dev creation PASS

Project Assistant creation PASS
Project Assistant Context7 PASS
Project Assistant repository read PASS
Project Assistant prompt materialization PASS

Project Assistant creates Agent correctly PASS
Agent template variables PASS
Agent runtime prompt rendering PASS

configurable GitHub ApprovalPolicy PASS
GitHub NONE writes PASS
HUMAN_EACH_EFFECT PASS
HUMAN_SCOPED PASS

Manager delegation PASS
Artifacts/files PASS

Developer git push PASS
Developer autonomous PR create PASS
Reviewer autonomous comment/review PASS
Developer autonomous response PASS
```

Если это требует ещё кода в PR #1798 — доделать.

---

# 45. Tests PR #1798

Перед merge:

- Go unit;
- component PostgreSQL;
- contract;
- Proto;
- OpenAPI;
- codegen;
- SQL boundary;
- frontend unit;
- lint;
- typecheck;
- build;
- relevant Playwright;
- RoleImage/admission;
- integration package tests;
- Context7/MCP materialization tests;
- prompt/template tests;
- Chrome manual acceptance.

Обновить PR body фактическим PASS/FAIL/NOT RUN.

---

# 46. Merge bootstrap PR

Когда bootstrap acceptance выполнена, слить текущий bootstrap PR #1798 в пределах согласованных полномочий. Не обходить обязательные checks и branch protection.

После merge получить GitHub readback: merged, merge SHA и new main SHA. Итоговый dogfooding PR не сливать и не одобрять от имени владельца.

---

# 47. Перейти на fresh main

```bash
git fetch --all --prune
git checkout main
git pull --ff-only
```

Проверить:

```text
local HEAD == origin/main == GitHub default branch HEAD
```

Зафиксировать:

`MAIN_SHA`.

---

# 48. Развернуть fresh main

Тестовый кластер должен быть обновлён именно на свежий `main`.

Не использовать старую PR branch.

Проверить:

- source SHA;
- images;
- migrations;
- Pods;
- controllers;
- runtime;
- integration gateway;
- egress gateway;
- control plane;
- UI.

Chrome hard reload.

Console/Network/WebSocket clean.

---

# 49. После merge перепроверить созданную конфигурацию

Не считать, что bootstrap resources автоматически корректны после deployment main.

Проверить:

- System Assistant;
- Project `Kodex | Dev`;
- Project Assistant;
- Agents;
- environments;
- secrets metadata;
- Context7;
- GitHub connection;
- grants;
- Workflow.

Если revisions нужно republish — выполнить штатно.

---

# 50. Выбор первой Issue

После обновления main запросить актуальные open Issues.

На момент подготовки задания приоритетная:

`#1796 — Показывать для подписочных аккаунтов юзадж и кол-во кредитов если это поддерживается codex`

Предпочтительно взять именно её.

Но Manager сначала проверяет её актуальность.

Если она:

- уже реализована;
- закрыта;
- полностью блокируется отсутствующим upstream API;
- не требует code change;

выбрать самую новую подходящую Issue.

---

# 51. Issue #1796

Если используется #1796, обязательно исследовать:

- есть ли supported Codex API/app-server usage data;
- subscription limits;
- credits;
- reset time;
- несколько accounts;
- unavailable data semantics.

Context7 first.

Затем официальный OpenAI upstream при необходимости.

Нельзя:

- scrape web UI;
- использовать undocumented private endpoint без решения;
- выдумывать credits API;
- показывать фиктивные данные.

---

# 52. Полный dogfooding Workflow

После запуска настоящей Issue ты НЕ выполняешь её вместо команды.

Manager должен реально запустить Workflow.

Доказать цепочку:

```text
Manager
→ Architect
→ Developer
→ PR
→ three reviewers
→ findings
→ Developer fixes
→ responses
→ repeat reviews
→ Manager
→ READY_FOR_HUMAN_REVIEW
```

---

# 53. Host verification во время Workflow

Продолжай проверять каждый значимый transition.

Особенно:

- Manager delegation;
- workflow graph;
- exact target Agent;
- exact commit SHA;
- prompt materialization;
- files/artifacts;
- integration grants;
- GitHub calls;
- absence/presence Human Gate;
- secret isolation;
- realtime state.

---

# 54. Prompt verification в Workflow

Для первого полного Workflow проверить materialized prompt КАЖДОЙ роли минимум один раз.

Manager:

- Issue context;
- Project;
- workflow stage;
- available Agents;
- integrations.

Architect:

- Manager handoff;
- Project;
- Issue;
- files.

Developer:

- architecture handoff;
- Issue;
- branch/task;
- integrations;
- files.

Reviewer:

- exact PR/commit;
- reviewer-specific instructions;
- relevant artifacts.

Fix Developer:

- review findings;
- current commit;
- input task;
- same Session/continuation semantics, если применимо.

---

# 55. User input body

Обязательно убедиться, что message/task input реально попадает в prompt.

Проверить не только metadata.

Для test messages используй узнаваемые harmless marker strings.

Например:

```text
PROMPT_INPUT_VERIFY_01
```

После materialization доказать наличие marker в правильном user/task slot.

Не оставлять marker в production files/PR.

---

# 56. Проверка переменных

Создай тестовый prompt, который использует разрешённые variables.

Проверить exact rendered values:

```text
organization
project
agent
integrations
workflow
step
input/files
```

только там, где они доступны по context catalog.

Если недоступная переменная проходит validate или доступная не materialize'ится — platform defect.

---

# 57. Review cycle

Reviewers работают на exact commit.

Каждый artifact содержит commit SHA.

Если Developer push'ит новый commit:

Manager должен определить необходимость re-review.

При существенных изменениях повторить все три.

---

# 58. Developer responses

Developer должен:

1. получить GitHub comments;
2. получить review artifacts;
3. исправить код;
4. tests;
5. commit;
6. push;
7. ответить reviewers;
8. передать new SHA.

Без моего Human Gate.

---

# 59. Final PR

Когда все required internal reviews PASS:

Manager формирует:

`final-readiness.md`.

В нём:

- Issue;
- acceptance criteria;
- final SHA;
- reviews;
- tests;
- PASS;
- FAIL;
- NOT RUN;
- known limitations.

Manager переводит Workflow в:

`READY_FOR_HUMAN_REVIEW`.

---

# 60. Финальный PR НЕ MERGE

После READY остановиться.

Не merge.

Не auto-merge.

Не одобрять от моего имени.

Я должен сам посмотреть итоговый PR.

---

# 61. Evidence

Использовать только:

`PASS`

проверено фактически.

`FAIL`

запуск был и не прошёл.

`NOT RUN`

не запускалось.

`BLOCKED`

объективно невозможно.

Не заменять живой proof:

- unit test;
- screenshot;
- green Pod;
- отсутствие exception;
- static code inspection.

---

# 62. Если помощник делает неправильные действия

Очень важное правило.

На bootstrap этапе помощники сами являются объектом тестирования.

Если помощник:

- неправильно создаёт Agent;
- выбирает неправильную capability;
- не использует template variables;
- создаёт плохой prompt;
- не добавляет integrations;
- забывает Context7;
- путает system/project scope;
- пишет secrets в prompt;
- неправильно формирует Environment;
- неправильно назначает network;

не исправляй результат молча вручную.

Определи, почему помощник это сделал.

При необходимости изменить:

- assistant core prompt;
- project assistant prompt;
- operation schema;
- configuration catalog;
- documentation exposed to assistant;
- template variable catalog;
- server-side validation.

После исправления повторить тот же scenario.

---

# 63. Критерий качества помощников

К концу bootstrap должен быть доказан не только технический API.

Общесистемный помощник должен быть способен корректно:

- настроить самого себя;
- создать Project;
- создать Project Assistant.

Project Assistant должен быть способен корректно:

- создать Agent;
- создать Environment;
- выбрать RoleImage;
- настроить integration;
- назначить grants;
- задать Agent capabilities;
- создать Workflow.

И делать это через корректные typed plans.

---

# 64. Финальный отчёт

В конце подготовить отчёт.

## Platform

```text
PR #1798 original SHA
Final #1798 SHA
Merge SHA
main SHA
deployed SHA
```

## System Assistant

```text
Environment
RoleImage
Context7
network
tools
prompt rendering
self-configuration status
```

## Project

```text
Kodex | Dev
Project ref
```

## Project Assistant

```text
Environment
RoleImage
Context7
GitHub read access
network
prompt rendering
```

## Team

Для каждого Agent:

```text
name
capabilities
environment
GitHub grants
ApprovalPolicies
Context7 status
prompt rendering status
```

## Workflow

```text
SOFTWARE_CHANGE
stages
delegation verification
```

## Dogfooding

```text
Issue
branch
commits
PR
review cycles
final SHA
status
```

## Defects discovered

Для каждого:

```text
Problem
Root cause
Fix
Verification
```

## Итог

Одно значение:

```text
READY_FOR_HUMAN_REVIEW
BLOCKED
FAILED
```

Если READY — дать мне номер/URL PR.

Не merge.

---

# 65. Главный критерий эксперимента

Успех — это не просто автоматически созданный Pull Request.

Нужно доказать три уровня self-hosting:

```text
Kodex System Assistant
настраивает себя
        ↓
Kodex System Assistant
создаёт и настраивает Project Assistant
        ↓
Kodex Project Assistant
создаёт и настраивает AI-команду
        ↓
AI-команда
разрабатывает сам Kodex
```

При этом host agent непрерывно доказывает:

- правильность prompts;
- правильность template rendering;
- правильность variables;
- передачу user/task input;
- доступность Context7;
- доступность tools;
- правильность grants;
- секретную изоляцию;
- network isolation;
- delegation;
- artifact handoff;
- runtime revisions;
- GitHub effects;
- Human Gate semantics.

Если один из этих уровней требует ручного обхода ограничений платформы, сначала исправь платформу, а затем повтори соответствующий уровень проверки.

Именно способность пройти эту цепочку без скрытых ручных подстановок является основной целью dogfooding.
