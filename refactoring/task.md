# Канонический мандат рефакторинга `kodex`

Этот документ — главный источник правды для программы рефакторинга. Он описывает не одну локальную волну, а целевую платформу целиком, включая enterprise seams, которые должны быть заложены уже в первой версии и не должны потеряться при дальнейшей декомпозиции.

## 1. Неподвижные рамки

### 1.1. Обратная совместимость не требуется
Платформа нигде ещё не используется, поэтому:
- обратную совместимость сохранять не нужно;
- нерелевантный код и документацию нужно удалять, а не тащить вперёд;
- то, что важно только для истории, переносится в `deprecated` с индексом, а не остаётся активной каноникой.

### 1.2. Рефакторинг идёт сверху вниз
Порядок приоритета такой:
1. бизнес-модель и каноническая документация;
2. сервисные и data seams;
3. UX и операторские сценарии;
4. runtime/deploy/bootstrap;
5. implementation waves.

### 1.3. Enterprise target фиксируется сразу
Даже если реализация делается поэтапно, документация должна сразу описывать:
- многоорганизационную модель;
- package catalogs;
- fleet management;
- billing;
- release policy;
- automation rules.

Иначе первые implementation waves зацементируют слишком узкую модель и потом заблокируют расширение.

## 2. Основа, которая остаётся неизменной

### 2.1. Kubernetes-first runtime
Платформа работает в Kubernetes и управляет Kubernetes-first execution model:
- под каждую задачу поднимается slot;
- slot в первой версии — отдельный namespace;
- в `code-only` режиме slot содержит pod агента и минимальный рабочий контур;
- в `full-env` режиме slot поднимает рабочее окружение проекта и pod агента с нужным доступом;
- в enterprise-расширении slot может быть не namespace, а nested-cluster.

Для ускорения работы:
- допустим prewarm slots;
- при reuse slot платформа должна уметь переключать ветку, чистить state, выполнять миграции и загружать фикстуры перед новой сессией.

### 2.2. Provider-first рабочая модель
Платформа не подменяет GitHub/GitLab как систему рабочих артефактов.

Базовые принципы:
- работа строится вокруг `Issue`, `PR/MR`, комментариев, mentions и provider relationships;
- review `PR/MR` остаётся у провайдера;
- platform-owned состояния — это orchestration, runtime, policy, audit, acceptance и projections;
- все provider-операции проектируются через интерфейсы, где GitHub — первая реализация, GitLab — следующая.

### 2.3. Agent-manager как центр управления
У платформы есть быстрые экземпляры `agent-manager`, через которых:
- пользователь общается с системой через фронтенд, текст и голос;
- обрабатываются mentions и запросы из GitHub/GitLab;
- выбираются flow, роли, шаблоны и следующий шаг;
- запускаются role-агенты и обязательные проверки.

Быстрый `agent-manager` по умолчанию использует дешёвую и быструю модель класса `GPT-5.3-Codex-Spark`, но выбор модели должен быть настраиваемым.

### 2.4. Role-driven flow model
В платформе есть:
- flow;
- stage;
- role;
- `stage role binding`;
- acceptance machine.

Эта модель остаётся центральной и в дальнейшем расширяется automation rules, release policy и package contracts.

### 2.5. Frontend и operator console
Фронтенд полностью переосмысляется:
- стек остаётся;
- UI-библиотека — `PrimeVue`;
- главный рабочий режим — командный центр с чатом и голосом;
- отдельные рабочие пространства нужны для `Issue`, `PR/MR`, `run`, `job` и slot;
- отдельный настроечный контур управляет flow, ролями, package catalogs, доступами, внешними аккаунтами, automation rules и другими policy-объектами.

## 3. Обязательные новые направления wave 5.1

### 3.1. Система плагинов и package platform
Платформа должна иметь полноценную package-модель.

Базовая идея:
- есть встроенные packages;
- есть внешние packages из каталога;
- есть пользовательские packages из произвольных источников;
- package может быть бесплатным или платным;
- package и конкретная версия могут быть верифицированы или не верифицированы;
- source of truth package — внешний репозиторий.

Виды packages:
- `plugin package` — исполняемое расширение платформы;
- `guidance package` — пакет руководящей документации, шаблонов и правил для агентов.

`Plugin package` должен описываться через manifest в репозитории и включать:
- идентичность, описание, локализованные названия и ссылки на изображения;
- package kind;
- версию;
- verification и коммерческий статус;
- требуемые platform APIs;
- обязательные и желательные permissions;
- schema для secret inputs с локализованными названиями, описаниями и типами значений;
- Dockerfile;
- Kubernetes manifests;
- другие runtime-зависимости, необходимые для запуска.

Плагин должен запускаться в Kubernetes-контуре платформы того, кто его установил, и может быть написан на любом языке и стеке.

Примеры, которые сразу должны идти через package platform:
- Telegram approver;
- Telegram feedback adapter;
- внешние интеграции для уведомлений, согласований и обратной связи.

### 3.2. Система руководящей документации и каталог guidance packages
Руководящая документация должна перестать быть неразделимой частью одного монорепозитория.

Нужно сразу заложить:
- отдельные guidance repositories;
- каталог guidance packages;
- import-модель в проект, репозиторий, сервис или саму платформу;
- работу агента только с локально доступной документацией без vector storage в первой версии.

В частности, должны быть возможны отдельные repositories для:
- общих design guidelines;
- backend guidelines на Go;
- frontend guidelines на Vue;
- guidelines по проектированию;
- guidelines по продуктовой документации;
- других guidance packs.

Ключевой принцип:
- guidance package живёт во внешнем repo;
- в рабочий контур он попадает как локальный checkout или import;
- self-improve задача может запускаться против самого guidance repo, а не только против проектного монорепозитория.

### 3.3. Организации, группы пользователей и гибкая access model
Платформа должна поддерживать:
- owner-организацию;
- клиентские организации;
- организации внешних исполнителей;
- будущие SaaS-организации.

Пользователь может:
- состоять в нескольких организациях;
- состоять в нескольких группах;
- получать права напрямую и через membership в group или organization;
- иметь разные роли и права в разных scopes.

Группы бывают:
- глобальные;
- организационные;
- в будущем допускаются проектные и специальные группы, если они не ломают общую модель.

Access model должна поддерживать:
- inheritance;
- явные overrides;
- приоритеты правил;
- разграничение по пользователям, группам, организациям, ролям агентов и другим policy-субъектам.

### 3.4. Серверы, кластеры и fleet management
Мы стартуем с одного сервера и одного Kubernetes-кластера, но документация и код больше не должны считать эту топологию единственно возможной.

Нужно сразу заложить:
- inventory серверов;
- inventory Kubernetes-кластеров;
- связи между организациями, проектами, репозиториями и допустимыми server/cluster scopes;
- placement policy;
- подготовку к multi-zone и delegated clusters;
- возможность закреплять проект, репозиторий или отдельный тяжёлый сервис за конкретным cluster scope.

### 3.5. Биллинг и cost accounting
Платформа должна сразу проектироваться так, чтобы:
- учитывать затраты на compute, storage и runtime;
- учитывать внешние провайдеры и их стоимость;
- учитывать usage packages и в будущем marketplace commissions;
- раскладывать расходы по организациям, проектам и при необходимости по другим scopes;
- формировать основу для invoice и коммерческого учёта.

В enterprise target нужно предусмотреть:
- рублёвый платёжный контур через российские эквайринги;
- валютный контур через `Stripe`;
- выставление счетов организациям, в том числе с разбиением по проектам.

### 3.6. Branch rules, release policy и связь с flow
Платформа должна различать:
- provider-native branches и tags;
- release branches;
- release line или release train;
- release policy;
- rollout policy;
- release task и release flow.

Нужно заложить модель, где:
- задачи и `PR/MR` попадают в release line;
- release branch и related checks живут у провайдера;
- платформа ведёт policy, gates, rollout strategy и операторскую логику;
- можно делать прямой rollout, staged rollout, canary и rollback.

### 3.7. Автозапуск flow по cron и trigger-driven automation
Платформа должна уметь:
- запускать flow по расписанию;
- создавать задачи по расписанию;
- реагировать на внешние триггеры;
- запускать специальные agent roles по alerts и operational signals.

Примеры:
- ночной flow для анализа production logs и метрик;
- flow по поиску slow queries;
- trigger на входящий alert от `Prometheus`;
- запуск расследования по превышению порога `500`-ошибок.

Это должно проектироваться как first-class capability, а не как ad-hoc cron script рядом с платформой.

## 4. MVP floor и enterprise target

| Направление | MVP floor | Enterprise target |
|---|---|---|
| Organizations и groups | owner-организация, клиентские организации, membership пользователей и групп, scoped permissions | SaaS tenancy, delegated admins, сложные inheritance/override policies |
| Package platform | built-in packages, пользовательские sources, локальная установка plugins и guidance packages, verification flag | marketplace, verified package versions, publisher profiles, paid/free packages, revenue share |
| Fleet | один production cluster с явным inventory-слоем и placement policy | multi-cluster, multi-server, per-org/per-project cluster placement, multi-zone |
| Billing | cost records, usage allocation, billing summaries | invoices, payment providers, package marketplace billing, automated charging |
| Release policy | release policy, release branch rules, release line, canary/staged rollout seams | full release programs, tenant-aware release policies, complex trains |
| Automation | cron rules и минимальные event triggers | event buses, reusable automation templates, complex trigger graphs |
| Guidance packages | import из отдельных repos в локальный рабочий контур | marketplace guidance packages, платные и бесплатные bundles, managed updates |

## 5. Архитектурные следствия

### 5.1. Новые или расширяемые owner-domains
Wave 5.1 означает, что в целевой архитектуре должны быть зафиксированы:
- tenancy-aware `access-manager`;
- расширенный `project-catalog`;
- `package-hub`;
- `fleet-manager`;
- `billing-hub`;
- расширенный `agent-manager` с automation rules;
- расширенный governance-контур для release policy и unattended automation.

### 5.2. Что остаётся provider-owned
Даже после этих расширений provider remains owner для:
- branches и tags;
- `Issue`, `PR/MR`, comments и review state;
- repository storage.

Платформа не должна плодить внутренние дубликаты там, где достаточно policy, mirror и orchestration.

## 6. Что нужно сделать прямо сейчас в документации
Сейчас, до старта wave 6, нужно:
1. актуализировать `refactoring/task.md` и индекс программы;
2. добавить канонику wave 5.1;
3. обновить wave 0-5 так, чтобы package platform, tenancy, fleet, billing, release policy и automation rules уже были видны в продуктовой, архитектурной, data, governance и UX-модели;
4. не пытаться детально расписать runtime/deploy ещё раз здесь — это задача wave 6;
5. не тащить это в код до завершения docs-only синхронизации.

## 7. Что нельзя потерять после сжатия контекста
Если контекст сессии будет сокращён, обязательными для восстановления картины остаются следующие идеи:
1. package platform = общий слой для plugins и guidance packages;
2. organizations и groups закладываются сразу, даже если MVP прост;
3. один сервер и один cluster — это только стартовая operational-конфигурация;
4. billing и cost accounting нужно проектировать сейчас, а не после MVP;
5. release branches, release policy, release trains и rollout strategy — часть канонической модели;
6. automation rules по cron и trigger-driven events — часть flow model;
7. guidance packages должны жить во внешних repos и попадать в рабочий контур локально;
8. всё это должно быть отражено в волнах 0-5 до старта wave 6 и implementation waves.
