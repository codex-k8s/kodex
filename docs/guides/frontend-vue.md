---
id: FE-DOC-001
title: PWA на Vue и TypeScript
type: guide
status: approved
owner: developer
version: 1.2.3
updated: 2026-09-09
---

# PWA на Vue и TypeScript

`FE-DOC-001` задает структуру и границы служебной PWA Kodex. Полное
дерево с комментариями приведено в `REPO-DOC-001`.

## Размещение

PWA владельца и операторов размещается в
`services/staff/control-center`.

## Базовый стек

- Vue 3 и Composition API.
- TypeScript в строгом режиме.
- Vite.
- Pinia для состояния сценария.
- Vue Router для маршрутов.
- vue-i18n для пользовательских текстов.
- Сгенерированный TypeScript client из OpenAPI.
- AsyncAPI-generated types/client boundary для WebSocket, если сценарий
  использует события.

Актуальные версии и API проверяются через Context7 перед изменением
зависимостей.

## Направление зависимостей

```text
main/App -> app
app router -> pages
pages -> features + shared/ui
features -> shared/api + shared/lib + shared/ui
shared/api adapter -> shared/api/generated
```

Обратные зависимости запрещены:

- `shared` не импортирует `features` или `pages`;
- одна feature не импортирует внутренности другой feature;
- `generated` ни от чего не зависит и не редактируется вручную;
- page не реализует API orchestration и бизнес-состояние;
- Vue component не вызывает `fetch`, `axios` или generated client напрямую.

## `app`

`src/app` содержит только сборку приложения:

- router и route metadata;
- i18n setup;
- UI/plugins registration;
- global styles и design tokens;
- верхнеуровневые providers.

Бизнес-сценарий и API state в `app` не размещаются.

## `pages`

Page:

- собирает одну route surface из feature-компонентов;
- обрабатывает route params через типизированную boundary;
- задает layout страницы;
- показывает loading/empty/error/ready states через feature state.

Page не содержит дублированные DTO, SQL-like filtering, произвольные network
requests и общую библиотеку компонентов.

## `features`

Каждый пользовательский сценарий находится в `src/features/<feature>`:

```text
features/resources/
├── api.ts                  # вызовы общего API adapter для сценария
├── model.ts                # UI model и преобразования из API DTO
├── store.ts                # Pinia state, actions и конкурентность запросов
├── components/             # UI только этого сценария
```

Feature:

- не экспортирует внутреннюю структуру без необходимости;
- отделяет API DTO от UI model;
- отменяет или игнорирует устаревший async result;
- связывает запрос, каждый retry и применение readback с одним временем жизни
  owner-контекста. Logout/invalidation отменяет этот контекст до очистки store;
  повторный вход создаёт новый. Сброс локального счётчика запроса не допускает
  повторного принятия старого ACK: старые данные, ошибки и HTTP 401 не изменяют
  состояние новой сессии. Уже выполненная mutation не объявляется отменённой
  на сервере из-за локального abort; её восстановление использует утверждённый
  idempotency/receipt path;
- нормализует ошибки через `shared/api/errors`;
- имеет явные loading, empty, error, forbidden и ready states, если они
  применимы.

## `shared/api`

- `generated/` полностью создается из утвержденного контракта.
- `<gateway>.ts` является типизированной оболочкой generated client:
  configuration, auth transport, timeout, correlation id и safe error mapping.
- `errors.ts` превращает transport problem details в устойчивые UI errors.
- Feature adapter не строит URL вручную, если операция есть в generated client.
- Секреты и server-only credentials не попадают в frontend config.

При изменении OpenAPI сначала меняется источник контракта, затем запускается
codegen, после чего адаптируется handwritten boundary.

## State и конкурентность

- Длительное состояние сценария хранится в Pinia store, локальное визуальное
  состояние - в component.
- Store не хранит сырой client instance в serializable state.
- Повторный request не позволяет более старому response перезаписать новое
  состояние.
- Pagination, sorting и filters имеют типизированную model.
- Cache invalidation задается явно после mutation.

## Завершение документа и исходящих запросов

Owner lifetime запроса включает локальное время жизни документа. `beforeunload`
закрывает его до provisional navigation, `pagehide` служит дополнительной
границей. Закрытый scope не запускает следующий retry, следующую операцию
bounded resync или native fetch после асинхронного interceptor. Проверка
не должна оставаться только внутри generated SDK: wrapper проверяет signal
непосредственно перед native fetch. Generated files не правятся вручную.

При отменённом уходе доверенный pointer/keyboard ввод или `pageshow` создаёт
новый локальный scope. Старые signals остаются закрытыми, old ACK не применяется
и старый retry не переходит в новый scope. Новое чтение не объединяется с
незавершённым promise старого resync. Realtime приостанавливает соединение,
сохраняет subscriptions/cursors и использует штатный ticket/rejoin после
возобновления; локальная граница не меняет session TTL или полномочия.

Abort после отправленной mutation не доказывает отмены на сервере. Такая
операция сохраняет прежний idempotency/UNKNOWN recovery contract; автоматический
повтор mutation на `pageshow` или пользовательский ввод запрещён. Реальные HTTP,
contract и access-control ошибки вне закрытого scope остаются наблюдаемыми.

## Владение конфигурацией UI и Git

Для конфигурации с несколькими источниками записи PWA отображает назначаемые
сервером `managed_by=ui|git`, идентичность source, revision/commit и состояние
drift из
авторитетного ответа. UI не вычисляет и не изменяет эти поля локально.

- Обычная форма update выключена для объекта, принадлежащего Git.
- Переход `detach` либо `copy` является отдельной подтверждаемой серверной
  операцией с собственными permission, OCC/idempotency и аудитом.
- `detach` показывает, какая source/revision перестанет управлять объектом, и
  создаёт новую назначаемую сервером revision; тихое продолжение редактирования
  запрещено.
- Git reconcile status/readback не отображается как успешный только по
  локальному optimistic state.
- Конфликт между изменением UI и новой Git revision сохраняет авторитетное
  состояние сервера и предлагает явный путь reload/detach/copy.

Frontend не подменяет серверное ограждение: подделанный API request к объекту,
принадлежащему Git, должен быть отклонён внутренним владельцем данных. UI лишь
делает утверждённую семантику понятной пользователю.

## Компоненты и UI

- `shared/ui` содержит только действительно переиспользуемые primitives.
- Feature components отражают предметный сценарий и остаются рядом с feature.
- Компонент имеет стабильные размеры для toolbar, table, card, dialog и
  controls, чтобы динамический текст не сдвигал интерфейс.
- Для форм используются подходящие controls: select, checkbox, switch,
  segmented control, stepper и field validation.
- Кнопки с общеизвестным действием используют иконку и tooltip.
- Интерфейс проверяется на desktop и mobile без пересечения текста и controls.
- Пользователь не видит сырой backend error, stack trace, secret или внутренний
  identifier вместо понятного имени.

Начальный focus модального окна назначается общим `ModalDialog` один раз
после render и только если пользователь ещё не выбрал элемент внутри панели.
Поле отмечается `data-dialog-initial-focus`; native `autofocus` в динамической
модалке не используется, поскольку поздний browser callback может перенести
ввод в соседнее поле. Escape, Tab trap и возврат focus сохраняются.

JSON Schema pattern, используемый как HTML `pattern`, обязан компилироваться
и сохранять одинаковую семантику в режимах `u` и `v`. Literal characters внутри
классов экранируются в канонической схеме, generated validator обновляется
штатно. Невалидный HTML pattern нельзя компенсировать подавлением console error.

После browser capture потенциально платная отправка аудио допускается только
по явному действию пользователя: штатный повторный клик остановки записи уже
является таким действием и не требует отдельной модалки подтверждения.
Системная остановка потока, отзыв разрешения микрофона, превышение лимита,
navigation, logout и unmount отменяют capture/request и освобождают ресурсы
без новой отправки аудио. Поздние callbacks не принимаются новым capture.
Вставка async результата в редактор сохраняет selection, focus и scroll;
изменение изолируется одной undo transaction от предшествующего и следующего
ручного ввода. Отмена после отправки прекращает ожидание, но не доказывает
отсутствие уже выполненного эффекта провайдера и не разрешает повтор.

## Локализация

- Пользовательские строки не пишутся напрямую в component/store.
- Ключи находятся в `src/i18n/<locale>.ts`.
- Русский является обязательной локалью проекта; английский сохраняется как
  дополнительная локаль для устойчивых интерфейсных терминов.
- Идентификаторы, protocol names и неизмененный внешний вывод не переводятся.

## PWA и runtime

- Service worker и offline policy добавляются только с явной моделью
  обновления, cache invalidation и ошибочного offline state.
- Runtime config отделяется от build-time secrets.
- Nginx fallback поддерживает Vue Router без скрытия отсутствующих static
  assets.
- Security headers и CSP согласуются с gateway/auth контуром.
- Runtime imports ограничены файлами, поставляемыми вместе с PWA во всех
  активных профилях. Канонические контракты за пределами frontend используются
  генератором; необходимые UI схемы поставляются в `shared/api/generated`.
  Их загрузка проверяется без внешнего дерева контрактов и без расширения
  файлового доступа Vite.

## Проверки

Обязательный профиль lint, typecheck, build, component и browser checks
определяется `GOV-DOC-003`. Для пользовательского изменения ручная проверка
охватывает desktop/mobile, loading, empty, error, forbidden и ready states.

Связанные документы: `REPO-DOC-001`, `ARCH-DOC-001`.
