# Редактор flow

Статус: `reviewing`

## Назначение
Последовательный экран настройки flow как набора этапов, правил, `stage role binding`, acceptance profile и Human gate.

## Главные сущности
- flow
- stage
- `stage role binding`
- acceptance profile
- Human gate
- follow-up rule

## Основные блоки
- список этапов
- инспектор выбранного этапа
- preview всего flow
- блок влияния и использования
- блок публикации изменений через `PR`

## Ключевые действия
- добавить или переставить этап
- настроить артефакты этапа
- задать `stage role binding`
- настроить Human gate
- настроить follow-up и переходы
- создать `PR` с изменениями

## Источники данных
- платформа: flow catalog, role catalog, policy projections
- репозиторий платформы: flow definitions и связанные шаблоны

## Что намеренно не показывается
- свободное ручное расположение нод как источник истины
- runtime-диагностика и operational noise

## Открытые вопросы
- нужен ли отдельный режим сравнения двух версий flow прямо в UI, или достаточно diff в `PR`
