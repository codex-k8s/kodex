---
id: FE-DOC-001
title: PWA на Vue и TypeScript
type: guide
status: approved
owner: developer
version: 1.1.0
updated: 2026-07-31
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

## Проверки

Обязательный профиль lint, typecheck, build, component и browser checks
определяется `GOV-DOC-003`. Для пользовательского изменения ручная проверка
охватывает desktop/mobile, loading, empty, error, forbidden и ready states.

Связанные документы: `REPO-DOC-001`, `ARCH-DOC-001`.
