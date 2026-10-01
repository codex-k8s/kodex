---
id: OPS-DOC-1787
title: Совместная ручная QA-приёмка интерфейса прототипа
type: operations
status: approved
owner: manager
version: 1.2.0
updated: 2026-10-01
---

# Совместная ручная QA-приёмка

Область: локальный `trusted-cluster`, Issue #1787 и единственный клон
`/home/s/projects/matter-codex`, напрямую примонтированный в Pod read-only для
Air/Vite hot reload. Staging и production не затрагиваются.

## Рабочий цикл

1. Владелец вручную открывает экран и сообщает одно замечание или присылает
   снимок.
2. Исполнитель исправляет актуальный дефект без обязательного промежуточного
   commit, дожидается hot reload и проверяет тот же экран в уже открытой вкладке
   Chrome DevTools MCP.
3. Проверяются console errors/warnings, относящиеся к сценарию network 4xx/5xx,
   пользовательское действие и итоговый screenshot при 1920x1080.
4. Если к моменту проверки владелец уже перешёл на другой экран, предыдущий шаг
   считается принятым: исполнитель не возвращает браузер назад и ждёт следующего
   замечания.
5. Если screenshot подтверждает исправление, исполнитель останавливается и ждёт
   ручного результата владельца.

Русские и английские пользовательские тексты изменяются вместе. Миграции
остаются forward-only, фикстуры без команды владельца не пересоздаются, а
сборка образов выполняется только когда source hot reload недостаточен.

## Журнал

| Время | Экран и замечание | Исправление | Проверка | Результат владельца |
| --- | --- | --- | --- | --- |
| 2026-09-29 | Подготовка совместной QA-сессии | Созданы Issue, ветка и Draft PR | Проверка read-only mount и совпадения исходников host/Pod | Ожидается |
| 2026-10-01 | Каталоги мерцали и повторно читались по HTTP | Добавлен bounded typed bootstrap/delta для глобального и проектного scope через session WebSocket; Pinia stores гидратируются из единого snapshot | Hard reload `/integrations`: 50 строк, console чистая, network без catalog GET, screenshot `/tmp/kodex-integrations-realtime-verified.png` | Ожидается |
| 2026-10-01 | Смена Проекта не должна оставлять старый scope | При смене Проекта прежний socket закрывается, новый session stream получает snapshot выбранного Проекта; gap/resync принудительно запрашивает полный WebSocket snapshot | Переход «Все Проекты» → `Marketplace`: только новый `POST /api/v1/session/ticket`, статус вернулся в «Подключено», screenshot `/tmp/kodex-project-scope-realtime.png` | Ожидается |
| 2026-10-01 | Фоновые изменения не попадали в открытый UI без polling | Добавлены доменные события для очистки корзин, provider lifecycle, email mailbox publication, managed configuration/writeback и role image lifecycle | Gateway и control-plane `go test ./...`; frontend 2036 unit-тестов; browser network без повторных catalog readback | Ожидается |
| 2026-10-01 | Неизвестное platform event маскировалось под системного помощника | Неизвестный event теперь закрыто отклоняется до sequence/outbox; полный registry закреплён unit-тестом | `go test ./internal/repository/postgres/platform` и полный `go test ./...` — PASS | Ожидается |
| 2026-10-01 | Reconnect повторно присылал все каталоги и мог принять частичный bootstrap за полный | Совпадающий cursor теперь подтверждается `PLATFORM_READY` без snapshot; полный bootstrap завершается атомарно и различает явный пустой набор доступных типов от reuse прежнего кэша | Перезапуск gateway под Air: второе соединение получило `PLATFORM_READY`, `SESSION_READY` и одну актуальную delta без bootstrap; все 50 строк сохранялись, console после контрольного reload чистая, screenshot `/tmp/kodex-realtime-reconnect-no-flicker.png` | Ожидается |

## Текущий статус проверок

- `make lint-control-api-gateway-asyncapi check-control-api-gateway-asyncapi-codegen` — PASS.
- `go test ./...` в `control-api-gateway` и `control-plane` — PASS.
- `npm run format:check`, `npm run lint`, `npm run typecheck`, `npm run test:unit` (2041 тест), `npm run build` — PASS; Vite оставляет неблокирующее предупреждение о крупном chunk.
- Интерактивная проверка локального hot-reload стенда через Chrome DevTools MCP — PASS для `/integrations` и смены project scope; это не формальная staging/disposable E2E-приёмка.
- `make test-control-plane-postgres` — NOT RUN: отдельный disposable PostgreSQL в этой сессии не поднимался.
- `npm run test:e2e` — NOT RUN: формальная disposable E2E-установка не запускалась.
- Ручное решение владельца по приёмке — ожидается.
