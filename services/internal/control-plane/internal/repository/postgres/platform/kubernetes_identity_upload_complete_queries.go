package platform

import _ "embed"

// queryKubernetesSetupFactClassification preserves permanent acknowledged-outcome classification.
// queryKubernetesSetupFactClassification сохраняет permanent acknowledged-outcome classification.
//
//go:embed sql/kubernetes_setup__fact_classification.sql
var queryKubernetesSetupFactClassification string

// queryKubernetesSetupRejectionInsert appends a private terminal ACK witness without raw response bytes.
// queryKubernetesSetupRejectionInsert добавляет private terminal ACK witness без raw response bytes.
//
//go:embed sql/kubernetes_setup__rejection_insert.sql
var queryKubernetesSetupRejectionInsert string

// queryKubernetesSetupRejectionRead verifies immutable receipt replay without replacing witness pins.
// queryKubernetesSetupRejectionRead проверяет immutable receipt replay без замены witness pins.
//
//go:embed sql/kubernetes_setup__rejection_read.sql
var queryKubernetesSetupRejectionRead string

// queryKubernetesSetupAttemptLookup resolves a stored original attempt through its private fence.
// queryKubernetesSetupAttemptLookup разрешает stored original attempt через private fence.
//
//go:embed sql/kubernetes_setup__attempt_lookup.sql
var queryKubernetesSetupAttemptLookup string

// queryKubernetesSetupConnectionLockAny retains the owner lock even after lifecycle revocation.
// queryKubernetesSetupConnectionLockAny сохраняет owner lock также после lifecycle revocation.
//
//go:embed sql/kubernetes_setup__connection_lock_any.sql
var queryKubernetesSetupConnectionLockAny string

// queryKubernetesSetupPrivateLock locks retained setup history without reactivating its actor.
// queryKubernetesSetupPrivateLock блокирует retained setup history без восстановления actor.
//
//go:embed sql/kubernetes_setup__private_lock.sql
var queryKubernetesSetupPrivateLock string

// queryKubernetesSetupAttemptLock checks the exact fence/instance after Connection and setup locks.
// queryKubernetesSetupAttemptLock проверяет exact fence/instance после Connection и setup locks.
//
//go:embed sql/kubernetes_setup__attempt_lock.sql
var queryKubernetesSetupAttemptLock string

// queryKubernetesSetupOriginalNamespace reads the immutable storage boundary of the original intent.
// queryKubernetesSetupOriginalNamespace читает immutable storage boundary исходного intent.
//
//go:embed sql/kubernetes_setup__original_namespace.sql
var queryKubernetesSetupOriginalNamespace string

// queryKubernetesSetupMaterialInsert inserts one immutable locator without accepting replacement pins.
// queryKubernetesSetupMaterialInsert добавляет один immutable locator без replacement pins.
//
//go:embed sql/kubernetes_setup__material_insert.sql
var queryKubernetesSetupMaterialInsert string

// queryKubernetesSetupObservationInsert appends KNOWN or UNKNOWN without rewriting earlier evidence.
// queryKubernetesSetupObservationInsert добавляет KNOWN либо UNKNOWN без изменения прежнего evidence.
//
//go:embed sql/kubernetes_setup__observation_insert.sql
var queryKubernetesSetupObservationInsert string

// queryKubernetesSetupAttemptComplete closes only the exact active attempt, never a later generation.
// queryKubernetesSetupAttemptComplete закрывает только exact active attempt, не последующее generation.
//
//go:embed sql/kubernetes_setup__attempt_complete.sql
var queryKubernetesSetupAttemptComplete string

// queryKubernetesSetupTransition advances only an open setup while leaving terminal history immutable.
// queryKubernetesSetupTransition меняет только open setup, сохраняя immutable terminal history.
//
//go:embed sql/kubernetes_setup__transition.sql
var queryKubernetesSetupTransition string

// queryKubernetesSetupCloseAuthority atomically closes every remaining grant of an expired/revoked setup.
// queryKubernetesSetupCloseAuthority атомарно закрывает оставшиеся grants expired/revoked setup.
//
//go:embed sql/kubernetes_setup__close_authority.sql
var queryKubernetesSetupCloseAuthority string

// queryKubernetesSetupPrivateView returns safe receipt metadata even when original eligibility is revoked.
// It is reachable only after the original private attempt fence, never from a public alternate selector.
// queryKubernetesSetupPrivateView возвращает safe receipt metadata также после отзыва original eligibility.
// Доступен только после original private attempt fence, не через public alternate selector.
//
//go:embed sql/kubernetes_setup__private_view.sql
var queryKubernetesSetupPrivateView string
