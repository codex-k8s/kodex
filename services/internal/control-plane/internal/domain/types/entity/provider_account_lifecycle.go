package entity

import "time"

// ProviderAccountBlockerCount summarizes how many account lifecycle blockers belong to one kind. The
// count is diagnostic and does not identify or authorize cancellation of individual work.
// ProviderAccountBlockerCount суммирует количество препятствий жизненного цикла аккаунта одного вида.
// Счётчик является диагностикой и не определяет и не разрешает отмену отдельной работы.
type ProviderAccountBlockerCount struct {
	Kind  string
	Total int64
}

// ProviderAccountDeletion presents deletion intent state, concurrency version, pending cleanup,
// blocker counts, and request and completion times. A requested or logically completed deletion must
// not be confused with confirmation of every external cleanup effect.
// ProviderAccountDeletion представляет состояние намерения удаления, версию конкуренции, ожидающую
// очистку, количества препятствий и время запроса и завершения. Запрошенное или логически завершённое
// удаление не равно подтверждению каждого внешнего эффекта очистки.
type ProviderAccountDeletion struct {
	Ref, State, SafeReason  string
	Version, PendingCleanup int64
	Blockers                []ProviderAccountBlockerCount
	RequestedAt             time.Time
	CompletedAt             *time.Time
}

// ProviderAccountVerification records a verification state and scope against exact account and
// credential revisions with request and completion times. It does not replace current credential,
// provider-health, or workload eligibility checks.
// ProviderAccountVerification фиксирует состояние и область проверки относительно точных ревизий
// аккаунта и реквизитов с временем запроса и завершения. Это не заменяет текущие проверки реквизитов,
// состояния провайдера или допустимости нагрузки.
type ProviderAccountVerification struct {
	Ref, State, Scope, SafeReason      string
	AccountVersion, CredentialRevision int64
	RequestedAt                        time.Time
	CompletedAt                        *time.Time
}

// ProviderAccountBlocker describes one versioned work item blocking an account transition, including
// its project and cancellation availability. CanCancel is observed eligibility, not permission to
// cancel work outside the caller's scope.
// ProviderAccountBlocker описывает версионированную работу, препятствующую переходу аккаунта, включая
// проект и возможность отмены. CanCancel является наблюдаемой допустимостью, а не правом отменить
// работу за пределами области вызывающего субъекта.
type ProviderAccountBlocker struct {
	Kind, Ref, Name, ProjectRef string
	Version                     int64
	CanCancel                   bool
}

// ProviderAccountBlockerPage returns visible blocker items, total and hidden counts, account and
// deletion-intent versions, context digest, and pagination. HiddenCount must not be mistaken for an
// empty blocker set or used to disclose hidden work identities.
// ProviderAccountBlockerPage возвращает видимые препятствия, общее и скрытое количество, версии
// аккаунта и намерения удаления, дайджест контекста и пагинацию. HiddenCount не означает пустой набор
// препятствий и не используется для раскрытия личностей скрытой работы.
type ProviderAccountBlockerPage struct {
	Items                                                     []ProviderAccountBlocker
	Total, HiddenCount, AccountVersion, DeletionIntentVersion int64
	NextPageToken, ContextDigest                              string
}

// ProviderQueuedWorkCancellationResult reports the outcome for one selected queued run. It preserves
// per-run results instead of implying that cancellation succeeded for every requested item.
// ProviderQueuedWorkCancellationResult сообщает исход для одного выбранного запуска в очереди.
// Структура сохраняет отдельные результаты, а не подразумевает успех отмены каждого запрошенного
// элемента.
type ProviderQueuedWorkCancellationResult struct {
	RunRef, Outcome string
}

// ProviderQueuedWorkCancellation returns the current account projection with individual queued-work
// cancellation outcomes. The account snapshot and result list are not authority to cancel additional
// runs.
// ProviderQueuedWorkCancellation возвращает текущее представление аккаунта с отдельными исходами
// отмены работы в очереди. Снимок аккаунта и список результатов не разрешают отмену дополнительных
// запусков.
type ProviderQueuedWorkCancellation struct {
	Account ProviderAccount
	Results []ProviderQueuedWorkCancellationResult
}
