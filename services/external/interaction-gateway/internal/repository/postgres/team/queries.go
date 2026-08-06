package team

import _ "embed"

var (
	//go:embed sql/transaction__activate_scope.sql
	activateScopeSQL string
	//go:embed sql/team_readiness__check.sql
	readinessCheckSQL string
	//go:embed sql/team_readiness__probe_cursor.sql
	readinessProbeCursorSQL string
	//go:embed sql/team_catalog_cursor__resolve.sql
	catalogCursorResolveSQL string
	//go:embed sql/team_catalog_cursor__upsert.sql
	catalogCursorUpsertSQL string
	//go:embed sql/team_selector__upsert.sql
	selectorUpsertSQL string
	//go:embed sql/team_selector__resolve.sql
	selectorResolveSQL string
	//go:embed sql/team_operation__insert.sql
	operationInsertSQL string
	//go:embed sql/team_operation__lock.sql
	operationLockSQL string
	//go:embed sql/team_operation__reclaim.sql
	operationReclaimSQL string
	//go:embed sql/team_operation__mark_effect.sql
	operationMarkEffectSQL string
	//go:embed sql/team_operation__mark_ambiguous.sql
	operationMarkAmbiguousSQL string
	//go:embed sql/team_operation__mark_repair.sql
	operationMarkRepairSQL string
	//go:embed sql/team_provider_watermark__advance.sql
	providerWatermarkAdvanceSQL string
	//go:embed sql/team_operation__accept.sql
	operationAcceptSQL string
	//go:embed sql/team_work_scope__next.sql
	nextWorkScopeSQL string
	//go:embed sql/team_operation__claim_recovery.sql
	operationClaimRecoverySQL string
)
