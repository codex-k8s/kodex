---
id: OPS-OPENCODE-1982
title: OpenCode runtime и доставка профиля
type: operation
status: approved
owner: developer
version: 1.0.1
updated: 2026-11-14
---

# OpenCode runtime и доставка профиля

Решение владельца в #2108 заменяет исходный additive-профиль #1982:
OpenCode — единственный runtime. Исполняемый Codex, его fallback и старые
session/overlay readers не поддерживаются. Модели OpenAI и имена с Codex
остаются допустимыми моделями, если их разрешает выбранный OpenCode adapter.
История прежней реализации доступна через Git, но не задаёт нынешний runtime.

Канонические API/version pins, native sessions, compaction, managed tools,
checkpoint/export/import, context/cache и сквозные lifecycle matrices описаны
в [OPS-OPENCODE-NATIVE-2108](opencode-native-sessions-2108.md).
[Чистая установка](../runbooks/fresh-install.md) определяет inventory,
ownership plan/apply и защиту соседних ресурсов. Старые данные в новую
установку не переносятся; новая история сохраняется по native checkpoint policy.

## Проверка версии в inventory

OpenCode1.19.5 создаёт XDG-каталоги даже до обработки `--version`.
Поэтому inventory-parent заранее создаёт отдельный пустой HOME вне `/image`,
включая private tmp, и делает все каталоги доступными только для чтения.
Дочерняя проба принимает только ограниченное дерево root-owned каталогов без
файлов, ссылок и права записи. После завершения или отказа parent удаляет HOME.

Существующие UID/GID10001, пустые capabilities/groups, Landlock/seccomp,
закрытый реестр аргументов, двухсекундный deadline и ограниченный вывод
сохраняются. Остальные инструменты используют прежний `HOME=/nonexistent`.
Это проверка версии исполняемого файла, а не доказательство настройки модели,
наличия credentials, допуска образа или успешного агентного turn.

## Совместимость MCP-каталога

`runtime-controller` публикует `required: []` для object schemas без обязательных
полей. Значение `null` недопустимо: строгий MCP-клиент OpenCode отвергает весь
каталог, хотя командная проверка `opencode mcp list` может завершиться с кодом0.
Готовность проверяется по статусу подключения и реальному вызову разрешённого
инструмента с результатом, а не только по успешному текстовому ответу модели.

Синтетическая регрессия вызывает настоящий JSON-RPC `tools/list` для agent,
SYSTEM и PROJECT профилей, проверяет вложенные input/output schemas и компилирует
их. Пустой optional input остаётся допустимым; обязательные поля, ограничения
длины и запрет неизвестных аргументов проверяются отдельно. Изменение формы
каталога сохраняет прежнюю проверку прав при вызове инструментов.

## Доставка и проверка

Конфигурация провайдера принадлежит установке в Git, выбор account/model и
допустимые preferences — существующему lifecycle Kodex. Неполная конфигурация
закрыто отклоняется до запуска; отсутствие descriptor не включает другой
движок. Exact runtime/input/contract и RoleImage ABI pins обязательны для всех
совместимых immutable компонентов. Generated output вручную не меняется.

Release/component profiles не обходят provenance, drain, readiness и owner
approval. Локальный native harness подтверждает конкретный upstream adapter,
а не установленный workspace producer/admission/PVC/S3 path. Состояние общей
приёмки ведётся в #2108: пока не выполнена доступная cluster matrix, весь
OpenCode-only переход не объявляется готовым. Подробные команды и границы
локальных доказательств находятся в OPS-OPENCODE-NATIVE-2108.
