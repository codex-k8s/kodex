---
id: PRD-MC-004
title: Пользовательские сценарии web-first Kodex
type: product
status: approved
owner: product
version: 1.0.0
updated: 2026-08-22
---

# Пользовательские сценарии web-first Kodex

## UC-01. Первый полезный результат без интеграций

Владелец входит, видит готового Помощника Kodex, создаёт Проект «Продажи»,
ИИ-сотрудника «Аналитик лидов» и инструкции, запускает анализ загруженного CSV,
наблюдает live graph и скачивает итоговый отчёт. Ни Git, ни Kubernetes,
Mattermost, CRM или внешнее хранилище не требуются.

## UC-02. Создание ИИ-сотрудника через помощника

Владелец просит подготовить сотрудника для классификации обращений. Помощник
показывает plan preview: Project, Agent profile, instruction draft, capabilities
и role image. После явного применения typed operations создают обычные ресурсы;
audit содержит пользователя и системного помощника.

## UC-03. Собственное окружение роли

Администратор создаёт RoleImageRecipe для юридического сотрудника с PDF/OCR и
офисными утилитами. `role-image-builder` собирает образ через BuildKit, supply
chain допускает exact digest, после чего каждый turn этого агента запускается в
отдельном Pod с этим окружением и защищённым `agent-runner`.

## UC-04. Многоагентный не-IT Процесс

Контент-координатор запускает Workflow: исследователь готовит факты, автор пишет
черновик, редактор проверяет. Делегирование идёт через типизированный MCP tool;
backend материализует nodes/edges и callbacks. Пользователь видит прогресс всех
ветвей и финальный пакет файлов.

## UC-05. IT Процесс как один из вариантов

Проект разработки подключает GitHub и Kubernetes definitions, выдаёт точные
grants агентам и запускает проверку релиза. Эти integrations не становятся
Project authority и могут быть отключены без изменения core модели.

## UC-06. Долговечное решение человека

Бухгалтерский Workflow просит согласовать платёж. Run переходит в
`WAITING_HUMAN`; Pod исполнителя может быть удалён. Владелец принимает решение в
web, Session восстанавливается и получает результат ровно один раз. Optional
Mattermost mirror после web-решения показывает stale/conflict readback.

## UC-07. Продолжение Session

После результата пользователь добавляет уточнение в существующую Session.
Control-plane ставит новый FIFO Turn, создаёт свежую RuntimeRevision и новый Pod
того же promoted role image, сохраняя provider session и историю.

## UC-08. Cancel и retry

Пользователь отменяет зависший Workflow. Все активные claims, gates и nodes
закрываются согласованно. Retry создаёт новую attempt и видимую lineage-связь,
не изменяя историю первой попытки.

## UC-09. Подключение CRM

Администратор создаёт connection, проверяет его и выдаёт capability только
агенту продаж. Credential остаётся в integration gateway. MCP invocation без
grant отклоняется; опасное изменение проходит approval policy и audit.

## UC-10. WebSocket reconnect

Клиент получил sequence N и потерял соединение. После reconnect gateway
авторизует Run, восстанавливает N+1…N+K из durable stream либо отдаёт новый
snapshot. Reducer не создаёт duplicate nodes и не откатывает новое состояние.

## UC-11. Optional Mattermost

Организация отдельно включает notifications и result mirror, не включая inbound
или gate decisions. Outage адаптера виден как retryable delivery failure, а
успешный core Run, result и artifacts остаются доступными в Control Center.
