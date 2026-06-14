# Документация matter-codex

## Разделы

- `idea/` - исходное описание ручной схемы и целевого состояния.
- `runbooks/` - операционные инструкции установки и проверки.
- `strategy/` - согласуемая стратегия MVP, архитектура, rollout и план PR.

## Текущий статус

Проект прошел bootstrap и первые dogfooding-срезы. Актуальный фокус - довести Mattermost-first owner UX: владелец работает через `/agents` карточки, кнопки, списки и dialogs, а typed slash commands остаются fallback/debug интерфейсом.

- независимость standalone `matter-codex` от большой платформы `kodex`;
- способ установки Mattermost в Kubernetes;
- модель изоляции agent pod, PVC и profile-based Kubernetes access;
- OpenAI device-code authorization и agent profile config overlays;
- Mattermost control surface и создание project/repo channels;
- owner UX contract без ручного ввода технических identifiers;
- структуру репозитория;
- сокращенный порядок оставшихся кодовых PR.

Актуальные strategy documents:

- `strategy/owner-ux-contract.md`;
- `strategy/acceptance-matrix.md`;
- `strategy/pr-roadmap.md`;
- `strategy/product-vision.md`;
- `strategy/architecture.md`;
- `strategy/open-decisions.md`;
- `strategy/production-gaps.md`.

Актуальные runbooks:

- `runbooks/mattermost-bootstrap.md`;
- `runbooks/bot-service.md`.
