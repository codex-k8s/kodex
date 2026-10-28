---
id: QA-PDLC-1830
title: Сквозная приёмка staging на полном цикле разработки
type: qa-plan
status: approved
owner: QA
version: 1.0.0
updated: 2026-10-24
---

# Приёмка по Issue #1830

Начальная контрольная точка — f2e64eb556276211e74e4ccd718fd4614ee1c04e.
Работа ведётся одним QA-worktree; независимые исследователи не меняют staging.
Каждый результат связывается с исходным SHA, фактическим образом и наблюдением.
При dirty diff проверка диагностическая, итоговая приёмка только на чистом SHA.

## План

- [ ] CP/API/PWA/runtime-controller: identity, migrations, actual source/image,
  private configuration snapshot, rollback и доступность соседних приложений.
- [ ] Browser: login/logout/refresh/reload/WebSocket reconnect; Console/Network.
- [ ] Главная после удаления проекта: PROJECT purge → resync → актуальные
  RUN/GATE/SESSION/ARTIFACT каталоги; SYSTEM-история сохраняется (#1831).
- [ ] Аккаунт Codex: AUTHORISED readback, свежий каталог и успешный реальный
  ход; модель gpt-6.1-sol/high подтверждается materialized revision.
- [ ] Помощники создают проект «Журнал обращений», сотрудников и процесс.
- [ ] Аналитик/архитектор уточняют требования и адаптируют документацию.
- [ ] Разработчик создаёт сервис и PR в codex-k8s/test-repo; рецензенты и
  лексический страж проверяют точный SHA; QA/поддержка заводят дефект.
- [ ] Разработчик исправляет дефект; повторная проверка, Human Gate и merge;
  SRE развёртывает через защищённый SSH-доступ к выделенному серверу.
- [ ] Делегирование, очередь, maxConcurrency=2, Stop/cancel/retry/expiry,
  параллельные сессии и отсутствие повторного внешнего действия.
- [ ] Human Gate: ожидание/отказ/expiry/продолжение на тестовых ресурсах.
- [ ] Изоляция project/file/secret/tool, отказ недоступной модели/интеграции.
- [ ] Файлы: quarantine → обязательные проверки → CLEAN admission → выдача;
  отрицательные проверки безопасными стандартными контрольными файлами.
- [ ] История/файлы после адресного рестарта; backup restore в изоляции.
- [ ] Три успешных PDLC-прогона одного чистого SHA, один после рестарта.
- [ ] Сборки штатных образов; immutable live acceptance учитывается отдельно.

## Карта исправления #1831

Инициатор — владелец с transport identity, штатная команда удаления проекта.
CP purge transaction является владельцем состояния: terminal PURGED удаляет
связанный граф и атомарно фиксирует PROJECT_CHANGED. Gateway получает
аутентифицированное событие, не извлекает полномочия из пользовательских полей.
PROJECT_CHANGED требует нового авторитетного чтения связанных каталогов через
существующий PLATFORM_RESYNC_REQUIRED/rejoin с monotonic cursor floor.
RUN snapshot содержит страницу, а не универсальный tombstone. PWA отдельно
хранит членство текущей страницы и подробные записи; отсутствие в странице
не уничтожает detail cache. Home/Runs используют членство страницы.

| Переход | Событие/read path | Требование |
| --- | --- | --- |
| Create/update/archive/restore/purge проекта | PROJECT_CHANGED → resync/rejoin | Все зависимые каталоги перечитаны внутри прежнего owner scope |
| RUN snapshot, частичная страница | exact scope + cursor/page | Проекция страницы заменена, detail cache сохранён |
| RUN snapshot, пустая страница | авторитетный пустой runs | Главная не выдаёт прежние карточки |
| Чужой project scope | отказ client/server scope guard | Ни выдачи, ни смешения владельцев |
| Logout/relogin | owner lifetime reset | Проекция и detail cache очищены |
| Reconnect/retry | свежий snapshot + monotonic versions | Нет повторных внешних действий; это read path |

Изменение не создаёт новую mutation, grant, task или событие. Cancel/lease/
expiry исполнения остаются в существующем owner-контракте; resync обновляет
только представление и доступные зарегистрированные run subscriptions.

## Границы

На тестовом сервере 1 CPU / 1 ГБ; только Docker Compose. SSH/обычные входящие
соединения ограничиваются IP QA-хоста; отдельно разрешены GitHub webhooks и
HTTP-01. Владелец поручил проверку согласований через Chrome MCP только в
изолированном QA-процессе. Итоговые QA-PR Kodex не сливаются; production
не затрагивается. После трёх неудачных попыток одного дефекта этот путь
останавливается, независимые проверки продолжаются.

Полная приёмка пока NOT RUN. Исторические результаты не переносятся.
