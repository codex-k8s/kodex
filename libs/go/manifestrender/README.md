# manifestrender

`libs/go/manifestrender` рендерит Kubernetes manifest templates (`*.yaml.tpl`)
через helpers из `libs/go/stackinventory`.

Пакет нужен, чтобы `cmd/manifest-render`, bootstrap preflight и будущие
install/deploy tools использовали один render path вместо отдельных shell или
Go реализаций. Значения env загружаются как override-слой и не логируются
самим пакетом.
