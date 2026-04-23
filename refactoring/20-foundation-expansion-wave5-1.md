---
doc_id: REF-CK8S-0020
type: platform-foundation
title: "kodex — расширение платформенного основания в wave 5.1"
status: active
owner_role: SA
created_at: 2026-04-23
updated_at: 2026-04-23
related_issues: [281, 282, 309, 470, 488, 586]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-23-refactoring-wave5-1"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-23
---

# Расширение платформенного основания в wave 5.1

## TL;DR
- Wave 5.1 добавляет в канонику платформы шесть обязательных доменных направлений до старта runtime/deploy волны: package-платформу, multi-org access model, fleet management, billing, release policy и automation rules.
- Новая модель проектируется сразу на enterprise-уровень, но для каждого направления фиксируется отдельный MVP floor, чтобы implementation waves не расползлись.
- Плагины и руководящие пакеты документации рассматриваются как разные виды одного package-контракта: первые исполняются в Kubernetes, вторые импортируются в локальный рабочий контур агентов.
- Один сервер и один кластер остаются стартовой operational-конфигурацией, но документация и кодовая архитектура больше не должны предполагать, что такая топология единственно возможна.
- С этого момента wave 6 и последующие implementation waves обязаны учитывать tenancy, package catalogs, billing, release trains и trigger-driven automation как first-class seams, а не как "расширения когда-нибудь потом".

## 1. Почему понадобилась отдельная wave 5.1
После завершения wave 5 стало понятно, что часть будущих требований относится не к конкретному экрану и не к runtime-деталям, а к самому основанию платформы:
- кто считается субъектом платформы и как делятся права;
- что такое расширение платформы и как оно подключается;
- как платформа работает не только для owner-организации, но и для клиентов, исполнителей и SaaS-арендаторов;
- как учитывать затраты и коммерческий контур;
- как планировать release trains и не терять automation rules;
- как разнести руководящую документацию по внешним репозиториям, не ломая локальную работу агентов.

Если не заложить эти seams сейчас, wave 6 и первые implementation waves зацементируют слишком узкую односерверную и одноорганизационную модель.

## 2. Новые обязательные направления wave 5.1

### 2.1. Package-платформа
Платформа должна поддерживать единый package-контракт с несколькими видами пакетов:
- `plugin package` — исполняемое расширение платформы;
- `guidance package` — пакет руководящей документации и правил для агентов;
- в будущем допускаются и другие package kinds, если они подчиняются тому же общему контракту.

Package-контракт нужен, чтобы:
- не проектировать каталог плагинов и каталог руководящей документации как две несвязанные системы;
- одинаково описывать источник, версию, верификацию, лицензию, коммерческую модель и локализацию;
- разделять package metadata и package runtime/import contract.

### 2.2. Multi-org access model
Платформа должна поддерживать:
- owner-организацию;
- клиентские организации;
- организации внешних исполнителей;
- будущую SaaS-модель с изоляцией по организациям.

Пользователь:
- может состоять в нескольких организациях;
- может состоять в нескольких группах;
- может получать права напрямую и через membership-граф.

### 2.3. Fleet management
Runtime-платформа больше не считается привязанной навсегда к одному серверу и одному кластеру.

Нужно сразу заложить:
- каталог серверов;
- каталог Kubernetes-кластеров;
- правила привязки организации, проекта, репозитория или workload к конкретному server/cluster scope;
- задел на multi-zone и delegated clusters.

### 2.4. Billing и cost accounting
Платформа должна уметь:
- учитывать свои расходы по compute, storage, runtime, package usage и внешним провайдерам;
- раскладывать их по организациям, проектам и при необходимости по репозиториям и release линиям;
- формировать основу для выставления счетов;
- в будущем поддержать рублёвый и валютный контур оплаты.

### 2.5. Release policy и release trains
Платформа должна различать:
- provider-native ветки и теги;
- platform-owned release policy;
- platform-owned release line или release train;
- rollout policy: прямой релиз, staged rollout, canary, rollback, hold, recovery.

### 2.6. Automation rules
Flow больше не может считаться только вручную запускаемым.

Нужны:
- расписания;
- trigger bindings;
- входящие автоматические сигналы;
- правила, создающие `Issue` или запускающие flow по cron и событиям.

## 3. Канонический package-подход

### 3.1. Единый package catalog
Вместо отдельного "каталога плагинов" и отдельного "каталога руководящей документации" платформа проектируется вокруг единого package catalog.

Каждая запись каталога описывает:
- `package kind`;
- источник пакета: встроенный, каталог платформы, внешний каталог, пользовательский источник;
- repository provider и ссылку на репозиторий;
- версию;
- verification status;
- коммерческий статус;
- локализацию названий и описаний;
- право установки и область применения.

### 3.2. `Plugin package`
`Plugin package` — это пакет, который:
- имеет package manifest в корне репозитория;
- описывает требуемые platform APIs, MCP surface, permissions и runtime-потребности;
- описывает обязательные и желательные secret inputs с локализованными названиями и описаниями;
- поставляет Dockerfile, Kubernetes manifests и другие runtime-зависимости;
- может быть реализован на любом языке и на любом стеке, если соблюдает package contract платформы.

### 3.3. `Guidance package`
`Guidance package` — это пакет, который:
- живёт во внешнем репозитории;
- поставляет руководящую документацию, шаблоны и правила;
- импортируется в локальный рабочий контур проекта, репозитория, сервиса или самой платформы;
- не требует vector storage и не заменяет локальное чтение файлов агентом;
- допускает self-improve задачи, которые запускаются против самого guidance-repository.

### 3.4. Почему не две разные системы
Разделение на два отдельных каталога дало бы ложное удвоение сущностей:
- два формата source metadata;
- две модели версий и верификации;
- две системы лицензий и платности;
- две несовместимые install/import истории.

Wave 5.1 фиксирует общий package layer с разными runtime-путями для разных видов пакетов.

## 4. MVP floor против enterprise target

| Направление | MVP floor | Enterprise target |
|---|---|---|
| Package platform | встроенные пакеты, пользовательские package sources, локальная установка плагина и guidance package без открытого marketplace billing | публичный marketplace, paid/free packages, publisher profiles, verified versions, revenue share |
| Organizations и groups | owner-организация + клиентские организации, глобальные и организационные группы, базовая иерархия прав | SaaS tenancy, delegated administration, сложные inheritance и exception policies |
| Fleet | один сервер и один cluster в production, но с явным inventory-слоем и placement policy | несколько серверов, несколько кластеров, multi-zone, dedicated cluster per org/project |
| Billing | сбор cost records и project/org allocation, ручное формирование billing summary | invoices, payment providers, commissions, тарификация packages и SaaS |
| Release policy | release branch rules, release line, canary/staged rollout policies на уровне модели | полные release programs, multi-env trains, tenant-aware rollout policies |
| Automation rules | cron и минимальный набор event triggers, включая alert-driven task creation | гибкие event buses, сложные trigger pipelines, cross-org automation templates |
| Guidance packages | импорт из отдельных repos в локальный контур и использование агентом по локальным файлам | marketplace guidance packages, policy bundles, managed updates и paid content |

## 5. Что это меняет для уже принятых волн

### Wave 0
Нужно закрепить:
- package catalog как постоянный домен программы;
- doc-governance для внешних guidance repositories;
- sequencing, где enterprise target описывается сразу, а implementation floors делаются поэтапно.

### Wave 1
Нужно добавить в продуктовую модель:
- package entries;
- plugin packages;
- guidance packages;
- organizations, groups и membership graph;
- server/cluster inventory;
- billing accounts и cost records;
- release policy и release line;
- schedule rules и trigger bindings.

### Wave 2
Нужно добавить архитектурные seams:
- `package-hub`;
- `fleet-manager`;
- `billing-hub`;
- расширение `access-manager` до tenancy-aware модели;
- расширение `project-catalog` до branch/release и package binding policy.

### Wave 3
Нужно закрепить:
- owner-сервисы для package metadata, install/import state и secret schema;
- data ownership для org/group/fleet/billing/automation objects;
- provider-модель для package source repositories, payment providers и branch semantics.

### Wave 4
Нужно закрепить:
- risk model для plugins, paid packages, release trains и unattended automation;
- политику human gate для branch/release policy и high-risk triggers;
- безопасный контур автоматического запуска flow и background automation.

### Wave 5
Нужно расширить UX-модель:
- package catalogs и package installs;
- organization/group management;
- fleet screens;
- billing and usage views;
- release policy и automation settings.

## 6. Что не должно потеряться дальше
После wave 5.1 любая следующая реализация обязана считать обязательными следующие seams:
1. package catalog и package verification;
2. user groups и organizations;
3. server/cluster inventory и placement policy;
4. billing и cost allocation;
5. release line, release policy и branch rules;
6. schedule rules и trigger bindings;
7. guidance packages как отдельный repository-driven слой.

Если какой-то из этих seams временно не реализуется в коде, он всё равно должен сохраняться:
- в канонической документации;
- в naming и data ownership;
- в API/DB design без блокирующих тупиков;
- в UI information architecture как минимум на уровне допустимых экранов и разделов.
