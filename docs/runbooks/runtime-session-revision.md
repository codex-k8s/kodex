---
id: RUN-MC-006
title: Проверка архива, восстановления и смены RuntimeRevision
type: runbook
status: approved
owner: developer
version: 0.1.0
updated: 2026-07-21
---

# Проверка архива, восстановления и смены RuntimeRevision

## Назначение и границы

Сценарий подтверждает переходный контур `archive -> restore -> pod recreation` без развертывания из рабочей ветки и без удаления долговечных ресурсов. Он выполняется владельцем только на уже подготовленной изолированной тестовой установке после сборки и развертывания PR штатным процессом.

Сценарий не проверяет retention/S3 и не разрешает `kubectl apply`, удаление PVC, удаление Secret, чтение значений Secret, `auth.json`, kubeconfig, session token, тела архива или полного манифеста ревизии. Production DSN и production namespace запрещены.

## Безопасные контрольные значения

Использовать отдельную сессию и два безопасных ожидаемых digest:

```text
session_key: runtime-revision-safe-01
revision_v1: 1111111111111111111111111111111111111111111111111111111111111111
revision_v2: 2222222222222222222222222222222222222222222222222222222222222222
archive_version_before: 0
archive_version_after: 1
```

Фактические `session_key` и digest будут другими и вычисляются приложением. Значения выше являются документационными псевдонимами: в командах `<safe-session-key>` заменяется на фактический безопасный идентификатор тестовой сессии. Эти значения используются только для сверки формата и факта смены, а не записываются в БД вручную.

## Предварительные условия

1. Подтвердить, что namespace и PostgreSQL относятся к одноразовой тестовой установке.
2. Зафиксировать `session_key`, имя pod, UID pod, имя PVC и имена session-token Secret, не читая их содержимое.
3. Убедиться, что у сессии нет выполняющегося хода перед сменой конфигурации.
4. Не выполнять реальные изменения кластера из этой рабочей задачи; сценарий предназначен для ручной приемки после штатного развертывания.

Безопасная инвентаризация Kubernetes выводит только имена, UID и digest-аннотацию:

```bash
kubectl -n <test-namespace> get pod <session-pod> \
  -o custom-columns=NAME:.metadata.name,UID:.metadata.uid,REVISION:.metadata.annotations.matter-codex\.dev/runtime-revision-digest
kubectl -n <test-namespace> get pvc <session-pvc> -o custom-columns=NAME:.metadata.name,UID:.metadata.uid
kubectl -n <test-namespace> get secret <session-secret> -o custom-columns=NAME:.metadata.name,UID:.metadata.uid,IMMUTABLE:.immutable
```

## Шаг 1. Создание подтверждённого архива

1. Поставить один безопасный ход в FIFO-очередь сессии и дождаться его штатного завершения.
2. Записать идентификатор хода и проверить только метаданные, не выбирая `payload_gzip_base64`, `manifest` или значения конфигурации:

```sql
select s.session_key,
       s.archive_version,
       s.archive_sha256,
       s.archive_size_bytes,
       t.status,
       t.runtime_revision_id
from matter_codex_agent_sessions s
join matter_codex_agent_session_turns t on t.session_id = s.id
where s.session_key = '<safe-session-key>'
order by t.id desc
limit 1;
```

Ожидается терминальный `status`, `archive_version = 1`, SHA-256 длиной 64 символа, положительный ограниченный размер и ненулевой `runtime_revision_id`. Повтор terminal callback не должен создавать версию 2 или менять checksum версии 1.

## Шаг 2. Восстановление

1. Дождаться штатного завершения хода и состояния сессии `idle`.
2. Повторно сверить имя и UID pod с `session_key`, затем в изолированной тестовой установке удалить только этот pod:

```bash
kubectl -n <test-namespace> delete pod <session-pod> --wait=true
```

3. Убедиться, что PVC и оба session-token Secret остались с прежними UID. Вручную PVC и Secret не удалять.
4. Запустить следующий безопасный ход в той же сессии.
5. Убедиться, что ход продолжил существующую Codex-сессию, а `archive_version`, checksum и размер исходной подтверждённой версии доступны до публикации следующей версии.
6. При намеренно повреждённых метаданных в одноразовом тесте runner должен завершиться до изменения каталога `sessions`; этот отказ подтверждается автоматической проверкой, а не ручной правкой БД.

Локальное доказательство проверки checksum, ограничений и атомарной замены:

```bash
go test ./services/jobs/agent-runner/cmd/agent-runner \
  -run 'TestRestoreSessionArchive(VerifiesConfirmedMetadataBeforeTargetMutation|RequiresDirectoryRootAndLeavesExistingTargetAtomic)' \
  -count=1
```

## Шаг 3. Смена RuntimeRevision и пересоздание только pod

1. Через штатную конфигурацию тестовой установки изменить одно безопасное влияющее поле, например digest образа runner или безопасный overlay роли. Значения Secret не менять и не выводить.
2. Поставить следующий ход. Если предыдущий ход ещё выполняется, pod не должен быть удалён, а применение новой ревизии откладывается.
3. После перехода сессии в idle дождаться reconciliation. Ожидаются новый UID pod и новый digest-аннотация при неизменных UID PVC и session-token Secret.
4. Проверить привязки без чтения манифеста:

```sql
select s.session_key,
       desired.digest as desired_digest,
       applied.digest as applied_digest,
       s.openai_account_name
from matter_codex_agent_sessions s
left join matter_codex_runtime_revisions desired on desired.id = s.desired_runtime_revision_id
left join matter_codex_runtime_revisions applied on applied.id = s.applied_runtime_revision_id
where s.session_key = '<safe-session-key>';
```

После подтверждённого запуска `desired_digest` и `applied_digest` совпадают и отличаются от предыдущего digest. `openai_account_name` остаётся прежним. В очереди сохраняются все ожидающие ходы.

Локальное доказательство переиспользования и безопасного пересоздания:

```bash
go test ./services/external/bot-service/internal/domain/service \
  -run TestRuntimeRevisionComponentPersistsTurnAndRecreatesIdleSessionForChangedDigest \
  -count=1
go test ./services/external/bot-service/internal/integration/kubernetes \
  -run 'TestStartAgentSession(ReusesMatchingRevisionAndDefersActiveMismatch|ChangedRevisionRecreatesOnlyExactPod)' \
  -count=1
```

## Критерии успешной проверки

- terminal-состояние хода и версия архива опубликованы атомарно;
- checksum и размер проверены до изменения каталога восстановления;
- новый ход использует сохранённую учетную запись и подтверждённую ревизию;
- одинаковый digest переиспользует pod;
- изменённый digest не прерывает активный ход и затем меняет только pod;
- PVC, workspace, основной и неизменяемый session-token Secret и FIFO-очередь сохранены;
- в выводах нет значений секретов, тела архива, `auth.json`, kubeconfig и token.

## Откат

Миграция forward-only и не откатывается удалением таблиц или колонок. Для прикладного отката вернуть предыдущий образ bot-service/runner штатным deployment-процессом и оставить новые строки неизменяемыми. Следующий запуск с прежней безопасной конфигурацией создаст либо переиспользует соответствующую `RuntimeRevision`. PVC, Secret и архивы не удалять.
