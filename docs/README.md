# Документация matter-codex

## Разделы

- `idea/` - исходное описание ручной схемы и целевого состояния.
- `runbooks/` - операционные инструкции установки и проверки.
- `strategy/` - согласуемая стратегия MVP, архитектура, rollout и план PR.

## Текущий статус

Проект находится на этапе согласования MVP. Перед кодовой реализацией нужно проверить зафиксированные решения:

- независимость standalone `matter-codex` от большой платформы `kodex`;
- способ установки Mattermost в Kubernetes;
- модель изоляции agent pod и PVC;
- OpenAI device-code authorization и agent profile config overlays;
- Mattermost control surface и создание project/repo channels;
- структуру репозитория;
- порядок максимум 10 кодовых PR.

Актуальные runbooks:

- `runbooks/mattermost-bootstrap.md`;
- `runbooks/bot-service.md`.
