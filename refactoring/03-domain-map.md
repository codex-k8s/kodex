---
doc_id: REF-CK8S-0003
type: domain-map
title: "kodex — доменная карта новой платформы"
status: active
owner_role: SA
created_at: 2026-04-21
updated_at: 2026-04-23
related_issues: [470, 488]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-21-refactoring-wave0"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-21
---

# Доменная карта новой платформы

## Принцип
Платформа должна строиться не вокруг внутренних "магических" рабочих сущностей, а вокруг сущностей, которыми реально управляют GitHub/GitLab и сам runtime платформы.

## Домены первой итерации

### 1. Доступ, организации, группы и внешние аккаунты
Что входит:
- пользователи платформы;
- организации;
- глобальные и организационные группы;
- права доступа;
- membership и inheritance политики;
- проектные и репозиторные разрешения;
- аккаунты GitHub/GitLab;
- аккаунты Codex/OpenAI и других model/runtime providers;
- системные настройки.

### 2. Проекты, репозитории и release policy
Что входит:
- проекты;
- репозитории внутри проекта;
- project/repository configuration;
- `services.yaml` policy;
- onboarding новых репозиториев;
- привязка документации и базовых project rules;
- branch rules;
- release policies;
- release lines и rollout strategy bindings.

### 3. Provider-native рабочие сущности
Что входит:
- `Issue`;
- `PR/MR`;
- комментарии и mentions;
- relationships;
- type / labels / milestones / project fields / provider metadata;
- platform watermarks и открытые инструкции в body/comment слоях.

Ключевое решение:
- инициатива = `Issue` типа `initiative`;
- follow-up работа = такие же `Issue` с другими типами;
- платформа не плодит отдельную собственную рабочую сущность для инициативы.

### 4. Package-платформа: плагины и guidance packages
Что входит:
- `plugin package`;
- `guidance package`;
- package catalog;
- package source repositories;
- verification status;
- package install/import state;
- package pricing и marketplace metadata;
- secret schemas и package capability declarations.

Ключевое решение:
- плагины и руководящие пакеты документации не проектируются как две разные несвязанные системы;
- они сходятся в общем package-контракте с разными runtime/import путями.

### 5. Агент-менеджер и оркестрация работы
Что входит:
- agent-manager как центральный управляющий агент;
- разбор пользовательских запросов;
- запуск role-агентов;
- управление flow;
- schedule rules;
- trigger bindings;
- продолжение сессий;
- acceptance machine;
- правила создания follow-up задач и артефактов.

### 6. Runtime-платформа, fleet и слоты
Что входит:
- inventory серверов и Kubernetes-кластеров;
- placement policy;
- `code-only` и `full-env` execution;
- namespace-per-task slot;
- prewarmed slots;
- platform jobs для mirror/build/deploy;
- регламентные cleanup и health-check jobs;
- очистка и переинициализация слота;
- future seam для nested cluster;
- подготовка окружения, миграции, фикстуры, runtime reuse.

### 7. Контур пользовательских взаимодействий и внешних каналов
Что входит:
- взаимодействие через UI фронта;
- голосовой интерфейс;
- внешние каналы уведомлений и запросов;
- настраиваемые уведомления по run/job/error/action событиям;
- callback/resolution contract;
- подключаемые внешние интеграции.

Ключевое решение:
- список каналов не фиксируется заранее;
- проектируется общий расширяемый контракт;
- механизм реализации (плагины, адаптеры, OpenAPI-контракт, гибрид) выбирается отдельным design-срезом.

### 8. Консоль и операционные интерфейсы
Что входит:
- central chat с agent-manager;
- управление проектами, репозиториями, доступами;
- рабочие представления по задачам;
- операционные статусы и диагностика;
- UX для запуска flow и наблюдения за выполнением;
- экраны package catalogs, fleet, billing и automation settings.

### 9. Billing и cost accounting
Что входит:
- cost records;
- allocation по организациям, проектам и другим scopes;
- usage внешних провайдеров и runtime;
- invoice basis;
- payment-provider seams;
- package marketplace economics.

### 10. Risk/release governance
Что входит:
- классификация риска;
- матрица review gates;
- release decision package;
- правила human approval;
- правила, когда owner утверждает документы, high-risk переходы и релизы, а не обязательно код построчно;
- unattended automation governance;
- release branches, release lines и rollout policies.

Это не optional домен. Он должен закладываться с самого начала, даже если полная реализация будет позже.

### 11. Документация и knowledge lifecycle
Что входит:
- шаблоны документов;
- project rules для агентов;
- guidance packages;
- индексы и структура проектной документации;
- будущий переход к knowledge storage через векторные БД и MCP.

## Cross-cutting требования
- Provider-first model.
- Minimal internal truth beyond provider and runtime state.
- Risk-based approvals.
- Compact PR discipline.
- Русскоязычная документация без лишнего смешивания с английским.

## Что будет спроектировано позже
- Точная сервисная карта и количество внутренних сервисов.
- Точный контракт внешних interaction channels.
- Точная модель risk classes и release gates.
- Точный frontend information architecture.
