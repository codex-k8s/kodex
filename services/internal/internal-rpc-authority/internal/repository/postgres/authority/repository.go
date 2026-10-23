package authority

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store binds a PostgreSQL pool, target workload and readiness reservation mode to fixed authority queries. SQL owns durable replay/snapshot effects; this struct is not a principal or authorization decision.
// Store связывает пул PostgreSQL, целевой workload и режим readiness reservation с фиксированными запросами authority. Устойчивые эффекты replay и снимков принадлежат SQL; структура не является principal или решением о доступе.
type Store struct {
	pool                     *pgxpool.Pool
	targetWorkloadID         string
	readinessReservationKind repository.ReservationKind
	queries                  querySet
}

// New rejects a nil pool, empty target and unsupported readiness mode, then validates embedded query headers. It does not open a connection, prove RLS permissions or activate a snapshot.
// New отклоняет nil-пул, пустую цель и неподдерживаемый режим readiness, затем проверяет заголовки встроенных запросов. Соединение не открывается, права RLS не доказываются и снимок не активируется.
func New(
	pool *pgxpool.Pool,
	targetWorkloadID string,
	readinessReservationKind repository.ReservationKind,
) (*Store, error) {
	if pool == nil || targetWorkloadID == "" ||
		readinessReservationKind != repository.ReservationAuthorityProof &&
			readinessReservationKind != repository.ReservationAuthorizationContext {
		return nil, errors.New("invalid authority store configuration")
	}
	queries, err := loadQueries()
	if err != nil {
		return nil, err
	}
	return &Store{
		pool:                     pool,
		targetWorkloadID:         targetWorkloadID,
		readinessReservationKind: readinessReservationKind,
		queries:                  queries,
	}, nil
}

// Reserve chooses proof SQL only for the proof kind and context SQL otherwise, mapping a false/no-row result to replay. The caller owns kind eligibility; this method does not independently validate every reservation field.
// Reserve выбирает proof SQL только для вида proof, иначе context SQL, преобразуя false или отсутствие строки в replay. Допустимость вида задаёт вызывающий; каждое поле reservation здесь независимо не проверяется.
func (store *Store) Reserve(
	ctx context.Context,
	reservation repository.Reservation,
) error {
	query := store.queries.contextReserve
	args := contextReservationArgs(reservation)
	if reservation.Kind == repository.ReservationAuthorityProof {
		query = store.queries.proofReserve
		args = proofReservationArgs(reservation)
	}
	var accepted bool
	err := store.pool.QueryRow(
		ctx,
		query,
		args,
	).Scan(&accepted)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !accepted {
		return repository.ErrReplay
	}
	if err != nil {
		return fmt.Errorf("reserve replay identifier: %w", err)
	}
	return nil
}

// ReserveContinuation executes one parent/child statement and distinguishes an unaccepted parent from a repeated child reservation. It does not mint either identity or create a new parent acceptance.
// ReserveContinuation выполняет один statement родителя и ребёнка, различая непринятого родителя и повтор reservation ребёнка. Идентичности не выпускаются и новое принятие родителя не создаётся.
func (store *Store) ReserveContinuation(
	ctx context.Context,
	parent repository.Reservation,
	child repository.Reservation,
) error {
	args := continuationReservationArgs(parent, child)
	var parentAccepted, childAccepted bool
	if err := store.pool.QueryRow(ctx, store.queries.continuationReserve, args).Scan(&parentAccepted, &childAccepted); err != nil {
		return fmt.Errorf("reserve authorization continuation: %w", err)
	}
	if !parentAccepted {
		return repository.ErrParentNotAccepted
	}
	if !childAccepted {
		return repository.ErrReplay
	}
	return nil
}

// ActivateSnapshot passes exact target, revision/history and receipt arguments to the database activation boundary and maps a false result to snapshot rollback. No local watermark or signature verification substitutes for that statement.
// ActivateSnapshot передаёт точные цель, ревизии и историю и квитанцию границе активации БД, преобразуя false в rollback снимка. Локальная отметка или проверка подписи не заменяет этот statement.
func (store *Store) ActivateSnapshot(
	ctx context.Context,
	state repository.SnapshotState,
) error {
	var accepted bool
	if err := store.pool.QueryRow(
		ctx,
		store.queries.verifierActivateSnapshot,
		snapshotArgs(store.targetWorkloadID, state),
	).Scan(&accepted); err != nil {
		return fmt.Errorf("activate served snapshot: %w", err)
	}
	if !accepted {
		return repository.ErrSnapshotRollback
	}
	return nil
}

// AcceptVerification combines snapshot and context arguments plus caller/signer bindings in one statement. It reports snapshot refusal before replay refusal; partial booleans are not a successful context acceptance.
// AcceptVerification объединяет аргументы снимка и контекста и привязки caller и подписанта в одном statement. Отказ снимка возвращается раньше отказа replay; частичные bool не означают успешное принятие контекста.
func (store *Store) AcceptVerification(
	ctx context.Context,
	state repository.SnapshotState,
	reservation repository.Reservation,
) error {
	args := snapshotArgs(store.targetWorkloadID, state)
	for key, value := range contextReservationArgs(reservation) {
		args[key] = value
	}
	args["caller_workload_id"] = reservation.CallerWorkloadID
	args["context_signer_generation"] = reservation.SignerGeneration
	var snapshotAccepted bool
	var replayAccepted bool
	if err := store.pool.QueryRow(
		ctx,
		store.queries.verifierAcceptContext,
		args,
	).Scan(&snapshotAccepted, &replayAccepted); err != nil {
		return fmt.Errorf("accept verified context: %w", err)
	}
	if !snapshotAccepted {
		return repository.ErrSnapshotRollback
	}
	if !replayAccepted {
		return repository.ErrReplay
	}
	return nil
}

// Ready pings PostgreSQL, checks the exact served snapshot and attempts a mode-specific replay write in a transaction that must explicitly roll back for success. Deferred rollback on other exits is best-effort under the same context; this method does not close the pool.
// Ready проверяет связь с PostgreSQL и точный обслуживаемый снимок, затем пробует запись replay нужного вида в транзакции, требующей явного успешного rollback. Отложенный rollback на остальных выходах выполняется по возможности с тем же контекстом; пул не закрывается.
func (store *Store) Ready(
	ctx context.Context,
	expected repository.SnapshotState,
) error {
	if err := store.pool.Ping(ctx); err != nil {
		return fmt.Errorf("check PostgreSQL connectivity: %w", err)
	}
	var ready bool
	if err := store.pool.QueryRow(
		ctx,
		store.queries.verifierReadiness,
		snapshotReadinessArgs(store.targetWorkloadID, expected),
	).Scan(&ready); err != nil {
		return fmt.Errorf("check served snapshot: %w", err)
	}
	if !ready {
		return repository.ErrNotReady
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin replay readiness transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()
	probeID, err := randomUUID()
	if err != nil {
		return fmt.Errorf("create replay readiness identifier: %w", err)
	}
	query, args, err := store.readinessReservation(probeID, time.Now().UTC())
	if err != nil {
		return err
	}
	var accepted bool
	if err := transaction.QueryRow(
		ctx,
		query,
		args,
	).Scan(&accepted); err != nil {
		return fmt.Errorf("check replay write path: %w", err)
	}
	if !accepted {
		return repository.ErrNotReady
	}
	if err := transaction.Rollback(ctx); err != nil {
		return fmt.Errorf("rollback replay readiness transaction: %w", err)
	}
	return nil
}

// readinessReservation builds a one-minute synthetic probe from the supplied ID/time and selects the exact proof or context query by the store's registered mode. It performs no SQL and rejects unknown modes.
// readinessReservation строит минутную синтетическую пробу по переданным ID и времени и выбирает точный proof либо context query по зарегистрированному режиму store. SQL не выполняется, неизвестные режимы отклоняются.
func (store *Store) readinessReservation(
	probeID string,
	now time.Time,
) (string, pgx.StrictNamedArgs, error) {
	reservation := repository.Reservation{
		Kind:        store.readinessReservationKind,
		ScopeID:     store.targetWorkloadID,
		OperationID: "readiness-" + probeID,
		Issuer:      "internal-rpc-authority-readiness",
		Revision:    1,
		JTI:         probeID,
		Digest:      strings.Repeat("0", 64),
		ExpiresAt:   now.Add(time.Minute),
	}
	switch store.readinessReservationKind {
	case repository.ReservationAuthorityProof:
		return store.queries.proofReserve, proofReservationArgs(reservation), nil
	case repository.ReservationAuthorizationContext:
		return store.queries.contextReserve, contextReservationArgs(reservation), nil
	default:
		return "", nil, errors.New("readiness reservation kind is not registered")
	}
}

// DeleteExpired selects a registered cleanup kind; proof cleanup first invokes caller-scoped issued-binding cleanup, then deletes expired proof rows. Those two statements are not wrapped here in one transaction, so a later failure does not undo the earlier cleanup.
// DeleteExpired выбирает зарегистрированный вид очистки; для proof сначала вызывается очистка issued bindings в области caller, затем удаляются просроченные proof rows. Два statement здесь не обёрнуты общей транзакцией, поэтому поздняя ошибка не отменяет раннюю очистку.
func (store *Store) DeleteExpired(
	ctx context.Context,
	kind repository.ReservationKind,
	deleteBefore time.Time,
) error {
	var query string
	switch kind {
	case repository.ReservationAuthorityProof:
		var removed int64
		if err := store.pool.QueryRow(ctx, store.queries.contextBindingsCleanup, pgx.StrictNamedArgs{"caller_workload_id": store.targetWorkloadID}).Scan(&removed); err != nil {
			return fmt.Errorf("cleanup issued context bindings: %w", err)
		}
		query = store.queries.proofReservationsDeleteExpired
	case repository.ReservationAuthorizationContext:
		query = store.queries.contextReservationsDeleteExpired
	default:
		return errors.New("replay reservation kind is not registered")
	}
	if _, err := store.pool.Exec(
		ctx,
		query,
		pgx.StrictNamedArgs{"delete_before": deleteBefore},
	); err != nil {
		return fmt.Errorf("delete expired replay reservations: %w", err)
	}
	return nil
}

// Close closes the injected PostgreSQL pool; lifecycle owners must stop consumers before invoking it and must not treat this as closing only a borrowed connection.
// Close закрывает внедрённый пул PostgreSQL; владелец жизненного цикла сначала останавливает потребителей и не трактует метод как закрытие одного заимствованного соединения.
func (store *Store) Close() {
	store.pool.Close()
}

// proofReservationArgs maps exact caller/operation/issuer/revision/JTI/digest bindings and UTC expiry into the proof statement's strict named arguments without validating eligibility or executing SQL.
// proofReservationArgs преобразует точные привязки caller, operation, issuer, revision, JTI, digest и срок UTC в строгие именованные аргументы proof statement без проверки допустимости и исполнения SQL.
func proofReservationArgs(value repository.Reservation) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"caller_workload_id":      value.ScopeID,
		"operation_id":            value.OperationID,
		"authority_proof_issuer":  value.Issuer,
		"proof_revision":          value.Revision,
		"jti":                     value.JTI,
		"canonical_digest_sha256": value.Digest,
		"expires_at":              value.ExpiresAt.UTC(),
	}
}

// continuationReservationArgs keeps parent target/JTI/digest separate from child caller/JTI/digest/UTC expiry, preserving the two lineage roles instead of inferring a parent from the child.
// continuationReservationArgs разделяет target, JTI и digest родителя и caller, JTI, digest и срок UTC ребёнка, сохраняя две роли происхождения вместо вывода родителя из ребёнка.
func continuationReservationArgs(parent, child repository.Reservation) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"parent_target_workload_id":      parent.ScopeID,
		"parent_jti":                     parent.JTI,
		"parent_canonical_digest_sha256": parent.Digest,
		"caller_workload_id":             child.ScopeID,
		"jti":                            child.JTI,
		"canonical_digest_sha256":        child.Digest,
		"expires_at":                     child.ExpiresAt.UTC(),
	}
}

// contextReservationArgs maps one target-scoped JTI, digest and UTC expiry to context reservation SQL; it neither derives target ownership nor alters the input value.
// contextReservationArgs преобразует JTI в области target, дайджест и срок UTC в аргументы context reservation SQL, не выводя владение целью и не меняя входное значение.
func contextReservationArgs(value repository.Reservation) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"target_workload_id":      value.ScopeID,
		"jti":                     value.JTI,
		"canonical_digest_sha256": value.Digest,
		"expires_at":              value.ExpiresAt.UTC(),
	}
}

// snapshotArgs creates fresh parallel history arrays in original order and maps snapshot/receipt/predecessor pins for one target. Revision-to-int64 conversion is unchanged and performs no separate range or signature validation.
// snapshotArgs создаёт новые параллельные массивы истории в исходном порядке и преобразует привязки снимка, квитанции и предшественника для одной цели. Преобразование ревизии в int64 не меняется и отдельно диапазон или подпись не проверяет.
func snapshotArgs(
	targetWorkloadID string,
	value repository.SnapshotState,
) pgx.StrictNamedArgs {
	historyRevisions := make([]int64, 0, len(value.History))
	historyDigests := make([]string, 0, len(value.History))
	for _, entry := range value.History {
		historyRevisions = append(historyRevisions, int64(entry.Revision))
		historyDigests = append(historyDigests, entry.DigestSHA256)
	}
	return pgx.StrictNamedArgs{
		"target_workload_id":        targetWorkloadID,
		"source_revision":           value.SourceRevision,
		"source_digest_sha256":      value.SourceDigestSHA256,
		"predecessor_revision":      value.PredecessorRevision,
		"predecessor_digest_sha256": value.PredecessorDigestSHA256,
		"key_set_revision":          value.KeySetRevision,
		"policy_revision":           value.PolicyRevision,
		"signer_generation":         value.SignerGeneration,
		"attestation_receipt_id":    value.AttestationReceiptID,
		"history_revisions":         historyRevisions,
		"history_digests":           historyDigests,
	}
}

// snapshotReadinessArgs supplies the exact target/source/key-set/policy/signer fields without a local receipt parameter, letting SQL validate the workload's persisted receipt.
// snapshotReadinessArgs передаёт точные target, source, key-set, policy и signer без параметра локальной квитанции, позволяя SQL проверить сохранённую квитанцию workload.
func snapshotReadinessArgs(
	targetWorkloadID string,
	value repository.SnapshotState,
) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"target_workload_id":   targetWorkloadID,
		"source_revision":      value.SourceRevision,
		"source_digest_sha256": value.SourceDigestSHA256,
		"key_set_revision":     value.KeySetRevision,
		"policy_revision":      value.PolicyRevision,
		"signer_generation":    value.SignerGeneration,
	}
}

// randomUUID obtains the readiness probe's fresh version-four identifier;
// the caller owns its transaction and rollback. Generating the ID does not
// perform a database write or prove that replay storage is writable.
// randomUUID получает свежий идентификатор версии четыре для пробы готовности;
// транзакцией и откатом владеет вызывающий. Генерация ID не выполняет запись
// в БД и не доказывает доступность записи replay storage.
func randomUUID() (string, error) {
	return value.NewAuthorityUUID()
}

// Freshness reads the shared workload receipt, database observation time and computed deadline. Missing rows, nil deadlines or nonfuture deadlines become NotReady; it returns no local-clock substitute or partial freshness on error.
// Freshness читает общую квитанцию workload, время наблюдения БД и вычисленный срок. Отсутствие строки, nil-срок или срок не позже наблюдения дают NotReady; локальные часы и частичная свежесть при ошибке не выдаются.
func (store *Store) Freshness(ctx context.Context, state repository.SnapshotState) (repository.SnapshotFreshness, error) {
	var result repository.SnapshotFreshness
	var deadline *time.Time
	err := store.pool.QueryRow(ctx, store.queries.verifierFreshness,
		snapshotReadinessArgs(store.targetWorkloadID, state)).Scan(&result.ReceiptID, &deadline, &result.ObservedAt)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && deadline == nil {
		return repository.SnapshotFreshness{}, repository.ErrNotReady
	}
	if err != nil {
		return repository.SnapshotFreshness{}, fmt.Errorf("read snapshot freshness: %w", err)
	}
	result.ValidUntil = *deadline
	if !result.ObservedAt.Before(result.ValidUntil) {
		return repository.SnapshotFreshness{}, repository.ErrNotReady
	}
	return result, nil
}

// RegisterIssuedContext attempts registration once and, after an error, reads only that same exact binding under the original context. A matching readback resolves uncertainty; otherwise the original failure is wrapped without a new JTI, budget or mutation retry.
// RegisterIssuedContext однократно пытается зарегистрировать контекст и после ошибки читает только ту же точную привязку под исходным контекстом. Совпавший readback разрешает неопределённость; иначе оборачивается исходная ошибка без нового JTI, бюджета и повтора изменения.
func (store *Store) RegisterIssuedContext(ctx context.Context, state repository.SnapshotState, binding repository.IssuedContextBinding) error {
	var parent *string
	if binding.ParentJTI != "" {
		parent = &binding.ParentJTI
	}
	args := pgx.StrictNamedArgs{
		"jti": binding.JTI, "canonical_digest_sha256": binding.Digest, "caller_workload_id": binding.CallerWorkloadID,
		"target_workload_id": binding.TargetWorkloadID, "source_revision": state.SourceRevision, "source_digest_sha256": state.SourceDigestSHA256,
		"key_set_revision": state.KeySetRevision, "policy_revision": state.PolicyRevision, "signer_generation": state.SignerGeneration,
		"issued_at": binding.IssuedAt, "expires_at": binding.ExpiresAt, "parent_jti": parent, "parent_digest_sha256": binding.ParentDigest,
	}
	var accepted bool
	if err := store.pool.QueryRow(ctx, store.queries.contextRegisterIssued, args).Scan(&accepted); err != nil {
		// После неопределённого результата читается только тот же exact binding.
		// Истёкший/cancelled context не получает нового бюджета или нового JTI.
		if readErr := store.pool.QueryRow(ctx, store.queries.contextIssuedReadback, args).Scan(&accepted); readErr != nil || !accepted {
			return fmt.Errorf("register issued context outcome uncertain: %w", err)
		}
	}
	if !accepted {
		return repository.ErrNotReady
	}
	return nil
}
