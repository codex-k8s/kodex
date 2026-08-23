---
id: RUN-MC-002
title: Чистое развертывание web-first MatterCodex
type: runbook
status: approved
owner: sre
version: 1.0.1
updated: 2026-08-23
---

# Чистое развертывание web-first MatterCodex

Эта инструкция предназначена только для новой установки без данных, которые
нужно сохранить. Она разрушительна. Исполнитель сначала подтверждает точный
cluster context и получает отдельное решение владельца. Implementation agent
не выполняет эти команды самостоятельно.

## 1. Preflight

1. Зафиксировать exact release SHA и выбранный профиль: `web-only` либо
   `web-with-mattermost`.
2. Убедиться, что Kubernetes context относится к новой disposable/target
   установке, а не к иной staging/production среде.
3. Проверить отсутствие требуемых пользовательских данных и активных Runs.
4. Сохранить только необходимые external secret references и OIDC/registry
   metadata без вывода значений.
5. Отрендерить manifest в файл и проверить его до применения.

## 2. Удаляемый scope reset

Целевой application namespace — `mattercodex-system`. При полном reset удаляется
именно этот namespace и принадлежащие установке cluster-scoped resources,
перечисленные в отрендеренном release manifest. Нельзя использовать wildcard,
`$HOME`, корень workspace или удалять namespace, не совпавший с preflight.

PostgreSQL database/schema и NATS stream этой установки пересоздаются пустыми.
Legacy databases, Mattermost database, старые migration jobs и compatibility
tables не импортируются. OCI registry очищается только от unreferenced artifacts
этой installation policy; promoted role images можно сохранить, если их
provenance/signature/readback доступны новой установке.

## 3. Secret material

Secret values создаются заново owner-controlled materialization tooling. В
terminal, CI output, manifest, Issue и PR выводятся только имена ресурсов/keys и
результат shape/readback. Не копировать старые application grants: authority
keys, workload TLS, database credentials, NATS credentials, OIDC client secret,
provider credentials и optional Mattermost credentials получают новые
generation/revision.

Публичный домен, Origin, OIDC issuer/JWKS, registry endpoints и allowed external
hosts передаются параметрами deployment environment. Репозиторий не задаёт
чужой public domain по умолчанию.

## 4. Порядок новой установки

1. Создать namespace и прямые stateful/security dependencies.
2. Материализовать exact required keys и дождаться readback authority.
3. Запустить `control-plane-cli up` с
   `CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE` через migration Job.
4. Запустить `control-plane-cli broker bootstrap` с exact NATS material.
5. Развернуть `control-plane`, затем runtime/scheduler/integration workers,
   gateway и Control Center.
6. Для Mattermost выбрать профиль `web-with-mattermost`; web-only не содержит
   interaction deployment, trust material или external credential mounts.
7. Выполнить отдельный service-graph smoke после локальной readiness всех Pod.

## 5. Проверка bootstrap

- повторный bootstrap не создаёт вторую Organization или системного помощника;
- initial owner membership разрешается из проверенного OIDC subject;
- stable key системного помощника — `system-assistant`;
- core prompt version, built-in platform capabilities, integration definitions,
  runtime defaults и policies совпадают с shipped revision;
- попытка delete/disable/archive помощника закрыто отклоняется;
- assistant readiness подтверждает фактически ready warm runtime.

## 6. Web-only smoke

1. Войти через Control Center.
2. Создать Project без integrations.
3. Создать Agent с admitted role image и опубликовать instructions.
4. Запустить Agent, увидеть `QUEUED -> RUNNING -> SUCCEEDED` по realtime stream.
5. Открыть result и скачать artifact.
6. Продолжить Session новым Turn.
7. Запустить Workflow с двумя child Agents и проверить graph/callback.
8. Разрешить Human Gate в web и убедиться, что continuation создан один раз.
9. Отменить Run и выполнить retry с новой attempt/`RETRY_OF` lineage.

Mattermost, GitHub и пользовательская Kubernetes integration при этой проверке
отключены. Их отсутствие не является warning или readiness failure.
