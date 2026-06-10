# Smoke-проверка Matter Codex Flow

## Developer-review flow

Проверка `flow-manual-d1` подтверждает, что developer-review flow запускает агентскую задачу в отдельном Kubernetes Job, передает контекст ветки и задачи, а результат можно использовать для ручной проверки владельцем перед созданием PR.
