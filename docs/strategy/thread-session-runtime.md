# Thread Session Runtime

Этот документ фиксирует выбранную модель долгоживущих Codex-сессий для project chat runtime.

## Принятые решения

- Codex session хранится в БД как metadata и сжатый snapshot session JSONL из `$CODEX_HOME/sessions`.
- Runner перед запуском восстанавливает snapshot в `CODEX_HOME`, а для продолжения вызывает `codex exec resume <session_id>`.
- Default manager session привязана к `chat_id + role_id`. Runtime pod держится прогретым 4 часа от последней активности, а сама session history сохраняется в БД.
- Явно упомянутые агенты получают thread-scoped session `chat_id + root_post_id + role_id`. Runtime pod держится прогретым 4 часа от последней активности, а сама session history сохраняется в БД.
- Если агент уже выполняет turn, новые сообщения попадают в FIFO queue этой session.
- Если один агент запускает другого через MCP `mattermost_request_agent`, а у целевого агента в этом thread уже есть queued turn, новый request не создает второй queued turn: prompt дописывается в существующий queued turn с явным указанием инициатора. Это защищает thread от массовых callback-циклов.
- У роли создается отдельная Mattermost bot identity. В chat/channel добавляются реальные bot users, чтобы ответы визуально шли от конкретного агента.
- Mattermost MCP server встроен в bot-service через официальный `github.com/modelcontextprotocol/go-sdk`.
- MCP tools получают capabilities из role/chat session. Запись в thread и запуск другого агента разрешены только при включенной capability.

## Routing сообщений

Top-level message без упоминаний:

- идет в default manager session чата;
- если manager role в чате нет, используется первая enabled role как fallback.

Top-level message с `@agent` mention:

- запускает или продолжает отдельную thread-scoped session для каждого упомянутого role bot;
- все turns привязаны к root post этого сообщения.

Thread reply без упоминаний:

- если в thread есть ровно одна agent session, продолжает ее;
- если agent session нет, идет в default manager session чата;
- если в thread несколько agent sessions, без явного mention идет в default manager session, чтобы не гадать.

Thread reply с `@agent` mention:

- идет в указанную thread-scoped session;
- можно упомянуть несколько agents, тогда создается по turn в каждую session.

Сообщения от Mattermost bot identities не запускают новый turn.

## Queue и жизненный цикл pod

`AgentSession` является долгоживущей сущностью. Kubernetes session pod:

- использует один PVC на session;
- при старте получает session snapshot из bot-service и восстанавливает `CODEX_HOME/sessions`;
- poll-ит bot-service за queued turns;
- выполняет turns строго последовательно;
- после каждого turn отправляет final answer, artifacts, Codex session id и обновленный session snapshot обратно в bot-service через internal HTTP API;
- завершает работу после `expires_at`, если очередь пуста.

TTL:

- default manager chat session pod TTL: 4 часа;
- explicit/thread role session pod TTL: 4 часа;
- каждый queued/running/completed turn продлевает `expires_at`.

## MCP tools

MCP endpoint доступен только внутри runtime через bearer token session pod.

Реализованные MVP tools:

- `mattermost_get_thread` - прочитать сообщения текущего thread;
- `mattermost_search_chat` - поиск по chat/channel;
- `mattermost_post_thread_update` - написать progress/update в текущий thread;
- `mattermost_request_agent` - поставить turn другому агенту в этот thread.

Ограничения:

- tools возвращают ограниченный объем данных;
- tool не отдает секреты, raw tokens или внутренние Kubernetes secret values;
- `post_thread_update` и `request_agent` доступны только если capability явно разрешена для session.
- `request_agent` является единственным штатным способом агенту запустить другого агента. Обычные Mattermost mentions в сообщениях от agent bot identities не запускают turns.

## Snapshot storage

Snapshot хранится в БД в поле `session_archive_gzip_base64` как gzip+tar архива `$CODEX_HOME/sessions`. Runner получает snapshot через internal HTTP endpoint bot-service и после каждого turn отправляет обновленный snapshot обратно.

Если размер snapshot станет слишком большим для одной записи БД или HTTP payload, следующий шаг - заменить payload на object storage/blob endpoint, сохранив БД как source of truth для metadata.
