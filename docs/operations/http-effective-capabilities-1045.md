---
id: OPS-HTTP-EFFECTIVE-CAPABILITIES-1045
title: HTTP-проекция текущих возможностей сотрудника и этапа
type: operational-contract
status: approved
owner: platform
version: 1.0.0
updated: 2026-09-05
---

# MVP-UI-31, Issues #1045/#1046

`GET /api/v1/agents/{agentRef}/effective-capabilities` → generated gateway
handler → `GetAgentEffectiveCapabilities` → current owner access transaction
из #1046 → typed SDK `getAgentEffectiveCapabilities` → Agent/Workflow UI.
Signed actor/tenant назначает существующая API boundary; профиль64 регистрирует
точный RPC. Это read path без события и без изменения конфигурации.

Параметры: `query`, `pageSize`, `pageToken`, необязательные `workflowRef` и
`stepKey` только вместе. Owner задаёт total, курсор и digest; HTTP не вычисляет
eligibility и не фильтрует результаты локально. Ответ содержит requested,
required, effective, grantable, закрытый reason/source и безопасные refs/versions.
Длинный owner cursor ограничен 2048 bytes; общий старый limit512 здесь не
обрезает новую проекцию. False/zero values и пустой items сохраняются явно.

Gateway проверяет Agent/Workflow/step identity, версии, digest, timestamp,
размеры, enum, порядок и отсутствие повторов. Integration row обязательно
имеет exact connection/grant/version/definition digest; platform row не может
получить эти поля. Effective требует requested/runtimeReady и required в
Workflow context. Read capability может быть effective при grantable=false:
право пользоваться подключением не подменяется правом выдавать grants.
Недостоверный upstream отклоняется 502 без частичного ответа; owner errors
сохраняют безопасную 400/403/404/503 семантику. Cache-Control — no-store.

Локальные проверки: focused HTTP race, полный gateway race/vet/build, строгая
OpenAPI validation kin-openapi0.135.0 и generated SDK strict typecheck — PASS.
Исходные Go/TS contracts генерируются каноническими Makefile targets; clean
replay фиксируется в PR на итоговом SHA. Первый расширенный запуск содержал
неверный путь тестового пакета boundary (setup FAIL); затем выполнен весь
реальный gateway module `go test -race ./...`.

Релевантная документация уже проверена через Context7 `/getkin/kin-openapi`,
`cmd/validate` и `Validate`; версии toolchain и codegen не меняются.
HTTP передаёт текущую проекцию CP, но real protected CP/browser, новый Docker,
live provider, общий baseline и staging acceptance — NOT RUN в этом checkpoint.
Ручная проверка после интеграции: открыть права сотрудника; выбрать workflow
step; сравнить requested/effective, причины и server search; отозвать разрешение
и убедиться, что новая проекция и повторная выдача закрыто отклоняются.
Изменение additive; rollback возвращает предыдущие HTTP/SDK файлы, не меняя
immutable owner state. Значения секретов и приватные payload не публикуются.
