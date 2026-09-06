---
id: OPS-DOC-1098
title: Авторитетные данные карточек каталогов
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-06
---

# Карточки проектов, сотрудников и процессов

Источники: Issue #1098, основной Issue #1046 / PR #1071, MVP-UI-10/28/32.
Чтение использует существующий проверенный actor/organization/project scope.
Payload не задаёт полномочия; карточка не расширяет доступность зависимостей.

| Требование | Внешний путь / RPC | Владелец и данные | Результат и потребитель |
| --- | --- | --- | --- |
| UI-10 | Project list/get, существующие mutation readback | CP: Project и доступные дочерние ресурсы; один пакетный SQL | Project.last_activity_at14 и integration_state15; HTTP/SDK/PWA карточка |
| UI-28 | Agent list/get, существующие mutation readback | CP: активный AGENT_EXECUTION и доступный Run | Agent.current_run_ref24; ссылка на текущую работу |
| UI-32 | Workflow list/get, существующие mutation readback | CP: опубликованный body, иначе draft; доступные Agent и Run/Gate | Workflow.card_summary12; counts1–6, nullable activity7 |

List/get и свежий command/replay readback используют одинаковые пакетные
проекции. Новых команд, permissions, событий, grants или внешних effects нет.
Version/idempotency остаются у существующих команд; новые поля вычисляются из
актуального owner snapshot и не восстанавливаются из старой receipt-проекции.
Run events не содержат Project/Agent/Workflow карточку: их потребитель перечитывает
соответствующий защищённый ресурс. Чтение Project, Agent и Workflow list/get
объединяет базовые поля и проекцию в одной read-only RepeatableRead транзакции.

## Правила значений

- Project counts считают только доступных сотрудников/процессы и корневые
  исполнения. Pending decisions относятся к доступным корневым Run.
- Activity проекта — максимум времени изменений доступных Agent, Workflow,
  Run, Gate и связей интеграций. Project.updated_at не заменяет activity.
  При отсутствии таких фактов значение отсутствует.
- Integration state относится к доступным сохранённым grants и connections:
  NONE — связей нет; DEGRADED — есть доказанный неготовый/отключённый элемент;
  UNKNOWN — готовность хотя бы одного элемента ещё не определена; READY — все
  элементы готовы. DEGRADED имеет приоритет над UNKNOWN. Скрытые связи не
  влияют на эти значения и не раскрываются через счётчики.
- Workflow summary использует опубликованную спецификацию, если она есть.
  Unique agents дедуплицирует доступных исполнителей этапов и координатора.
  Parallel groups — число положительных групп параллельных этапов.
  Last activity — последнее изменение доступного корневого исполнения;
  при отсутствии исполнений значение отсутствует.
- Current run выбирается только среди доступных активных Run с незавершённым
  узлом исполнения данного Agent. Отсутствующая ссылка не доказывает отсутствие
  скрытой работы. Terminal/cancel/expiry удаляют ссылку при следующем чтении.

Deploy owner остаётся CP; additive Proto доставляется каноническим генератором.
Изменений миграций и конфигурации среды для этой проекции нет. Проверки
фиксируются на конкретном checkpoint отдельно; live/deploy не выполняются.
