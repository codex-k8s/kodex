# Control Center: история автоматизаций

Минимальный вертикальный срез показывает историю состояний `ScheduledRun`, полученных из structured output MCP-инструмента `mattermost_complete_automation`. Пакет не содержит собственного mock API: хост передаёт сохранённые ответы через `window.__MATTERCODEX_AUTOMATION_HISTORY__`, после чего runtime-парсер закрыто отбрасывает записи, не соответствующие transport-контракту.

Для `requires_human` интерфейс различает:

- `waiting_owner` + `human_decision_status: open` — решение ожидается;
- `succeeded` + `human_decision_status: resolved` — сохранённый ручной шлюз закрыт.

Проверки:

```bash
npm install
npm run typecheck
npm test
npm run build
```
