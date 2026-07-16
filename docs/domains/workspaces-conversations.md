---
id: DOM-MC-003
title: Workspaces & Conversations
type: domain
status: proposed
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Workspaces & Conversations

## Назначение

Связывает универсальные Workspace/Room/Conversation с Mattermost teams/channels/posts и определяет routing пользовательских сообщений.

## В границах

- workspace lifecycle и Mattermost team binding;
- room lifecycle и channel binding;
- default agent и room participants;
- conversation/thread bindings;
- inbound message deduplication;
- explicit user routing;
- delivery targets и locale.

Не владеет Turn execution, file blob и Mattermost server configuration.

## Routing

- Сообщение пользователя с явным упоминанием agent направляется этому agent.
- Сообщение пользователя без упоминаний идет default agent контекста.
- Сообщение bot identity не запускает agent по mention.
- Делегирование агентов выполняется только MCP command.
- Internal notrigger property имеет приоритет над текстовыми mentions.
- Повторно доставленный post event не создает duplicate turn.

## Данные

Workspace, Room, AgentAssignment, ConversationBinding, MattermostBinding, InboundEventReceipt.

## События

`WorkspaceCreated`, `RoomCreated`, `ConversationStarted`, `UserInstructionReceived`, `ConversationDeliveryRequested`.

## Acceptance

- Team/channel создаются либо привязываются идемпотентно.
- Пользователь выбирает entities без ввода IDs.
- Role bots приглашаются в team/room.
- Agent callback/attachment/status post не триггерит новый turn.
- Conversation может отсутствовать у headless ScheduledRun.
