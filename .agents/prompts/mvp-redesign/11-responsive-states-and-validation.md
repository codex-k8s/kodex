# Responsive, состояния и финальная валидация

## Источники

- `.agents/13-responsive-and-states.md` только как прежний перечень проверок.
- `.agents/14-dark-theme-validation.md`.
- `docs/guides/frontend-vue.md`.
- Все результаты текущего пакета.

## Результат

Создай `docs/design/mvp-redesign/validation.html` и недостающие mobile-файлы,
названные в тематических prompts.

Проверь на отдельных artboards:

- desktop `1440x1024`, tablet около `1024x768`, mobile `390x844`;
- главную, Проект, новый Run, Live Run, сотрудников, Kodex drawer, интеграции и
  RBAC;
- loading первого открытия, background refresh, empty, validation error,
  forbidden, network error, offline snapshot, conflict, running, Human Gate,
  cancel и partial section failure;
- длинные русские названия, 1000+ элементов в async picker, duplicate file
  names и несколько строк metadata;
- keyboard-only flow, видимый focus, focus trap/restore, минимум 44x44 на mobile;
- reduced motion и screen-reader friendly live updates;
- отсутствие overlap, обрезанного текста и layout shift.

Добавь dark-theme validation для основных компонентов и трёх ключевых экранов:
главная, Live Run и контекстный Kodex. Dark theme не должна превращаться в
однотонную тёмно-синюю палитру; статусы сохраняют контраст и смысл.

Проверь интерактивный HTML вручную в браузере: dropdown закрывается снаружи,
drawer возвращает focus, list/grid не меняет размер layout, infinite scroll
добавляет элементы без скачка, canvas поддерживает pan/zoom, а realtime update
не заменяет экран skeleton-состоянием.

В `index.md` перечисли все проверенные сценарии, найденные ограничения и
осознанные отличия от старых макетов. Не выдавай статическую картинку за
проверенный интерактивный сценарий.
