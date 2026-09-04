package platform

import _ "embed"

var (
	//go:embed sql/credential_projection_resolve_runtime.sql
	queryCredentialProjectionResolveRuntime string
	//go:embed sql/credential_projection_resolve_runtime_secret.sql
	queryCredentialProjectionResolveRuntimeSecret string
	//go:embed sql/credential_projection_resolve_stt.sql
	queryCredentialProjectionResolveSTT string
)
