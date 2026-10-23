package readback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/postgres/readbackrow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// consumeSerializationRetryDelays fixes five attempt starts at zero,25,75,150,300 ms for typed PostgreSQL serialization failures only. It is not a generic driver retry policy or an extra transaction deadline.
// consumeSerializationRetryDelays закрепляет пять начал попыток с задержками ноль, 25, 75, 150, 300 мс только для типизированных ошибок сериализации PostgreSQL. Это не общая политика повторов драйвера и не дополнительный дедлайн транзакции.
var consumeSerializationRetryDelays = [...]time.Duration{
	0,
	25 * time.Millisecond,
	75 * time.Millisecond,
	150 * time.Millisecond,
	300 * time.Millisecond,
}

// Repository owns durable challenge, receipt and served-trust operations over an injected PostgreSQL pool; peer scope and state-transition enforcement are carried through its fixed SQL boundary.
// Repository выполняет устойчивые операции challenge, квитанций и обслуживаемого доверия через внедрённый пул PostgreSQL; область peer и ограничения переходов передаются фиксированной границе SQL.
type Repository struct {
	pool *pgxpool.Pool
}

// New requires a nonnil pool and checks the embedded readback query names before constructing the adapter. It does not ping the database or activate trust.
// New требует ненулевой пул и проверяет имена встроенных readback queries до создания адаптера. Ping БД и активация доверия не выполняются.
func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("readback PostgreSQL pool is nil")
	}
	if err := validateQueries(); err != nil {
		return nil, err
	}
	return &Repository{pool: pool}, nil
}

// ResolveReadbackIntent queries the exact intent/peer boundary and maps no rows to NotFound. Row decoding does not independently authenticate credentials or verify the public JWK.
// ResolveReadbackIntent обращается к точной границе intent и peer, преобразуя отсутствие строк в NotFound. Декодирование строки отдельно не аутентифицирует credentials и не проверяет публичный JWK.
func (repository *Repository) ResolveReadbackIntent(
	ctx context.Context,
	intentID string,
	peerSPIFFEID string,
) (model.ReadbackIntent, error) {
	intent, err := scanIntent(repository.pool.QueryRow(
		ctx,
		resolveIntentSQL,
		pgx.StrictNamedArgs{
			"intent_id":      intentID,
			"peer_spiffe_id": peerSPIFFEID,
		},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackIntent{}, domainrepository.ErrNotFound
	}
	if err != nil {
		return model.ReadbackIntent{}, fmt.Errorf("resolve readback intent: %w", err)
	}
	return intent, nil
}

// IssueReadbackChallenge invokes durable challenge issuance with semantic idempotency pins, then separately loads the returned ID under the peer boundary. A later load error does not roll back an already issued challenge; no-row issuance is an idempotency conflict.
// IssueReadbackChallenge вызывает устойчивую выдачу challenge с семантическими привязками идемпотентности, затем отдельно загружает возвращённый ID в области peer. Поздняя ошибка чтения не откатывает выданный challenge; отсутствие строки выдачи означает конфликт идемпотентности.
func (repository *Repository) IssueReadbackChallenge(
	ctx context.Context,
	command domainrepository.IssueReadbackChallengeCommand,
) (model.ReadbackChallenge, error) {
	var challengeID string
	err := repository.pool.QueryRow(
		ctx,
		issueChallengeSQL,
		pgx.StrictNamedArgs{
			"challenge_id":                      command.ChallengeID,
			"challenge_jti":                     command.ChallengeJTI,
			"intent_id":                         command.IntentID,
			"readback_credential_jti":           command.ReadbackCredentialJTI,
			"readback_credential_digest_sha256": command.ReadbackCredentialDigest,
			"idempotency_key":                   command.IdempotencyKey,
			"semantic_request_digest_sha256":    command.SemanticRequestDigest,
			"challenge_nonce":                   command.ChallengeNonce,
			"challenge_digest_sha256":           command.ChallengeDigestSHA256,
		},
	).Scan(&challengeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackChallenge{}, domainrepository.ErrIdempotencyConflict
	}
	if err != nil {
		return model.ReadbackChallenge{}, fmt.Errorf("issue durable readback challenge: %w", err)
	}
	return repository.LoadReadbackChallenge(ctx, challengeID, command.PeerSPIFFEID)
}

// LoadReadbackChallenge reads the challenge and its intent under exact challenge/peer arguments, mapping absence separately from driver failure. Status, expiry and signature eligibility remain caller/domain responsibilities.
// LoadReadbackChallenge читает challenge и его intent по точным аргументам challenge и peer, отдельно преобразуя отсутствие и ошибку драйвера. Допустимость статуса, срока и подписи остаётся ответственностью caller и домена.
func (repository *Repository) LoadReadbackChallenge(
	ctx context.Context,
	challengeID string,
	peerSPIFFEID string,
) (model.ReadbackChallenge, error) {
	challenge, err := scanChallenge(repository.pool.QueryRow(
		ctx,
		loadChallengeSQL,
		pgx.StrictNamedArgs{
			"challenge_id":   challengeID,
			"peer_spiffe_id": peerSPIFFEID,
		},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackChallenge{}, domainrepository.ErrNotFound
	}
	if err != nil {
		return model.ReadbackChallenge{}, fmt.Errorf("load durable readback challenge: %w", err)
	}
	return challenge, nil
}

// ConsumeReadbackChallenge retries the whole single-attempt path only for wrapped typed SQLSTATE40001 under the original context and five fixed delays. Wait cancellation returns ctx.Err directly; other outcomes return immediately and exhaustion wraps the last failure. It creates no new receipt identity or input.
// ConsumeReadbackChallenge повторяет весь путь одной попытки только для обёрнутого типизированного SQLSTATE40001 под исходным контекстом с пятью фиксированными задержками. Отмена ожидания возвращает ctx.Err напрямую; остальные исходы возвращаются сразу, исчерпание оборачивает последнюю ошибку. Новые идентичность квитанции и вход не создаются.
func (repository *Repository) ConsumeReadbackChallenge(
	ctx context.Context,
	command domainrepository.ConsumeReadbackChallengeCommand,
) (model.ReadbackReceipt, error) {
	var lastErr error
	for _, delay := range consumeSerializationRetryDelays {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return model.ReadbackReceipt{}, ctx.Err()
			case <-timer.C:
			}
		}
		receipt, err := repository.consumeReadbackChallengeOnce(ctx, command)
		if err == nil || !isSerializationFailure(err) {
			return receipt, err
		}
		lastErr = err
	}
	return model.ReadbackReceipt{}, fmt.Errorf(
		"consume readback challenge after serialization retries: %w",
		lastErr,
	)
}

// consumeReadbackChallengeOnce begins READ COMMITTED, reads the peer-bound challenge and an existing idempotent receipt, then either validates four repeat bindings or consumes once and reloads the immutable receipt before commit. Error exits return no receipt and defer best-effort rollback under the same context; cleanup success is not independently proved here.
// consumeReadbackChallengeOnce начинает READ COMMITTED, читает challenge в области peer и существующую идемпотентную квитанцию, затем проверяет четыре привязки повтора либо однократно потребляет challenge и перечитывает неизменяемую квитанцию до commit. Ошибки возвращают пустую квитанцию с отложенным rollback по возможности под тем же контекстом; успех очистки отдельно не доказывается.
func (repository *Repository) consumeReadbackChallengeOnce(
	ctx context.Context,
	command domainrepository.ConsumeReadbackChallengeCommand,
) (model.ReadbackReceipt, error) {
	transaction, err := repository.pool.BeginTx(
		ctx,
		readbackConsumeTransactionOptions(),
	)
	if err != nil {
		return model.ReadbackReceipt{}, fmt.Errorf("begin readback consume transaction: %w", err)
	}
	defer func() {
		_ = transaction.Rollback(ctx)
	}()
	challenge, err := scanChallenge(transaction.QueryRow(
		ctx,
		loadChallengeSQL,
		pgx.StrictNamedArgs{
			"challenge_id":   command.ChallengeID,
			"peer_spiffe_id": command.PeerSPIFFEID,
		},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackReceipt{}, domainrepository.ErrNotFound
	}
	if err != nil {
		return model.ReadbackReceipt{}, fmt.Errorf("lock readback challenge: %w", err)
	}
	receipt, found, err := loadReceipt(
		ctx,
		transaction,
		command.PeerSPIFFEID,
		command.IdempotencyKey,
	)
	if err != nil {
		return model.ReadbackReceipt{}, err
	}
	if found {
		if receipt.ChallengeID != command.ChallengeID ||
			receipt.EvidenceJTI != command.EvidenceJTI ||
			receipt.EvidenceDigestSHA256 != command.EvidenceDigestSHA256 ||
			receipt.SemanticRequestDigest != command.SemanticRequestDigest {
			return model.ReadbackReceipt{}, domainrepository.ErrIdempotencyConflict
		}
		receipt.Intent = challenge.Intent
		if err := transaction.Commit(ctx); err != nil {
			return model.ReadbackReceipt{}, fmt.Errorf("commit persisted readback receipt retry: %w", err)
		}
		return receipt, nil
	}
	if challenge.ConsumedAt != nil {
		return model.ReadbackReceipt{}, domainrepository.ErrReplay
	}
	var receiptID string
	err = transaction.QueryRow(
		ctx,
		consumeChallengeSQL,
		pgx.StrictNamedArgs{
			"challenge_id":                   command.ChallengeID,
			"receipt_id":                     command.ReceiptID,
			"evidence_jti":                   command.EvidenceJTI,
			"evidence_digest_sha256":         command.EvidenceDigestSHA256,
			"idempotency_key":                command.IdempotencyKey,
			"semantic_request_digest_sha256": command.SemanticRequestDigest,
			"verifier_generation":            command.VerifierGeneration,
		},
	).Scan(&receiptID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackReceipt{}, domainrepository.ErrExpired
	}
	if err != nil {
		return model.ReadbackReceipt{}, fmt.Errorf("consume durable readback challenge: %w", err)
	}
	receipt, found, err = loadReceipt(
		ctx,
		transaction,
		command.PeerSPIFFEID,
		command.IdempotencyKey,
	)
	if err != nil {
		return model.ReadbackReceipt{}, err
	}
	if !found || receipt.ReceiptID != receiptID {
		return model.ReadbackReceipt{}, errors.New(
			"readback consume function returned no immutable receipt",
		)
	}
	receipt.Intent = challenge.Intent
	if err := transaction.Commit(ctx); err != nil {
		return model.ReadbackReceipt{}, fmt.Errorf("commit readback consume transaction: %w", err)
	}
	return receipt, nil
}

// readbackConsumeTransactionOptions returns READ COMMITTED for database-function-owned targeted locking, avoiding a new serializable transaction profile or local lock implementation.
// readbackConsumeTransactionOptions возвращает READ COMMITTED для адресных блокировок функции БД без нового профиля SERIALIZABLE или локальной реализации блокировок.
func readbackConsumeTransactionOptions() pgx.TxOptions {
	// SQL-функция адресно блокирует idempotency key, challenge и intent.
	// SERIALIZABLE здесь связывает независимые startup readback через SSI.
	return pgx.TxOptions{IsoLevel: pgx.ReadCommitted}
}

// isSerializationFailure uses errors.As to accept only a pgconn.PgError with SQLSTATE40001, including wrapped errors; plain messages or other SQLSTATE values do not authorize retry.
// isSerializationFailure через errors.As принимает только pgconn.PgError с SQLSTATE40001, включая обёрнутую ошибку; обычный текст и другие SQLSTATE не разрешают повтор.
func isSerializationFailure(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "40001"
}

// ActivateReadbackTrust passes exact served-trust/predecessor pins to the database activation function and maps a false result to snapshot rollback. It does not load files, publish secrets or verify signatures in this adapter.
// ActivateReadbackTrust передаёт точные привязки обслуживаемого доверия и предшественника функции активации БД, преобразуя false в rollback снимка. Файлы не читаются, секреты не публикуются и подписи в этом адаптере не проверяются.
func (repository *Repository) ActivateReadbackTrust(
	ctx context.Context,
	state model.ReadbackTrustState,
) error {
	var accepted bool
	if err := repository.pool.QueryRow(
		ctx,
		activateTrustSQL,
		readbackTrustArgs(state),
	).Scan(&accepted); err != nil {
		return fmt.Errorf("activate durable readback trust: %w", err)
	}
	if !accepted {
		return domainrepository.ErrSnapshotRollback
	}
	return nil
}

// ReadbackReady first checks the persistence privilege/fence boundary, then the exact stored trust watermark with a separate statement. Either false result fails closed; the method does not claim those two reads share one transaction snapshot.
// ReadbackReady сначала проверяет границу привилегий хранения и ограждения, затем точную сохранённую отметку доверия отдельным statement. Любой false закрыто отклоняется; общий транзакционный снимок двух чтений не заявляется.
func (repository *Repository) ReadbackReady(
	ctx context.Context,
	state model.ReadbackTrustState,
) error {
	var ready bool
	if err := repository.pool.QueryRow(ctx, readinessSQL).Scan(&ready); err != nil {
		return fmt.Errorf("verify readback persistence boundary: %w", err)
	}
	if !ready {
		return errors.New("readback persistence boundary is not ready")
	}
	if err := repository.pool.QueryRow(
		ctx,
		trustReadinessSQL,
		readbackTrustReadinessArgs(state),
	).Scan(&ready); err != nil {
		return fmt.Errorf("verify readback trust watermark: %w", err)
	}
	if !ready {
		return errors.New("readback trust watermark is not ready")
	}
	return nil
}

// readbackTrustReadinessArgs creates the nine exact root, manifest, trust-set, signer and served-digest bindings used by readiness, excluding activation-only predecessor and observation time.
// readbackTrustReadinessArgs создаёт девять точных привязок корня, manifest, trust-set, подписанта и обслуживаемого дайджеста для readiness без используемых лишь активацией предшественника и времени наблюдения.
func readbackTrustReadinessArgs(state model.ReadbackTrustState) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"root_id":                       state.RootID,
		"root_fingerprint_sha256":       state.RootFingerprintSHA256,
		"manifest_bundle_revision":      state.ManifestBundleRevision,
		"manifest_bundle_digest_sha256": state.ManifestBundleDigestSHA256,
		"trust_source_revision":         state.TrustSourceRevision,
		"trust_set_digest_sha256":       state.TrustSetDigestSHA256,
		"trust_key_set_revision":        state.TrustKeySetRevision,
		"signer_generation":             state.SignerGeneration,
		"served_state_digest_sha256":    state.ServedStateDigestSHA256,
	}
}

// readbackTrustArgs supplies the readiness trust pins plus predecessor-state digest and served-at time for activation, preserving zero values without normalization, eligibility checks or SQL execution.
// readbackTrustArgs передаёт привязки доверия readiness вместе с дайджестом состояния предшественника и временем served-at для активации, сохраняя нулевые значения без нормализации, проверки допустимости и исполнения SQL.
func readbackTrustArgs(state model.ReadbackTrustState) pgx.StrictNamedArgs {
	arguments := readbackTrustReadinessArgs(state)
	arguments["predecessor_state_digest_sha256"] = state.PredecessorStateDigestSHA256
	arguments["served_at"] = state.ServedAt
	return arguments
}

// rowScanner narrows database decoding to an ordered Scan operation; it carries no transaction ownership, peer identity or permission to load another row.
// rowScanner ограничивает декодирование БД упорядоченной операцией Scan; владение транзакцией, идентичность peer и право читать другую строку не передаются.
type rowScanner interface {
	// Scan decodes into ordered destination pointers and may return an error after partial writes; each scanner wrapper controls whether any partial model is returned.
	// Scan декодирует в упорядоченные указатели результата и может ошибиться после частичной записи; возврат частичной модели определяет обёртка scanner.
	Scan(dest ...any) error
}

// scanIntent reads eighteen intent columns and assigns the public-key bytes as json.RawMessage without JSON validation or a defensive copy. Scan errors discard the local intent; trust and value eligibility are not established by this decoder.
// scanIntent читает восемнадцать колонок intent и присваивает байты публичного ключа как json.RawMessage без проверки JSON и защитной копии. Ошибка Scan отбрасывает локальное намерение; доверие и допустимость значений декодером не устанавливаются.
func scanIntent(row rowScanner) (model.ReadbackIntent, error) {
	var intent model.ReadbackIntent
	var possessionPublicJWK []byte
	err := row.Scan(readbackrow.Destinations(&intent, &possessionPublicJWK)...)
	if err != nil {
		return model.ReadbackIntent{}, err
	}
	intent.PossessionPublicJWK = json.RawMessage(possessionPublicJWK)
	return intent, nil
}

// scanChallenge decodes eleven challenge columns followed by the same eighteen intent columns, returning a zero model on scan failure. Public JWK bytes retain the readback path's unvalidated, noncopied RawMessage semantics.
// scanChallenge декодирует одиннадцать колонок challenge и затем те же восемнадцать колонок intent, возвращая нулевую модель при ошибке Scan. Байты публичного JWK сохраняют семантику readback RawMessage без проверки и копирования.
func scanChallenge(row rowScanner) (model.ReadbackChallenge, error) {
	var challenge model.ReadbackChallenge
	var possessionPublicJWK []byte
	destinations := []any{
		&challenge.ChallengeID,
		&challenge.ChallengeJTI,
		&challenge.Nonce,
		&challenge.DigestSHA256,
		&challenge.ReadbackCredentialJTI,
		&challenge.ReadbackCredentialDigest,
		&challenge.IdempotencyKey,
		&challenge.SemanticRequestDigest,
		&challenge.IssuedAt,
		&challenge.ExpiresAt,
		&challenge.ConsumedAt,
	}
	destinations = append(destinations, readbackrow.Destinations(&challenge.Intent, &possessionPublicJWK)...)
	err := row.Scan(destinations...)
	if err != nil {
		return model.ReadbackChallenge{}, err
	}
	challenge.Intent.PossessionPublicJWK = json.RawMessage(possessionPublicJWK)
	return challenge, nil
}

// loadReceipt reads the peer/idempotency-bound eight receipt fields inside the supplied transaction, distinguishing absence from error. It does not validate replay eligibility or mutate the transaction, and discards partial rows on failure.
// loadReceipt читает восемь полей квитанции с привязкой peer и idempotency внутри переданной транзакции, отличая отсутствие от ошибки. Допустимость replay не проверяется, транзакция не меняется, частичные строки при отказе отбрасываются.
func loadReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	peerSPIFFEID string,
	idempotencyKey string,
) (model.ReadbackReceipt, bool, error) {
	var receipt model.ReadbackReceipt
	err := transaction.QueryRow(
		ctx,
		loadReceiptSQL,
		pgx.StrictNamedArgs{
			"peer_spiffe_id":  peerSPIFFEID,
			"idempotency_key": idempotencyKey,
		},
	).Scan(
		&receipt.ReceiptID,
		&receipt.ChallengeID,
		&receipt.EvidenceJTI,
		&receipt.EvidenceDigestSHA256,
		&receipt.SemanticRequestDigest,
		&receipt.VerifierGeneration,
		&receipt.AcceptedAt,
		&receipt.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackReceipt{}, false, nil
	}
	if err != nil {
		return model.ReadbackReceipt{}, false, fmt.Errorf("read persisted readback receipt: %w", err)
	}
	return receipt, true, nil
}
