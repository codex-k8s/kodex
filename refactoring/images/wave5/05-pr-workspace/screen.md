# Рабочее пространство PR/MR

Статус: `reviewing`

## Назначение
Операционный экран для одного `PR/MR`, который показывает review state, acceptance, риск, связанные `run`/`job` и release context, но не пытается заменить нативный diff review провайдера.

## Главные сущности
- `PR/MR`
- связанный `Issue`
- approvals
- Human gate
- acceptance state
- `run`
- `job`
- release context

## Основные блоки
- шапка `PR/MR` и контекст связанной задачи
- верхняя summary-полоса review state
- центральная лента обсуждения и review-сводки с вашими сообщениями справа
- правая колонка `Acceptance и риск`, `Исполнения`, `Платформенные jobs`, `Release context`
- нижние вкладки `Проверки`, `Acceptance`, `Связи`, `Журнал активности`

## Ключевые действия
- открыть `PR/MR` в GitHub/GitLab
- открыть связанный `Issue`
- запустить revise
- дать approval
- перейти к связанному `run` или `job`

## Источники данных
- провайдер: `PR/MR`, review threads, approvals, checks, relationships
- платформа: acceptance projection, risk class, `run`, `job`, release context

## Что намеренно не показывается
- полный diff review как замена провайдеру
- сырые длинные логи
- свободный canvas или graph view
- неосмысленно висящие статусы `approved`/`ожидает`/`нужно решение` без привязки к конкретному контексту сообщения или gate
- длинная лента из множества сообщений, если для понимания достаточно 3-4 ключевых событий

## Открытые вопросы
- нет
