# Рабочее пространство Issue

Статус: `approved`

## Назначение
Единый рабочий экран для `Issue` любого типа: `initiative`, этапной задачи, follow-up, `risk`, `incident`, `self-improve`.

## Главные сущности
- `Issue`
- связанные `PR/MR`
- `run`
- `job`
- slot
- Human gate
- acceptance state

## Основные блоки
- шапка задачи и stage-линия
- центральный диалог и action-полоса
- список связанных `PR/MR`
- блоки `run`, `job`, slot и рисков
- нижние вкладки: `Артефакты`, `Проверки`, `История переходов`, `Связи`, `Журнал активности`

## Семантика вкладок
- `Артефакты` — связанные `Issue`, `PR/MR`, approvals и evidence;
- `Проверки` — acceptance state, review-роли и Human gate;
- `История переходов` — движение по этапам flow;
- `Связи` — relationships между `Issue`, `PR/MR`, `run`, `job`, slot;
- `Журнал активности` — единая timeline событий.

## Ключевые действия
- дать ответ agent-manager
- открыть связанный `Issue` или `PR/MR` из сообщения через badge-кнопку
- перейти к связанному `PR/MR`
- открыть `run`, `job` или slot
- выполнить действие по Human gate
- перейти к связанным `Issue`

## Источники данных
- провайдер: `Issue`, `PR/MR`, comments, relationships
- платформа: acceptance, Human gate, `run`, `job`, slot, risk, activity projections

## Что намеренно не показывается
- отдельная строка артефакта типа `документ`
- единый блок `Связанный PR`, который скрывает остальные `PR/MR`
- плавающая кнопка микрофона на уровне экрана
- одна общая кнопка `Открыть в GitHub` на весь блок связанных `PR/MR`

## Макеты
- [Основной макет](screen.png) — вкладка `Артефакты`, базовый вид рабочего пространства `Issue`.
- [Вкладка `Проверки`](tab-checks.png) — acceptance state, review-роли, Human gate и checklist качества.
- [Вкладка `История переходов`](tab-stage-history.png) — движение по этапам flow, решения, revise-циклы и follow-up.
- [Вкладка `Связи`](tab-links.png) — связанные `PR/MR`, `Issue`, `run`, `job`, slot, release и flow context.
- [Вкладка `Журнал активности`](tab-activity.png) — единая timeline событий по задаче, агентам, provider-комментариям и runtime.

## Открытые вопросы
- нет
