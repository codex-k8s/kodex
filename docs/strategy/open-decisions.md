# Open Decisions

Перед кодовой реализацией нужно выбрать эти решения. Приоритет - не заблокировать быстрый MVP и не сломать будущую совместимость с `kodex`.

## 1. Модель изоляции agent run

Есть противоречие:

- пользовательская установка для этого проекта: каждый агент запускается в своем pod в namespace Mattermost и имеет PVC;
- каноника `kodex`: slot первой версии - отдельный namespace задачи.

### Вариант A: один namespace `mattermost`

Agent pod и PVC создаются в одном namespace `mattermost`.

Плюсы:

- быстрее реализовать;
- проще bootstrap и диагностика;
- соответствует текущему запросу;
- меньше RBAC и cleanup-сложности.

Минусы:

- слабее изоляция между run;
- больше риск случайного доступа к соседним PVC/Secrets при ошибке RBAC;
- дальше придется мигрировать к namespace-per-run.

### Вариант B: namespace-per-run

Mattermost и bot-service живут в `mattermost`, каждый agent run получает отдельный namespace.

Плюсы:

- ближе к `kodex`;
- чище cleanup;
- лучше security boundary;
- проще будущий runtime-manager extraction.

Минусы:

- дольше первый rollout;
- сложнее RBAC;
- больше Kubernetes-объектов и edge cases.

### Рекомендация

Для самого короткого MVP выбрать вариант A, но:

- создавать отдельный ServiceAccount на run или role;
- использовать label-based ownership;
- запрещать mount чужих PVC;
- не давать agent pod права читать Kubernetes API;
- оставить runtime interface так, чтобы перейти на namespace-per-run без изменения orchestrator.

## 2. Mattermost install path

### Вариант A: official Helm/Operator

Ближе к официальной документации, но тяжелее для быстрого dogfooding.

### Вариант B: custom manifests

Быстрее и прозрачнее для одного сервера, но требует собственного upgrade path.

### Рекомендация

Вариант B для MVP. Зафиксировать переходный статус и не обещать HA до отдельного PR.

## 3. Bot-service или Mattermost plugin

### Вариант A: external bot-service

Slash commands, REST API, interactive actions.

### Вариант B: server plugin

Глубже интеграция, но тяжелее rollout и совместимость.

### Рекомендация

Вариант A для MVP. Plugin рассматривать после стабильного workflow.

## 4. GitHub PAT или GitHub App

### Вариант A: bot PAT

Быстрее старт, уже есть env-ключи.

### Вариант B: GitHub App

Правильнее для production permissions и installations.

### Рекомендация

PAT для MVP, но abstractions назвать provider account/credential, не `pat` в доменной модели.

## 5. Prompt templates в БД или Git

### Вариант A: БД

Соответствует `kodex` target: flow, role и prompt templates канонически живут в БД.

### Вариант B: Git fixtures

Проще ревьюить prompt через PR, но хуже runtime-editing.

### Рекомендация

БД как источник правды, Git fixtures только для seed/default templates.

## 6. Один сервис или микросервисы

### Вариант A: modular monolith

Один deployable service с явными модулями.

### Вариант B: сразу owner-services

Архитектурно чище, но не укладывается в быстрый MVP.

### Рекомендация

Modular monolith для MVP. Доменные модули держать совместимыми с будущим разделением.
