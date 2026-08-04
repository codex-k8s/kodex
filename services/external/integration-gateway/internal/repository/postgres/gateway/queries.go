package gateway

import _ "embed"

//go:embed sql/transaction__set_scope.sql
var sqlTransactionSetScope string

//go:embed sql/definition__insert.sql
var sqlDefinitionInsert string

//go:embed sql/connection__upsert.sql
var sqlConnectionUpsert string

//go:embed sql/grant__upsert.sql
var sqlGrantUpsert string

//go:embed sql/session__insert.sql
var sqlSessionInsert string

//go:embed sql/audit__insert.sql
var sqlAuditInsert string

//go:embed sql/tools__list.sql
var sqlToolsList string

//go:embed sql/session__touch.sql
var sqlSessionTouch string

//go:embed sql/session__release.sql
var sqlSessionRelease string

//go:embed sql/receipt__lock.sql
var sqlReceiptLock string

//go:embed sql/receipt__get.sql
var sqlReceiptGet string

//go:embed sql/invocation__insert.sql
var sqlInvocationInsert string

//go:embed sql/invocation__authority_lock.sql
var sqlInvocationAuthorityLock string

//go:embed sql/continuation__insert.sql
var sqlContinuationInsert string

//go:embed sql/continuation__schedule.sql
var sqlContinuationSchedule string

//go:embed sql/continuation__claim.sql
var sqlContinuationClaim string

//go:embed sql/continuation__complete.sql
var sqlContinuationComplete string

//go:embed sql/continuation__lock.sql
var sqlContinuationLock string

//go:embed sql/continuation__retry.sql
var sqlContinuationRetry string

//go:embed sql/next_continuation_scope__get.sql
var sqlNextContinuationScope string

//go:embed sql/approval__insert.sql
var sqlApprovalInsert string

//go:embed sql/receipt__insert.sql
var sqlReceiptInsert string

//go:embed sql/approval__lock.sql
var sqlApprovalLock string

//go:embed sql/approval__update.sql
var sqlApprovalUpdate string

//go:embed sql/invocation__cancel_lock.sql
var sqlInvocationCancelLock string

//go:embed sql/approval__cancel.sql
var sqlApprovalCancel string

//go:embed sql/invocation__cancel.sql
var sqlInvocationCancel string

//go:embed sql/next_execution_scope__get.sql
var sqlNextExecutionScope string

//go:embed sql/execution__claim.sql
var sqlExecutionClaim string

//go:embed sql/attempt__insert.sql
var sqlAttemptInsert string

//go:embed sql/invocation__mark_executing.sql
var sqlInvocationMarkExecuting string

//go:embed sql/execution__dispatch.sql
var sqlExecutionDispatch string

//go:embed sql/execution__attempt_lock.sql
var sqlExecutionAttemptLock string

//go:embed sql/execution_work_scope__delete.sql
var sqlExecutionWorkScopeDelete string

//go:embed sql/execution__lock.sql
var sqlExecutionLock string

//go:embed sql/attempt__complete.sql
var sqlAttemptComplete string

//go:embed sql/result__insert.sql
var sqlResultInsert string

//go:embed sql/invocation__complete.sql
var sqlInvocationComplete string

//go:embed sql/invocation__get.sql
var sqlInvocationGet string

//go:embed sql/session__close.sql
var sqlSessionClose string

//go:embed sql/lifecycle__expire.sql
var sqlLifecycleExpire string

//go:embed sql/readiness__check.sql
var sqlReadinessCheck string

//go:embed sql/connection__get.sql
var sqlConnectionGet string

//go:embed sql/connection__validate.sql
var sqlConnectionValidate string

//go:embed sql/next_lifecycle_scope__get.sql
var sqlNextLifecycleScope string

//go:embed sql/authority__revoke_stale.sql
var sqlAuthorityRevokeStale string
