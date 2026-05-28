# stackinventory

`libs/go/stackinventory` читает корневой `services.yaml` как операционный
инвентарь стека платформы: версии, образы и deploy inventory для render,
bootstrap, install и будущих deploy tools.

Граница важна: это не проектный `services.yaml` пользовательского репозитория.
Проектной политикой, импортом и проверенной проекцией владеет
`project-catalog`.

Новые Go-инструменты, которым нужны версии или образы платформенного стека,
должны использовать эту библиотеку вместо собственного YAML/awk/parsing слоя.
Shell wrappers могут оставлять только минимальный fallback там, где Go-wrapper
ещё не является активным entrypoint.

Для рендера Kubernetes templates поверх этого inventory используется
`libs/go/manifestrender`; новые preflight/install/deploy tools не должны
создавать отдельный render path.
