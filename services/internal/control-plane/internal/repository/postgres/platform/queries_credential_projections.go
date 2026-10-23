package platform

import _ "embed"

var (
	// queryCredentialProjectionTrustedAuthority resolves active root actor and owner scope from an exact live runtime lease and pinned revision; full credential eligibility is checked by the next query in the transaction.
	// queryCredentialProjectionTrustedAuthority разрешает активного автора корневого запуска и область владельца по точной живой аренде и закреплённой ревизии; полная допустимость реквизитов проверяется следующим запросом транзакции.
	//go:embed sql/credential_projection_trusted_authority.sql
	queryCredentialProjectionTrustedAuthority string
	// queryCredentialProjectionResolveRuntime resolves provider and secret projection pins only for an eligible lease, actor, session/turn, assistant scope, attempt, revision and current account credential.
	// queryCredentialProjectionResolveRuntime разрешает закреплённые реквизиты провайдера и проекций секретов только для допустимых аренды, автора, сессии и хода, области помощника, попытки, ревизии и текущего реквизита аккаунта.
	//go:embed sql/credential_projection_resolve_runtime.sql
	queryCredentialProjectionResolveRuntime string
	// queryCredentialProjectionResolveRuntimeSecret matches an ACTIVE scoped runtime secret revision to exact Kubernetes name, key, UID, resource version and content digest.
	// queryCredentialProjectionResolveRuntimeSecret сопоставляет ACTIVE ревизию runtime-секрета нужной области с точными именем и ключом Kubernetes, UID, версией ресурса и дайджестом содержимого.
	//go:embed sql/credential_projection_resolve_runtime_secret.sql
	queryCredentialProjectionResolveRuntimeSecret string
	// queryCredentialProjectionResolveSTT resolves a bound enabled STT profile only at pinned configuration and API-key credential generation of an authorized provider account.
	// queryCredentialProjectionResolveSTT разрешает привязанный включённый профиль STT только при закреплённых конфигурации и поколении реквизита API-key авторизованного аккаунта провайдера.
	//go:embed sql/credential_projection_resolve_stt.sql
	queryCredentialProjectionResolveSTT string
)
