---
id: OPS-PROJECT-PURGE-RECOVERY-UI-1819
title: Подтверждаемое продолжение удаления проекта в PWA
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-19
---

# Подтверждаемое продолжение удаления проекта

Issue: [#1819](https://github.com/codex-k8s/kodex/issues/1819), родитель
[#1816](https://github.com/codex-k8s/kodex/issues/1816). Решение владельца
19.10 — вариант 1. Порядок зависимостей: CP → gateway → PWA.
Каноническая серверная матрица —
[план восстановления удаления](project-purge-1816.md).

## Границы

Это один полный PWA unit. Сервер назначает `RESOLVE_PURGE`, `canResolve`,
версию recovery и digest; UI не вычисляет полномочия из выбранного проекта,
идентификатора в URL или наличия строки в корзине. Операции `getProjectPurgeRecovery`
и `resolveProjectPurgeRecovery` получены штатным OpenAPI codegen. Их exact
organization-bound route не передаёт `X-Kodex-Project-ID`, включая случай
совпадения с выбранным проектом. Глобальный выбор проекта не меняется.

Запросы наследуют owner/document lifetime и ограниченный timeout. Модалка
использует общий `ModalDialog`: Escape, outside close, trap/возврат focus.
Она закрывается также во время POST, но локальный abort не объявляется
отменой серверного решения. Согласие не выбрано при открытии, повторном
чтении, stale и новом owner scope.

## Сценарии и жизненный цикл

| Событие | UI и сетевой эффект |
| --- | --- |
| Корзина с `RESOLVE_PURGE` | Причина остановки и действие «Подробнее»; чтение только после открытия |
| Preview `WAITING_OWNER`, unknown >0 | Необратимость отказа, отдельный unchecked checkbox; без POST до клика человека |
| Preview retry, unknown=0 | Повтор очистки, а не новое решение о потере данных; новое отдельное согласие |
| Explicit submit | Exact snapshot/digest, `If-Match` recovery.version, CSRF и один idempotency key |
| Двойной клик | Второй POST не запускается |
| HTTP409/412 | Fresh GET и сброс согласия, без автоматического POST |
| Неизвестный итог POST | Не показывается успех; preview снимается до readback, прежний ключ сохраняется для явного повтора того же snapshot |
| Close/unmount/scope change | Abort и generation fence; поздний ACK не меняет новое представление, согласие очищено |
| Ответ QUEUED/OBSERVING | Не объявляется полной очисткой; показывается число незавершённых проверок |
| Project WebSocket update/delete | Перечитывается preview открытой модалки; POST не повторяется |
| Техническое продвижение worker | Без секундного polling; явное «Обновить состояние» читает authoritative recovery |
| Проект уже удалён | Сохранённая ссылка `/projects?trash=1&purgeRecovery=<projectRef>` открывает surviving receipt; generic заголовок при отсутствии строки |
| Отзыв прав | Ошибка штатного read/command; UI не сохраняет доступный decision preview после отказа |

Минимальный технический контроль запоздалого объекта не называется полной
очисткой по одному HEAD404. Пользовательские object keys, содержимое, имена
файлов и технический digest не отображаются. В интерфейсе сохраняется только
разрешённая проекция; mutation attempts живут в памяти и отзываются при смене
владельца.

## Ручная проверка владельцем

- [ ] В корзине у заблокированного проекта понятна причина и доступно «Подробнее».
- [ ] Модалка читаема на desktop/mobile; outside click и Escape закрывают её,
  а повторное открытие не сохраняет checkbox.
- [ ] Без отдельного согласия и явного клика POST отсутствует в Network.
- [ ] При stale показываются свежие данные, checkbox снова пустой.
- [ ] После собственного подтверждения показывается реальное состояние,
  не фиктивное «удалено». Ссылку на статус можно открыть после исчезновения проекта.
- [ ] Скриншот, Console и Network не содержат неожиданных ошибок и polling.
- [ ] Проверка не меняет другой проект и не нажимает подтверждение за владельца.

Живые browser-проверки и owner decision выполняет корневой агент в отдельно
разрешённом контуре; в этом unit до их выполнения `NOT RUN`.

## Документация и проверки

Через Context7 проверены Vue 3 `watch` cleanup/AbortController и
`onBeforeUnmount` (официальный источник: `vuejs.org/guide/essentials/watchers.html`).
Зависимости не обновлялись. Изменённый transport использует существующий
generated client и document lifetime, не handwritten fetch.

Результаты команд фиксируются в отчёте на точном SHA, а не выдаются за live
acceptance. Unit покрывает consent, stale, unknown/replay, late response,
owner invalidation, double click, generated transport/CSRF/OCC, SSR состояния
и responsive assertions. Реальный browser screenshot остаётся отдельным доказательством.

Rollback — прежний PWA bundle; серверные ограничения остаются закрытыми.
Скрытие нового UI не отзывает уже принятое сервером решение и не отменяет
его обязательства очистки. Секреты и персональные данные не включены.

### Локальный журнал 19.10

Production-код: `e2e17d290f82a2ed6358377f0ae8572e970f8610`, база
gateway `709035789d9004e7baadc732a52ccddb6e14239e`. Последующая правка
`String(version)` касается только lint синтетического API-теста; поведение
production-кода не меняет.

| Контур | Фактический результат |
| --- | --- |
| Полный `npm run test:unit -- --maxWorkers=4` | PASS: 404 файла, 4013 тестов, 57.63s |
| Новые store/API/SSR/transport тесты после test-only исправления | PASS: 15 тестов |
| `npm run typecheck` | PASS |
| `npm run build` | PASS: 2798 модулей, 8.30s; прежнее предупреждение о крупном chunk, без ошибки сборки |
| `npm run test:e2e:check` | PASS: компиляция и discovery, не выполнение browser E2E |
| Повтор `npm run generate:openapi` | PASS: нет diff generated файлов |
| ESLint изменённой PWA-области | PASS |
| Полный `npm run lint` после исправления синтетического теста | PASS: без предупреждений |
| Prettier изменённых файлов / `git diff --check` | PASS |
| Живой Chrome desktop/mobile, Console/Network | NOT RUN: передано корневому агенту |
| Живое подтверждение владельца и физическая очистка | NOT RUN: агент не подтверждает за человека |

Первый полный unit запуск выявил timeout общего i18n-теста при чрезмерной
параллельности; ограничение четырьмя workers позволило выполнить тест с
исходным лимитом 5s. Отдельный промежуточный
запуск пересёкся с регенерацией SDK и получил ошибки импорта; повтор после
окончания codegen прошёл полностью. Первый lint выявил ошибки новых
синтетических тестов; исправлены, без ослабления правил ESLint.
