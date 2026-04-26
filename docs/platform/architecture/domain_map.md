---
doc_id: ARC-CK8S-DOMAIN-MAP-0001
type: architecture-domain-map
title: kodex — архитектурная карта доменов
status: active
owner_role: SA
created_at: 2026-04-26
updated_at: 2026-04-26
related_issues: [599, 600, 601, 602]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-26-platform-architecture-frame"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-26
---

# Архитектурная карта доменов

## TL;DR

Платформа делится на домены по владению бизнес-смыслом и состоянием. Каждый домен имеет owner-сервис или явно отмечен как read-проекционный контур. Сервис-владелец отвечает за правила, каноническое состояние, события и публичные контракты своего домена.

## Принципы деления

- Один домен имеет один центр владения.
- Provider-native сущности остаются у провайдера, а платформа хранит проекции и платформенные метаданные.
- Runtime-состояние не смешивается с агентной оркестрацией.
- UI не становится владельцем данных и работает через edge и read-проекции.
- Каталоги пакетов, руководящие пакеты, плагины и магазины пакетов относятся к одному пакетному контуру.
- Биллинг, релизы, риск, доступы и серверы проектируются как обязательные контуры, даже если реализуются поэтапно.

## Доменная карта

| Домен | Owner-сервис | Что владеет |
|---|---|---|
| Доступ и аккаунты | `access-manager` | Пользователи, организации, группы, членство, allowlist, права, внешние аккаунты входа и административный аудит. |
| Проекты и репозитории | `project-catalog` | Проекты, репозитории, project policy, `services.yaml`, источники проектной документации, branch rules, release policy, placement policy. |
| Рабочие сущности провайдера | `provider-hub` | Зеркальные проекции `Issue`, `PR/MR`, комментариев, mentions, relationships, provider accounts, webhooks, reconciliation и лимиты. |
| Пакетная платформа | `package-hub` | Каталог пакетов, установленные пакеты, доступные пакеты, источники магазинов, plugin package, guidance package, versions и verification. |
| Оркестрация агентов | `agent-manager` | Flow, stage, role, stage role binding, prompt templates, sessions, runs, automation rules, acceptance machine. |
| Runtime и контур серверов | `fleet-manager`, `runtime-manager` | Серверы, Kubernetes-кластеры, placement, slots, jobs, prewarm, cleanup, build/deploy/mirror. |
| Центр взаимодействий | `interaction-hub` | Dialog threads, approvals, notifications, subscriptions, delivery attempts, external channel callbacks. |
| Операционная консоль | `operations-hub` | Read-проекции, timelines, очереди, блокировки, агрегированные статусы и операторские срезы. |
| Биллинг и учёт затрат | `billing-hub` | Billing accounts, cost records, распределение затрат, invoice basis, экономика пакетов и SaaS-контур. |
| Риск и релизы | `agent-manager`, `project-catalog`, `runtime-manager`, `interaction-hub` | Risk gates, release lines, release decisions, deployment gates и Human gate. Владение разделено по типу состояния. |
| Жизненный цикл знаний | `project-catalog`, `package-hub`, `agent-manager` | Источники проектной документации, guidance packages, шаблоны документов, seed fixtures и self-improve контуры. |

## Владение в разделённых доменах

Некоторые бизнес-процессы проходят через несколько owner-сервисов. В таких случаях владельцем считается сервис, который принимает конкретное каноническое решение.

| Процесс | Кто владеет состоянием |
|---|---|
| Запуск агентной работы | `agent-manager` владеет run/session, `runtime-manager` владеет slot/job, `provider-hub` владеет provider mirror. |
| Релиз | `project-catalog` владеет release policy, `agent-manager` владеет flow execution, `runtime-manager` владеет deploy job, `interaction-hub` владеет запросами решений. |
| Установка пакета | `package-hub` владеет package installation, `runtime-manager` запускает plugin workload, `fleet-manager` выбирает инфраструктурный контур. |
| Доступ к проекту | `access-manager` вычисляет права, `project-catalog` владеет проектом и репозиторием, `provider-hub` знает внешний аккаунт. |
| Self-improve руководства | `package-hub` знает пакет, `agent-manager` запускает роль, `provider-hub` отражает PR в репозитории пакета. |

## Доменные события

Сервис-владелец публикует события при изменении важных состояний. Подписчики не получают право менять чужую истину, а строят свои проекции или запускают свою бизнес-логику.

Минимальные группы событий:
- `access.*`: организация, пользователь, группа, членство, права, внешний аккаунт;
- `project.*`: проект, репозиторий, `services.yaml`, release policy, placement policy;
- `provider.*`: provider artifact synced, drift detected, limit changed, reauth required;
- `package.*`: package discovered, installed, updated, revoked, verification changed;
- `agent.*`: run requested, run state changed, acceptance result, follow-up required;
- `runtime.*`: slot state changed, job state changed, cleanup failed, cluster health changed;
- `interaction.*`: approval requested, notification delivered, callback received;
- `billing.*`: cost record created, allocation changed, invoice draft updated.

## Что не считается доменом-владельцем

- `api-gateway` — edge, а не домен.
- `web-console` — интерфейс, а не домен.
- `platform-mcp-server` — инструментальная поверхность и policy-шов, а не владелец business state.
- `worker` — исполнитель фоновой работы, а не домен.
- `agent-runner` — исполнитель агентной сессии, а не домен.

## Апрув

- request_id: `owner-2026-04-26-platform-architecture-frame`
- Решение: approved
- Комментарий: доменная карта входит в сквозной архитектурный каркас платформы.
