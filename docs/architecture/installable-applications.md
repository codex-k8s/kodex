---
id: ARCH-MC-012
title: Архитектура устанавливаемых приложений
type: architecture
status: approved
owner: architect
version: 1.0.0
updated: 2026-08-25
---

# Архитектура устанавливаемых приложений

## Статус и границы

Документ фиксирует утверждённую будущую архитектуру package/application. Все
новые агрегаты, контракты и deployable-компоненты этого документа относятся к
`POST-MVP`. Текущий web-first MVP не зависит от package catalog, package store
или установленного приложения и не получает временных заглушек для них.

До начала реализации требуется отдельный Issue на полный package unit с
контрактами, lifecycle matrix, storage, observability, deploy и ручной
проверкой. Имена wire-контрактов и физических таблиц настоящим документом не
утверждаются.

## Логические границы

| Граница                        | Владеет                                                                                          | Не владеет                                                                              |
| ------------------------------ | ------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------- |
| Package catalog                | Источники, пакеты, неизменяемые версии, manifest, результаты проверки и desired state установок. | Runtime workloads, значения секретов, provider effects, grants и общий аудит платформы. |
| Access в `control-plane`       | Actor, область установки, platform actions, grants, risk policy и Human Gate.                    | Содержимое пакета и выполнение его workload.                                            |
| Audit/outbox в `control-plane` | Авторитетная запись команды установки, решения человека и доменных фактов.                       | Бинарные артефакты пакета и технические логи workload.                                  |
| `runtime-controller`           | Материализация одобренного immutable runtime plan, health и техническое состояние workloads.     | Выбор пакета, разрешения и business state установки.                                    |
| `integration-gateway`          | Исполнение типизированных внешних effects и изоляция credentials.                                | Установка пакета, выдача grants и оркестрация agents.                                   |
| Artifact/registry adapters     | Проверяемое хранение и чтение exact manifest, OCI images и content artifacts.                    | Каталоговая видимость, установка и access policy.                                       |
| Store adapter                  | Синхронизация подписанного/проверяемого индекса внешнего источника.                              | Локальное состояние установки и решение о доверии.                                      |

Package catalog является одним логическим bounded context. Решение, будет ли
он отдельным внутренним сервисом или модулем `control-plane`, принимается перед
реализацией по нагрузке и ownership. Независимо от физического размещения его
таблицы и команды не разделяются между несколькими владельцами.

## Модель состояния

### PackageSource

Хранит тип источника, immutable configuration revision, состояние последней
синхронизации и локальную trust policy. Secret values находятся в secret
storage; источник хранит только ссылки и безопасные metadata.

Типы источников:

- `internal` — управляемый владельцем инсталляции каталог;
- `external_store` — удалённый магазин через специализированный adapter;
- `user_source` — прямой источник, зарегистрированный администратором.

### ApplicationPackage и PackageVersion

`ApplicationPackage` задаёт устойчивую identity. `PackageVersion` неизменяема и
связывает source revision, manifest digest и каждый исполняемый или контентный
artifact digest. Mutable tag не является идентификатором версии.

Manifest проходит синтаксическую и семантическую проверку до публикации версии
в локальном каталоге. Ошибка, неизвестная обязательная capability или
несовместимая версия платформы закрыто отклоняются.

### PackageInstallation

Установка связывает exact package version, server-resolved scope, effective
capabilities, одобренные permissions, secret bindings и immutable runtime plan.
Desired state и observed state разделены. Каждая операция использует OCC,
semantic idempotency и отдельную attempt.

Состояния должны покрывать как минимум `PLANNED`, `WAITING_OWNER`, `APPLYING`,
`ACTIVE`, `DEGRADED`, `SUSPENDED`, `FAILED` и `REMOVED`. Точная state machine и
terminal events утверждаются в lifecycle matrix реализации.

## Manifest

Концептуальная форма manifest:

```yaml
apiVersion: packages.kodex.io/v1alpha1
kind: ApplicationPackage
metadata:
  name: example-application
  version: 1.2.3
  publisher: example
spec:
  compatibility: {}
  capabilities: []
  permissions: []
  secretsSchema: {}
  runtime: {}
  installationScopes: []
  dependencies: []
  lifecycle: {}
```

Это пример структуры, а не готовый contract. Будущая schema должна обеспечить:

- canonical serialization и digest;
- закрытый versioned registry capabilities и permissions;
- локализованные display metadata отдельно от stable keys;
- ссылки на immutable artifacts и images только по digest;
- ограничения ресурсов, сети, volumes и platform API;
- health/readiness и безопасную failure policy;
- совместимость с версией платформы и runtime ABI;
- secret schema без default secret values;
- декларативный uninstall/retention plan без произвольного privileged shell.

Категории для UI не влияют на admission. Capability, отсутствующая в
утверждённом registry, не исполняется и не преобразуется в универсальный proxy.

## Capabilities, permissions и secrets

Capability описывает предоставляемый результат: например, MCP tool, внешний
канал, набор руководств, пользовательскую поверхность или управляемое
развёртывание. Permission описывает необходимое действие над ресурсом, но не
является выданным grant.

При установке `control-plane`:

1. разрешает actor и target scope из проверенной session boundary;
2. пересекает требования manifest с политикой инсталляции;
3. показывает пользователю exact permission plan и класс риска;
4. создаёт Human Gate для опасных возможностей;
5. выпускает только одобренные grants с ограниченной областью и сроком;
6. фиксирует command, audit и outbox facts одной транзакцией владельца.

Схема секрета описывает stable key, тип, обязательность, формат и
локализованную подсказку. Значение вводится отдельно, хранится в secret storage
и материализуется только в компонент, которому оно требуется. Package catalog,
Control Center JSON, события и audit никогда не содержат значение.

## Источники и локальная истина

Store adapter получает versioned index, проверяет provenance транспортного
ответа и сохраняет bounded snapshot. Package catalog импортирует metadata и
manifest в локальную БД. Поиск, просмотр и планирование установки читают
локальное состояние, поэтому кратковременная недоступность магазина не ломает
Control Center.

Доверие назначается локально и не наследуется от надписи «внутренний» или
«официальный». Политика может учитывать подпись издателя, allowlist, provenance,
SBOM, vulnerability verdict, лицензию и ручное решение владельца.

Магазин сам может поставляться как `ApplicationPackage`, предоставляющий
capability источника каталога. Bootstrap внутреннего каталога при этом не может
циклически зависеть от уже установленного магазина.

## Runtime materialization

Manifest может объявить один или несколько режимов materialization:

- content-only: проверенные документы, шаблоны или UI resources без workload;
- managed workload: изолированный workload из exact promoted image;
- integration adapter: типизированные definitions/tools для
  `integration-gateway`;
- agent extension: ограниченные инструкции, инструменты или capability для
  выбранного Agent через свежую `RuntimeRevision`.

Content-only пакет руководств или шаблонов сохраняет package/version
provenance. Импортируемые шаблоны становятся отдельными draft-объектами
`control-plane`; обновление пакета не перезаписывает опубликованные инструкции,
Agent или Workflow. Проектные документы остаются источниками конкретного
Проекта и не становятся общими guidance только из-за установки пакета.

Режим является закрытым контрактом, а не произвольным shell hook.
`runtime-controller` получает только server-owned immutable plan после
проверки manifest, permissions, secrets readiness и Human Gate. Он не читает
магазин и не принимает caller-provided Kubernetes manifest как authority.

Пакет развёртывания в nested cluster является обычным integration/runtime
application. Он может содержать проверенные templates и images, но доступ к
целевому кластеру получает через отдельную `IntegrationConnection`, exact
capability/grant и обязательную risk policy. Core Kodex не знает бизнес-смысл
развёртываемой нагрузки и не получает встроенный cluster-admin path.

## Upgrade, rollback и удаление

- Upgrade создаёт новую attempt с новой exact version и runtime plan.
- Активная версия не заменяется до успешного readback новой materialization.
- Rollback выбирает ранее проверенную совместимую версию и не переиспользует
  устаревшие grants или secret bindings без повторной проверки.
- Suspend отзывает runtime grants и останавливает workload, не удаляя
  авторитетное состояние установки.
- Uninstall исполняет утверждённую retention policy, отзывает grants, закрывает
  workloads и сохраняет audit. Удаление внешнего источника не является
  uninstall.
- Неуспешная cleanup-операция остаётся наблюдаемым состоянием и допускает
  bounded retry; она не маскируется как `REMOVED`.

## События и наблюдаемость

Команды изменения установки атомарно фиксируют audit и transactional outbox.
Минимальный будущий набор фактов включает регистрацию версии, изменение плана,
активацию, degradation, suspend, upgrade и удаление. Полные payload, consumers,
cardinality и terminal semantics утверждаются вместе с contract unit.

Технические метрики runtime не заменяют business state установки. Control
Center показывает источник, exact version, scope, requested/effective
permissions, readiness, последнюю операцию и безопасное действие восстановления.

## Последовательность POST-MVP реализации

1. Утвердить lifecycle matrix, capability registry и manifest schema.
2. Реализовать локальный package catalog и пользовательский direct source без
   внешнего магазина.
3. Добавить install plan, access/Human Gate, audit и content-only
   materialization.
4. Добавить managed runtime и integration adapter через существующие границы.
5. Реализовать внешний store adapter и синхронизацию каталога.
6. Только после проверенного внутреннего контура рассматривать публичный магазин
   и коммерческие функции.
