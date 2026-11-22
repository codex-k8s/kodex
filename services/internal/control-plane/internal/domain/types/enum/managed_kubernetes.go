package enum

// ManagedKubernetesEffectOutcome records a proven or explicitly uncertain external observation.
// ManagedKubernetesEffectOutcome хранит подтверждённый либо явно неопределённый external observation.
type ManagedKubernetesEffectOutcome string

const (
	// KubernetesEffectSucceeded requires the matching safe result and exact effect attempt receipt.
	// KubernetesEffectSucceeded требует matching safe result и exact effect attempt receipt.
	KubernetesEffectSucceeded ManagedKubernetesEffectOutcome = "SUCCEEDED"
	// KubernetesEffectFailed records a proven failure without implying rollback of partial effects.
	// KubernetesEffectFailed хранит proven failure без предположения о rollback partial effects.
	KubernetesEffectFailed ManagedKubernetesEffectOutcome = "FAILED"
	// KubernetesEffectUnknown permanently forbids automatic replay of the uncertain attempt.
	// KubernetesEffectUnknown запрещает automatic replay неопределённой attempt.
	KubernetesEffectUnknown ManagedKubernetesEffectOutcome = "UNKNOWN"
)

// ManagedKubernetesStreamState belongs to the owner ledger, not a caller-controlled task state.
// ManagedKubernetesStreamState принадлежит owner ledger, не caller-controlled task state.
type ManagedKubernetesStreamState string

const (
	// KubernetesStreamPrepared reserves an immutable stream before the first external handshake.
	// KubernetesStreamPrepared резервирует immutable stream до первого external handshake.
	KubernetesStreamPrepared ManagedKubernetesStreamState = "PREPARED"
	// KubernetesStreamActive admits bounded frames only while its current owner graph remains valid.
	// KubernetesStreamActive допускает bounded frames только при valid current owner graph.
	KubernetesStreamActive ManagedKubernetesStreamState = "ACTIVE"
	// KubernetesStreamCompleted records a proven terminal stream without proving all descendant processes stopped.
	// KubernetesStreamCompleted фиксирует proven terminal stream, не доказывая остановку всех descendant processes.
	KubernetesStreamCompleted ManagedKubernetesStreamState = "COMPLETED"
	// KubernetesStreamCanceled closes an owner-canceled stream and all further frame credit.
	// KubernetesStreamCanceled закрывает owner-canceled stream и последующий frame credit.
	KubernetesStreamCanceled ManagedKubernetesStreamState = "CANCELED"
	// KubernetesStreamRevoked closes a stream whose grant or protected dependency was revoked.
	// KubernetesStreamRevoked закрывает stream после отзыва grant либо protected dependency.
	KubernetesStreamRevoked ManagedKubernetesStreamState = "REVOKED"
	// KubernetesStreamExpired closes an elapsed lease without refreshing the original execution.
	// KubernetesStreamExpired закрывает elapsed lease без refresh исходного execution.
	KubernetesStreamExpired ManagedKubernetesStreamState = "EXPIRED"
	// KubernetesStreamLost preserves an uncertain remote outcome after broker or receipt loss.
	// KubernetesStreamLost сохраняет uncertain remote outcome после потери broker либо receipt.
	KubernetesStreamLost ManagedKubernetesStreamState = "LOST"
)

// Terminal recognizes only registered terminal values; unknown input is never a terminal proof.
// Terminal распознаёт только registered terminal values; unknown input не является terminal proof.
func (state ManagedKubernetesStreamState) Terminal() bool {
	switch state {
	case KubernetesStreamCompleted, KubernetesStreamCanceled, KubernetesStreamRevoked, KubernetesStreamExpired, KubernetesStreamLost:
		return true
	default:
		return false
	}
}

// ManagedKubernetesFrameDirection separates remote input credit from output delivery receipts.
// ManagedKubernetesFrameDirection разделяет remote input credit и output delivery receipts.
type ManagedKubernetesFrameDirection string

const (
	// KubernetesFrameInput permits one separately admitted write to the selected remote stream.
	// KubernetesFrameInput допускает одну отдельно разрешённую write в выбранный remote stream.
	KubernetesFrameInput ManagedKubernetesFrameDirection = "INPUT"
	// KubernetesFrameOutput tracks bounded safe output without storing its payload in CP.
	// KubernetesFrameOutput отслеживает bounded safe output без хранения payload в CP.
	KubernetesFrameOutput ManagedKubernetesFrameDirection = "OUTPUT"
)

// ManagedKubernetesFrameDisposition distinguishes first credit, proven ACK and uncertain in-flight work.
// ManagedKubernetesFrameDisposition различает first credit, proven ACK и uncertain in-flight work.
type ManagedKubernetesFrameDisposition string

const (
	// KubernetesFrameSend admits the first INPUT attempt or a bounded fresh delivery of identical OUTPUT.
	// KubernetesFrameSend допускает первую INPUT attempt либо bounded fresh доставку идентичного OUTPUT.
	KubernetesFrameSend ManagedKubernetesFrameDisposition = "SEND"
	// KubernetesFrameAcknowledged returns a stored receipt without repeating the external write.
	// KubernetesFrameAcknowledged возвращает stored receipt без повторной external write.
	KubernetesFrameAcknowledged ManagedKubernetesFrameDisposition = "ACKNOWLEDGED"
	// KubernetesFrameUnknown forbids retransmission after an uncertain frame attempt.
	// KubernetesFrameUnknown запрещает retransmission после uncertain frame attempt.
	KubernetesFrameUnknown ManagedKubernetesFrameDisposition = "UNKNOWN"
	// KubernetesFrameClosed records a captured partial PF candidate with no new sequence, credit or ACK.
	// KubernetesFrameClosed фиксирует captured partial PF candidate без новой sequence, credit либо ACK.
	KubernetesFrameClosed ManagedKubernetesFrameDisposition = "CLOSED"
)

// ManagedKubernetesInvocationState is a readback of the existing owner lifecycle, not a caller decision.
// ManagedKubernetesInvocationState отражает existing owner lifecycle, не caller decision.
type ManagedKubernetesInvocationState string

const (
	// KubernetesInvocationRunning preserves an original still-live attempt without granting another one.
	// KubernetesInvocationRunning сохраняет original still-live attempt без выдачи другой.
	KubernetesInvocationRunning ManagedKubernetesInvocationState = "RUNNING"
	// KubernetesInvocationSucceeded requires the owner's complete accepted child aggregate.
	// KubernetesInvocationSucceeded требует complete accepted child aggregate владельца.
	KubernetesInvocationSucceeded ManagedKubernetesInvocationState = "SUCCEEDED"
	// KubernetesInvocationFailed records a closed proven failure or explicit no-effect finish.
	// KubernetesInvocationFailed хранит closed proven failure либо explicit no-effect finish.
	KubernetesInvocationFailed ManagedKubernetesInvocationState = "FAILED"
	// KubernetesInvocationUnknown preserves unresolved external effects without retry credit.
	// KubernetesInvocationUnknown сохраняет unresolved external effects без retry credit.
	KubernetesInvocationUnknown ManagedKubernetesInvocationState = "UNKNOWN_OUTCOME"
	// KubernetesInvocationCancelled records owner cancellation without proving remote rollback or stop.
	// KubernetesInvocationCancelled хранит owner cancellation, не доказывая remote rollback либо stop.
	KubernetesInvocationCancelled ManagedKubernetesInvocationState = "CANCELLED"
)

// Known recognizes only the existing finite invocation readback states.
// Known распознаёт лишь existing finite invocation readback states.
func (state ManagedKubernetesInvocationState) Known() bool {
	switch state {
	case KubernetesInvocationRunning, KubernetesInvocationSucceeded, KubernetesInvocationFailed, KubernetesInvocationUnknown, KubernetesInvocationCancelled:
		return true
	default:
		return false
	}
}
