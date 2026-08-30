---
id: ADR-MC-004
title: RuntimeRevision и привязка учетной записи
type: decision
status: approved
owner: architect
version: 1.1.0
updated: 2026-08-30
---

# ADR-MC-004. RuntimeRevision и привязка учетной записи

## Решение

Перед каждым ходом строится неизменяемая `RuntimeRevision` и новый
execution-scoped Pod. Session PVC можно сохранить, но env, авторизация, образ,
подключения файлов, права и конфигурация поставщика материализуются заново
перед запуском или возобновлением provider session. Первый Codex adapter
materialize-ит это как app-server `thread/start` либо `thread/resume`, но эти
идентификаторы не входят в универсальную доменную модель.

`AIProviderAccount` выбирается при создании сессии и после первого запуска неизменяема. Автоматическая балансировка разрешена только для новых сессий. Возобновление другой учетной записью запрещено.

Managed OAuth refresh внутри provider turn считается forward-only операционной
ротацией той же учетной записи. Provider-sidecar передает обновленный snapshot
runtime-controller по execution-scoped mTLS callback; runtime-controller
создает следующую immutable Secret, а control-plane активирует ее только через
lease/fence/generation и compare-and-swap с credential revision текущей
`RuntimeRevision`. Уже выполняемый ход остается привязан к прежней revision;
новая revision используется только последующими turns.

Учетная запись с rotating refresh token имеет concurrency limit `1`. Это
предотвращает одновременное потребление одного refresh token независимыми
app-server process. Учетная запись с API key может иметь отдельный bounded
limit, поскольку ее credential не меняется при успешном provider response.

## Последствия

- Изменения конфигурации предсказуемо применяются без вмешательства пользователя.
- Выполняемый ход не меняется на лету.
- Недоступная учетная запись требует повторной авторизации либо новой сессии с передачей контекста.
- Refresh не меняет логическую учетную запись Session и не мутирует
  `RuntimeRevision`; он публикует следующую credential revision для будущих
  turns.
- Ошибка фиксации refresh закрыто завершает provider turn, но не делает
  частично созданную Secret действующей ревизией.
- Целевой архив сессии должен быть независим от жизненного цикла pod и
  реализуется отдельным unit #1002. До него session PVC сохраняется.
