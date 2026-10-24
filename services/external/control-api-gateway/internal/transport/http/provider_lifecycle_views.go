package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// validProviderAccountLifecycle checks deletion/verification projections and their relationship to
// the account lifecycle/version. It does not issue cleanup work, revoke credentials or prove
// provider reachability.
// validProviderAccountLifecycle проверяет проекции удаления и проверки и их связь с lifecycle и
// версией аккаунта. Задачи очистки не выдаются, credentials не отзываются и достижимость
// провайдера не доказывается.
func validProviderAccountLifecycle(a *cp.ProviderAccount) bool {
	if a == nil || !validProviderDeletion(a.Deletion) || !validProviderVerification(a.Verification) {
		return false
	}
	deleting := a.State == cp.ProviderAccountState_PROVIDER_ACCOUNT_STATE_DELETING
	deleted := a.State == cp.ProviderAccountState_PROVIDER_ACCOUNT_STATE_DELETED
	if (deleting || deleted) != (a.Deletion != nil) {
		return false
	}
	if a.Deletion != nil && deleted != (a.Deletion.State == cp.ProviderAccountDeletionState_PROVIDER_ACCOUNT_DELETION_STATE_DELETED) {
		return false
	}
	return a.Verification == nil || a.Verification.AccountVersion <= a.Version
}

// validProviderDeletion allows no deletion intent or validates one exact state/reason pair with
// six unique blocker kinds, bounded counts and coherent completion time. A deleted intent requires
// zero remaining cleanup and blockers.
// validProviderDeletion допускает отсутствие намерения удаления либо проверяет точную пару
// state/reason, шесть уникальных видов блокеров, ограниченные счётчики и согласованное время
// завершения. Завершённое удаление требует нулевых остатков cleanup и блокеров.
func validProviderDeletion(d *cp.ProviderAccountDeletion) bool {
	if d == nil {
		return true
	}
	if !effectiveCapabilityRef(d.Ref) || !validManagedVersion(d.Version) || d.RequestedAt == nil || d.RequestedAt.CheckValid() != nil ||
		len(d.Blockers) != 6 || d.PendingCleanup < 0 || d.PendingCleanup > maximumSafeJSONInteger {
		return false
	}
	reason := map[cp.ProviderAccountDeletionState]string{
		cp.ProviderAccountDeletionState_PROVIDER_ACCOUNT_DELETION_STATE_PENDING_BLOCKERS: "WAITING_FOR_DEPENDENCIES",
		cp.ProviderAccountDeletionState_PROVIDER_ACCOUNT_DELETION_STATE_CLEANUP_QUEUED:   "CREDENTIAL_CLEANUP_PENDING",
		cp.ProviderAccountDeletionState_PROVIDER_ACCOUNT_DELETION_STATE_CLEANING:         "CREDENTIAL_CLEANUP_IN_PROGRESS",
		cp.ProviderAccountDeletionState_PROVIDER_ACCOUNT_DELETION_STATE_FAILED:           "CREDENTIAL_CLEANUP_FAILED",
		cp.ProviderAccountDeletionState_PROVIDER_ACCOUNT_DELETION_STATE_DELETED:          "ACCOUNT_DELETED",
	}[d.State]
	if reason == "" || d.SafeReason != reason {
		return false
	}
	deleted := d.State == cp.ProviderAccountDeletionState_PROVIDER_ACCOUNT_DELETION_STATE_DELETED
	if deleted && d.PendingCleanup != 0 {
		return false
	}
	if deleted != (d.CompletedAt != nil) || d.CompletedAt != nil && (d.CompletedAt.CheckValid() != nil || d.CompletedAt.AsTime().Before(d.RequestedAt.AsTime())) {
		return false
	}
	seen := map[cp.ProviderAccountBlockerKind]bool{}
	for _, count := range d.Blockers {
		if count == nil || !validProviderBlockerKind(count.Kind) || seen[count.Kind] || count.Total < 0 || count.Total > maximumSafeJSONInteger || deleted && count.Total != 0 {
			return false
		}
		seen[count.Kind] = true
	}
	return true
}

// validProviderVerification allows an absent verification or checks credentialed-catalog scope,
// version pins, state/reason and requested/completed times. The receipt describes an observation;
// this validator never contacts the provider.
// validProviderVerification допускает отсутствие проверки либо сверяет область credentialed
// catalog, pins версий, state/reason и время запроса/завершения. Receipt описывает наблюдение;
// валидатор не обращается к провайдеру.
func validProviderVerification(v *cp.ProviderAccountVerification) bool {
	if v == nil {
		return true
	}
	if !effectiveCapabilityRef(v.Ref) || !validManagedVersion(v.AccountVersion) || !validManagedVersion(v.CredentialRevision) ||
		v.Scope != cp.ProviderAccountVerificationScope_PROVIDER_ACCOUNT_VERIFICATION_SCOPE_CREDENTIALED_CATALOG_REACHABILITY || v.RequestedAt == nil || v.RequestedAt.CheckValid() != nil {
		return false
	}
	reason := map[cp.ProviderAccountVerificationState]string{
		cp.ProviderAccountVerificationState_PROVIDER_ACCOUNT_VERIFICATION_STATE_PENDING:  "VERIFICATION_PENDING",
		cp.ProviderAccountVerificationState_PROVIDER_ACCOUNT_VERIFICATION_STATE_VERIFIED: "CREDENTIAL_REACHABILITY_VERIFIED",
		cp.ProviderAccountVerificationState_PROVIDER_ACCOUNT_VERIFICATION_STATE_FAILED:   "CREDENTIAL_VERIFICATION_FAILED",
		cp.ProviderAccountVerificationState_PROVIDER_ACCOUNT_VERIFICATION_STATE_STALE:    "VERIFICATION_SOURCE_CHANGED",
	}[v.State]
	return reason != "" && v.SafeReason == reason && (v.State != cp.ProviderAccountVerificationState_PROVIDER_ACCOUNT_VERIFICATION_STATE_PENDING) == (v.CompletedAt != nil) &&
		(v.CompletedAt == nil || v.CompletedAt.CheckValid() == nil && !v.CompletedAt.AsTime().Before(v.RequestedAt.AsTime()))
}
