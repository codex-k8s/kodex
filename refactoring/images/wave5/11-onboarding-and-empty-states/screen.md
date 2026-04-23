# Первый запуск и empty states

Статус: `approved`

## Назначение
Управляемый onboarding-экран для первого успешного `run` в новом или ещё неготовом проекте.

## Главные сущности
- onboarding дорожка
- preflight
- project rules
- flow
- первый `Issue`
- первый `run`
- readiness

## Основные блоки
- переключатель трёх дорожек первого запуска
- vertical stepper пути к первому успешному `run`
- компактный диалоговый блок agent-manager с action badges
- правая панель readiness и найденных проблем
- блок empty states текущего проекта
- блок того, что создаст платформа в onboarding

## Ключевые действия
- подключить репозиторий
- загрузить project rules
- создать первый flow
- создать первый `Issue`
- запустить первый `run`

## Источники данных
- платформа: onboarding projection, readiness, first-run journey, presence of flow and runs
- провайдер: repository connection state

## Что намеренно не показывается
- декоративный marketing hero
- отдельная плавающая кнопка микрофона
- свободный canvas и ручной граф
- длинный чат вместо управляемого маршрута
- отдельная крупная CTA-кнопка в шапке, если тот же старт уже читается через шаги и действия agent-manager

## Макеты
- [Основной макет](screen.png) — базовый экран первого запуска, guided onboarding и empty states.

## Открытые вопросы
- нет
