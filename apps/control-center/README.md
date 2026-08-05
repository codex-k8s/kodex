# Control Center: инертный исторический интерфейс

После strategy reset этот каталог не входит в production owner graph. Bot-service
публикует только статические assets по `/control-center/`, но не регистрирует
`GET /api/control-center/v1/automation-runs`, не принимает read-only token и не
исполняет Schedule/Runtime state. Исходный OpenAPI-клиент и UI сохранены как
историческая заготовка до отдельного unit автоматизации; они не являются
поддерживаемым runtime contract Issue #192.

Проверки:

```bash
npm ci
npm run typecheck
npm test
npm run build
```

После изменения исторического OpenAPI-контракта оба клиента пересоздаются из
корня репозитория:

```bash
make gen-openapi
```

## Ручная проверка cutover

1. Открыть `/control-center/` и подтвердить доступность только статических assets.
2. Проверить, что `/api/control-center/v1/automation-runs` не зарегистрирован и
   не получает доступ к историческим таблицам.
3. Проверить, что `MATTERCODEX_CONTROL_CENTER_READ_TOKEN` отсутствует в
   config, Secret template и Deployment bot-service.
