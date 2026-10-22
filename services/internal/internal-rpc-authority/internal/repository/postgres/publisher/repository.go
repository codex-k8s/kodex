package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	domainrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/postgres/readbackrow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository persists publisher delivery, publication and rotation state through a caller-configured PostgreSQL pool. Database functions own transition eligibility; it does not deliver Kubernetes material itself.
// Repository сохраняет состояния доставки, публикации и ротации publisher через настроенный вызывающим пул PostgreSQL. Допустимостью переходов владеют функции БД; материал Kubernetes самостоятельно не доставляется.
type Repository struct {
	pool *pgxpool.Pool
}

// LoadSnapshotHistory reads the SQL-bounded history, closes rows, rejects scan/iteration failures and sorts revision/digest pairs ascending. It can retain the current revision among the thirty-three rows; predecessor-window selection belongs to the consumer.
// LoadSnapshotHistory читает ограниченную SQL историю, закрывает rows, отклоняет ошибки Scan и итерации и сортирует пары ревизии и дайджеста по возрастанию. Среди тридцати трёх строк может быть текущая ревизия; окно предшественников выбирает потребитель.
func (repository *Repository) LoadSnapshotHistory(
	ctx context.Context,
) (model.AuthoritySnapshotHistory, error) {
	rows, err := repository.pool.Query(ctx, loadSnapshotHistorySQL)
	if err != nil {
		return model.AuthoritySnapshotHistory{}, fmt.Errorf(
			"load publisher snapshot history: %w",
			err,
		)
	}
	defer rows.Close()
	result := model.AuthoritySnapshotHistory{}
	for rows.Next() {
		var item model.RevisionDigest
		if err := rows.Scan(&item.Revision, &item.DigestSHA256); err != nil {
			return model.AuthoritySnapshotHistory{}, fmt.Errorf(
				"scan publisher snapshot history: %w",
				err,
			)
		}
		result.Current = append(result.Current, item)
	}
	if err := rows.Err(); err != nil {
		return model.AuthoritySnapshotHistory{}, fmt.Errorf(
			"iterate publisher snapshot history: %w",
			err,
		)
	}
	sort.Slice(result.Current, func(left, right int) bool {
		return result.Current[left].Revision < result.Current[right].Revision
	})
	return result, nil
}

// LoadSnapshotPublication reads immutable publication fields by source revision and input digest, distinguishing absence from failure. It does not regenerate a signed snapshot or validate its signature here.
// LoadSnapshotPublication читает неизменяемые поля публикации по исходной ревизии и дайджесту входа, отличая отсутствие от ошибки. Подписанный снимок не генерируется заново и его подпись здесь не проверяется.
func (repository *Repository) LoadSnapshotPublication(
	ctx context.Context,
	sourceRevision uint64,
	inputDigest string,
) (model.AuthoritySnapshotPublication, bool, error) {
	var result model.AuthoritySnapshotPublication
	err := repository.pool.QueryRow(
		ctx,
		loadSnapshotPublicationSQL,
		pgx.StrictNamedArgs{
			"source_revision":                 sourceRevision,
			"publication_input_digest_sha256": inputDigest,
		},
	).Scan(
		&result.IntentID,
		&result.InputDigestSHA256,
		&result.SourceRevision,
		&result.SourceDigestSHA256,
		&result.KeySetRevision,
		&result.PolicyRevision,
		&result.SignerGeneration,
		&result.PredecessorRevision,
		&result.PredecessorDigestSHA256,
		&result.SnapshotCompactJWS,
		&result.PublishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AuthoritySnapshotPublication{}, false, nil
	}
	if err != nil {
		return model.AuthoritySnapshotPublication{}, false, fmt.Errorf(
			"load publisher snapshot publication: %w",
			err,
		)
	}
	return result, true, nil
}

// LoadSnapshotPredecessor reads persisted publication plus registry/phase provenance under exact source and registry digests. Absence is explicit; historic inputs are not reconstructed from current configuration.
// LoadSnapshotPredecessor читает сохранённую публикацию с происхождением реестра и фазы по точным дайджестам источника и реестра. Отсутствие выражено явно; исторический вход не восстанавливается из текущей конфигурации.
func (repository *Repository) LoadSnapshotPredecessor(
	ctx context.Context,
	sourceRevision uint64,
	sourceDigest string,
	registryDigest string,
) (model.AuthoritySnapshotPredecessor, bool, error) {
	var result model.AuthoritySnapshotPredecessor
	err := repository.pool.QueryRow(
		ctx,
		loadSnapshotPredecessorSQL,
		pgx.StrictNamedArgs{
			"source_revision":        sourceRevision,
			"source_digest_sha256":   sourceDigest,
			"registry_digest_sha256": registryDigest,
		},
	).Scan(
		&result.Publication.IntentID,
		&result.Publication.InputDigestSHA256,
		&result.Publication.SourceRevision,
		&result.Publication.SourceDigestSHA256,
		&result.Publication.KeySetRevision,
		&result.Publication.PolicyRevision,
		&result.Publication.SignerGeneration,
		&result.Publication.PredecessorRevision,
		&result.Publication.PredecessorDigestSHA256,
		&result.Publication.SnapshotCompactJWS,
		&result.Publication.PublishedAt,
		&result.RegistryDigestSHA256,
		&result.RotationPhase,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AuthoritySnapshotPredecessor{}, false, nil
	}
	if err != nil {
		return model.AuthoritySnapshotPredecessor{}, false, fmt.Errorf(
			"load publisher snapshot predecessor: %w",
			err,
		)
	}
	return result, true, nil
}

// PrepareRotation invokes the fixed preparation transition with intent, source, predecessor and expected-readback arguments before external delivery; it performs no Kubernetes CAS.
// PrepareRotation вызывает фиксированный переход подготовки с аргументами намерения, источника, предшественника и ожидаемых readback до внешней доставки; Kubernetes CAS не выполняется.
func (repository *Repository) PrepareRotation(
	ctx context.Context,
	value model.AuthorityRotationIntent,
) error {
	return repository.rotationTransition(ctx, prepareRotationSQL, value,
		"prepare publisher authority rotation")
}

// LoadOrPrepareRotationOperation selects the database function that durably resolves one composite rotation from its registry/base pins, without supplying phase-publication arguments or local deadlines.
// LoadOrPrepareRotationOperation выбирает функцию БД, устойчиво разрешающую составную ротацию по привязкам реестра и основания, без аргументов публикации фазы или локальных дедлайнов.
func (repository *Repository) LoadOrPrepareRotationOperation(
	ctx context.Context,
	value model.AuthorityRotationOperation,
) (model.AuthorityRotationOperation, error) {
	return repository.rotationOperation(ctx, loadOrPrepareRotationOperationSQL, value, "")
}

// LoadRotationOperation reads one persisted operation by ID and reports absence separately, preserving database-assigned status/times rather than creating an intent or extending a deadline.
// LoadRotationOperation читает одну сохранённую операцию по ID и отдельно сообщает отсутствие, сохраняя назначенные БД статус и времена без нового намерения или продления дедлайна.
func (repository *Repository) LoadRotationOperation(
	ctx context.Context,
	operationID string,
) (model.AuthorityRotationOperation, bool, error) {
	var result model.AuthorityRotationOperation
	err := repository.pool.QueryRow(ctx, loadRotationOperationSQL, operationID).Scan(
		&result.OperationID, &result.RegistryRevision,
		&result.RegistryDigestSHA256, &result.BaseRevision,
		&result.BaseDigestSHA256, &result.ExpectedReadbackCount,
		&result.Status, &result.SwitchNotBefore, &result.PreviousNotAfter,
		&result.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AuthorityRotationOperation{}, false, nil
	}
	if err != nil {
		return model.AuthorityRotationOperation{}, false,
			fmt.Errorf("load authority rotation operation: %w", err)
	}
	return result, true, nil
}

// PrepareRotationPhase binds operation, phase, intent/source/predecessor and publication-input digest in the fixed function call. A false result becomes snapshot rollback; no external delivery occurs.
// PrepareRotationPhase связывает операцию, фазу, намерение, источник, предшественника и дайджест входа публикации в фиксированном вызове функции. False становится rollback снимка; внешней доставки нет.
func (repository *Repository) PrepareRotationPhase(
	ctx context.Context,
	operation model.AuthorityRotationOperation,
	phase string,
	value model.AuthorityRotationIntent,
) error {
	var accepted bool
	err := repository.pool.QueryRow(ctx, prepareRotationPhaseSQL,
		operation.OperationID, phase, value.IntentID, value.SourceRevision,
		value.SourceDigestSHA256, value.PredecessorRevision,
		value.PredecessorDigestSHA256, value.ExpectedReadbackCount,
		value.PublicationInputDigestSHA256,
	).Scan(&accepted)
	if err != nil {
		return fmt.Errorf("prepare authority rotation phase: %w", err)
	}
	if !accepted {
		return domainrepository.ErrSnapshotRollback
	}
	return nil
}

// AdvanceRotationOperation submits the phase and exact publication pins to the fixed database operation-advance function, returning its persisted state rather than deciding eligibility from local booleans.
// AdvanceRotationOperation передаёт фазу и точные привязки публикации фиксированной функции БД продвижения операции, возвращая сохранённое состояние вместо определения допустимости локальными bool.
func (repository *Repository) AdvanceRotationOperation(
	ctx context.Context,
	value model.AuthorityRotationOperation,
	phase string,
	publication model.AuthoritySnapshotPublication,
) (model.AuthorityRotationOperation, error) {
	return repository.rotationOperation(ctx, advanceRotationOperationSQL, value, phase,
		publication.IntentID, publication.SourceRevision,
		publication.SourceDigestSHA256, publication.InputDigestSHA256)
}

// rotationOperation supplies six base positional arguments and adds phase/publication arguments only for a nonempty phase, then scans the ten-column operation state. Callers must choose the matching fixed SQL profile; this helper does not validate a generic dynamic query.
// rotationOperation передаёт шесть основных позиционных аргументов и добавляет фазу и публикацию только при непустой фазе, затем читает десять колонок состояния операции. Вызывающий выбирает совместимый фиксированный SQL-профиль; общий динамический запрос helper не проверяет.
func (repository *Repository) rotationOperation(
	ctx context.Context,
	query string,
	value model.AuthorityRotationOperation,
	phase string,
	publication ...any,
) (model.AuthorityRotationOperation, error) {
	arguments := []any{value.OperationID, value.RegistryRevision,
		value.RegistryDigestSHA256, value.BaseRevision, value.BaseDigestSHA256,
		value.ExpectedReadbackCount}
	if phase != "" {
		arguments = append(arguments, phase)
		arguments = append(arguments, publication...)
	}
	var result model.AuthorityRotationOperation
	err := repository.pool.QueryRow(ctx, query, arguments...).Scan(
		&result.OperationID, &result.RegistryRevision,
		&result.RegistryDigestSHA256, &result.BaseRevision,
		&result.BaseDigestSHA256, &result.ExpectedReadbackCount,
		&result.Status, &result.SwitchNotBefore, &result.PreviousNotAfter,
		&result.CompletedAt,
	)
	if err != nil {
		return model.AuthorityRotationOperation{}, fmt.Errorf("load authority rotation operation: %w", err)
	}
	return result, nil
}

// BeginRotationDelivery executes the fixed durable transition that marks delivery started for exact intent/source pins; actual CAS and readback remain outside this method.
// BeginRotationDelivery выполняет фиксированный устойчивый переход начала доставки по точным привязкам намерения и источника; сами CAS и readback остаются вне метода.
func (repository *Repository) BeginRotationDelivery(
	ctx context.Context,
	value model.AuthorityRotationIntent,
) error {
	return repository.rotationTransition(ctx, beginRotationDeliverySQL, value,
		"begin publisher authority rotation delivery")
}

// MarkRotationDelivered records the fixed delivered transition for the exact intent/source binding, separately from snapshot promotion and consumer readiness.
// MarkRotationDelivered фиксирует переход доставки для точной привязки намерения и источника отдельно от продвижения снимка и готовности потребителей.
func (repository *Repository) MarkRotationDelivered(
	ctx context.Context,
	value model.AuthorityRotationIntent,
) error {
	return repository.rotationTransition(ctx, markRotationDeliveredSQL, value,
		"mark publisher authority rotation delivered")
}

// rotationTransition builds strict intent/source arguments and adds predecessor/readback-count fields only for the exact prepare SQL body. It stage-wraps driver errors and maps false to rollback; fixed call sites own the query choice.
// rotationTransition строит строгие аргументы намерения и источника и добавляет поля предшественника и числа readback только для точного тела prepare SQL. Ошибки драйвера оборачиваются этапом, false означает rollback; запрос выбирают фиксированные call sites.
func (repository *Repository) rotationTransition(
	ctx context.Context,
	query string,
	value model.AuthorityRotationIntent,
	operation string,
) error {
	arguments := pgx.StrictNamedArgs{
		"intent_id":            value.IntentID,
		"source_revision":      value.SourceRevision,
		"source_digest_sha256": value.SourceDigestSHA256,
	}
	if query == prepareRotationSQL {
		arguments["predecessor_revision"] = value.PredecessorRevision
		arguments["predecessor_digest_sha256"] = value.PredecessorDigestSHA256
		arguments["expected_readback_count"] = value.ExpectedReadbackCount
	}
	var accepted bool
	if err := repository.pool.QueryRow(ctx, query, arguments).Scan(&accepted); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if !accepted {
		return domainrepository.ErrSnapshotRollback
	}
	return nil
}

// AppendSnapshot submits immutable publication pins once; on false it reads the same revision/input digest and accepts a match of the checked intent/version/predecessor fields. Driver errors propagate without mutation retry, and success may return either the submitted value or persisted publication.
// AppendSnapshot однократно передаёт неизменяемые привязки публикации; при false читает те же ревизию и дайджест входа и принимает совпадение проверенных полей намерения, версий и предшественника. Ошибки драйвера возвращаются без повтора изменения; успех возвращает переданное либо сохранённое значение.
func (repository *Repository) AppendSnapshot(
	ctx context.Context,
	value model.AuthoritySnapshotPublication,
	expectedReadbacks int,
) (model.AuthoritySnapshotPublication, error) {
	var accepted bool
	if err := repository.pool.QueryRow(
		ctx,
		appendSnapshotSQL,
		pgx.StrictNamedArgs{
			"source_revision":                 value.SourceRevision,
			"source_digest_sha256":            value.SourceDigestSHA256,
			"key_set_revision":                value.KeySetRevision,
			"policy_revision":                 value.PolicyRevision,
			"signer_generation":               value.SignerGeneration,
			"predecessor_revision":            value.PredecessorRevision,
			"predecessor_digest_sha256":       value.PredecessorDigestSHA256,
			"snapshot_compact_jws":            value.SnapshotCompactJWS,
			"publication_intent_id":           value.IntentID,
			"publication_input_digest_sha256": value.InputDigestSHA256,
			"expected_readback_count":         expectedReadbacks,
			"published_at":                    value.PublishedAt,
		},
	).Scan(&accepted); err != nil {
		return model.AuthoritySnapshotPublication{}, fmt.Errorf(
			"append publisher snapshot history: %w",
			err,
		)
	}
	if !accepted {
		existing, found, loadErr := repository.LoadSnapshotPublication(
			ctx,
			value.SourceRevision,
			value.InputDigestSHA256,
		)
		if loadErr != nil {
			return model.AuthoritySnapshotPublication{}, loadErr
		}
		if found &&
			existing.IntentID == value.IntentID &&
			existing.SourceDigestSHA256 == value.SourceDigestSHA256 &&
			existing.KeySetRevision == value.KeySetRevision &&
			existing.PolicyRevision == value.PolicyRevision &&
			existing.SignerGeneration == value.SignerGeneration &&
			existing.PredecessorRevision == value.PredecessorRevision &&
			existing.PredecessorDigestSHA256 == value.PredecessorDigestSHA256 {
			return existing, nil
		}
		return model.AuthoritySnapshotPublication{}, domainrepository.ErrSnapshotRollback
	}
	return value, nil
}

// SnapshotPublicationReady is a promotion command despite its name: it requires nonempty expected targets and passes their ordered workload/role/generation arrays and exact publication pins to SQL. False reports an incomplete readback set.
// SnapshotPublicationReady, несмотря на имя, является командой продвижения: требует непустые ожидаемые цели и передаёт в SQL упорядоченные массивы workload, ролей и поколений и точные привязки публикации. False сообщает неполный набор readback.
func (repository *Repository) SnapshotPublicationReady(
	ctx context.Context,
	value model.AuthoritySnapshotPublication,
	expectedTargets []model.SnapshotReadbackTarget,
) error {
	if len(expectedTargets) == 0 {
		return errors.New("publisher snapshot expected readback set is empty")
	}
	workloadIDs := make([]string, 0, len(expectedTargets))
	roles := make([]string, 0, len(expectedTargets))
	workloadGenerations := make([]int64, 0, len(expectedTargets))
	for _, target := range expectedTargets {
		workloadIDs = append(workloadIDs, target.WorkloadID)
		roles = append(roles, target.Role)
		workloadGenerations = append(
			workloadGenerations,
			int64(target.WorkloadGeneration),
		)
	}
	var ready bool
	if err := repository.pool.QueryRow(
		ctx,
		promoteSnapshotSQL,
		pgx.StrictNamedArgs{
			"publication_intent_id":   value.IntentID,
			"source_revision":         value.SourceRevision,
			"source_digest_sha256":    value.SourceDigestSHA256,
			"expected_readback_count": len(expectedTargets),
			"expected_workload_ids":   workloadIDs,
			"expected_roles":          roles,
			"expected_generations":    workloadGenerations,
		},
	).Scan(&ready); err != nil {
		return fmt.Errorf("promote publisher snapshot: %w", err)
	}
	if !ready {
		return errors.New("publisher snapshot readback set is incomplete")
	}
	return nil
}

// New rejects a nil pool and validates fixed publisher query headers without pinging the database, asserting runtime privileges or running a rotation.
// New отклоняет nil-пул и проверяет заголовки фиксированных publisher queries без Ping БД, подтверждения runtime-привилегий или запуска ротации.
func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("publisher PostgreSQL pool is nil")
	}
	if err := validateQueries(); err != nil {
		return nil, err
	}
	return &Repository{pool: pool}, nil
}

// LoadPublishedCredential reads a delivery receipt by its idempotency key under the pool's database boundary, returning explicit absence and discarding partial scan values on failure.
// LoadPublishedCredential читает квитанцию доставки по ключу идемпотентности под границей БД пула, явно возвращая отсутствие и отбрасывая частичные значения Scan при ошибке.
func (repository *Repository) LoadPublishedCredential(
	ctx context.Context,
	idempotencyKey string,
) (model.PublishedCredential, bool, error) {
	result, err := scanPublished(repository.pool.QueryRow(
		ctx,
		loadDeliverySQL,
		pgx.StrictNamedArgs{"idempotency_key": idempotencyKey},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.PublishedCredential{}, false, nil
	}
	if err != nil {
		return model.PublishedCredential{}, false, fmt.Errorf(
			"load publisher delivery receipt: %w",
			err,
		)
	}
	return result, true, nil
}

// SavePublishedCredential submits the exact receipt/directive/credential-generation fields and returns the database's stored result. No row means idempotency conflict; signing, external delivery and automatic retries are not performed here.
// SavePublishedCredential передаёт точные поля квитанции, директивы и поколений credential и возвращает сохранённый результат БД. Отсутствие строки означает конфликт идемпотентности; подписи, внешней доставки и автоматических повторов здесь нет.
func (repository *Repository) SavePublishedCredential(
	ctx context.Context,
	value model.PublishedCredential,
) (model.PublishedCredential, error) {
	result, err := scanPublished(repository.pool.QueryRow(
		ctx,
		saveDeliverySQL,
		pgx.StrictNamedArgs{
			"idempotency_key":               value.IdempotencyKey,
			"directive_jti":                 value.DirectiveJTI,
			"directive_digest_sha256":       value.DirectiveDigest,
			"delivery_receipt_compact_jws":  value.DeliveryReceiptJWS,
			"role_credential_digest_sha256": value.RoleCredentialDigest,
			"credential_generation":         value.CredentialGeneration,
			"ack_key_generation":            value.ACKKeyGeneration,
			"accepted_at":                   value.AcceptedAt,
		},
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.PublishedCredential{}, domainrepository.ErrIdempotencyConflict
	}
	if err != nil {
		return model.PublishedCredential{}, fmt.Errorf(
			"save publisher delivery receipt: %w",
			err,
		)
	}
	return result, nil
}

// PublisherReady executes the persistence-boundary readiness statement and requires its boolean result, rather than treating a nonnil pool or successful query-header validation as readiness.
// PublisherReady выполняет statement готовности границы хранения и требует его bool-результат вместо признания готовности по ненулевому пулу или проверенным заголовкам.
func (repository *Repository) PublisherReady(ctx context.Context) error {
	var ready bool
	if err := repository.pool.QueryRow(ctx, readinessSQL).Scan(&ready); err != nil {
		return fmt.Errorf("verify publisher persistence boundary: %w", err)
	}
	if !ready {
		return errors.New("publisher persistence boundary is not ready")
	}
	return nil
}

// PinReadbackIntent inserts or resolves exact intent/material bindings. After no rows it performs one separate exact pinned-intent lookup to observe a concurrent commit without granting UPDATE; persistent absence becomes idempotency conflict.
// PinReadbackIntent вставляет либо разрешает точные привязки намерения и материала. После отсутствия строк выполняется один отдельный точный поиск PINNED intent для наблюдения конкурентного commit без выдачи UPDATE; повторное отсутствие становится конфликтом идемпотентности.
func (repository *Repository) PinReadbackIntent(
	ctx context.Context,
	value model.ReadbackIntent,
) (model.ReadbackIntent, error) {
	args := pgx.StrictNamedArgs{
		"intent_id":                        value.IntentID,
		"kind":                             value.Kind,
		"intent_revision":                  value.IntentRevision,
		"intent_digest_sha256":             value.IntentDigestSHA256,
		"workload_id":                      value.WorkloadID,
		"workload_spiffe_id":               value.WorkloadSPIFFEID,
		"role":                             value.Role,
		"workload_generation":              value.WorkloadGeneration,
		"credential_generation":            value.CredentialGeneration,
		"material_generation":              value.MaterialGeneration,
		"possession_key_generation":        value.PossessionKeyGeneration,
		"possession_key_kid":               value.PossessionKeyID,
		"possession_public_jwk":            value.PossessionPublicJWK,
		"possession_key_thumbprint_sha256": value.PossessionKeyThumbprint,
		"source_revision":                  value.SourceRevision,
		"served_state_digest_sha256":       value.ServedStateDigestSHA256,
		"expires_at":                       value.ExpiresAt,
	}
	result, err := scanReadbackIntent(repository.pool.QueryRow(
		ctx,
		pinReadbackIntentSQL,
		args,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		// INSERT ... ON CONFLICT может дождаться конкурентного commit, сохранив
		// старый snapshot statement. Отдельный запрос увидит commit без выдачи
		// publisher права UPDATE.
		result, err = scanReadbackIntent(repository.pool.QueryRow(
			ctx,
			loadPinnedReadbackIntentSQL,
			pgx.StrictNamedArgs{
				"intent_id":            value.IntentID,
				"intent_digest_sha256": value.IntentDigestSHA256,
				"workload_spiffe_id":   value.WorkloadSPIFFEID,
			},
		))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReadbackIntent{}, domainrepository.ErrIdempotencyConflict
	}
	if err != nil {
		return model.ReadbackIntent{}, fmt.Errorf("pin publisher readback intent: %w", err)
	}
	return result, nil
}

// scanReadbackIntent scans the fixed eighteen-column intent layout, rejects scan errors and invalid JSON, then copies the public JWK bytes. It does not validate JWK algorithm/key identity or prove the intent's eligibility.
// scanReadbackIntent читает фиксированный восемнадцатиколоночный layout intent, отклоняет ошибки Scan и невалидный JSON, затем копирует байты публичного JWK. Алгоритм и идентичность JWK и допустимость intent здесь не проверяются.
func scanReadbackIntent(row rowScanner) (model.ReadbackIntent, error) {
	var result model.ReadbackIntent
	var publicJWK []byte
	err := row.Scan(readbackrow.Destinations(&result, &publicJWK)...)
	if err != nil {
		return model.ReadbackIntent{}, err
	}
	if !json.Valid(publicJWK) {
		return model.ReadbackIntent{}, errors.New("pinned readback public JWK is invalid")
	}
	result.PossessionPublicJWK = append([]byte(nil), publicJWK...)
	return result, nil
}

// rowScanner is the narrow positional scan seam for publisher result decoding, independent of a concrete pgx row or database connection lifecycle.
// rowScanner — узкий интерфейс позиционного Scan для декодирования publisher results независимо от конкретного pgx row и жизненного цикла соединения БД.
type rowScanner interface {
	// Scan writes into the exact ordered destination pointers and returns a decoding error; callers must not treat partially written destinations as accepted domain state.
	// Scan пишет в точные упорядоченные указатели результата и возвращает ошибку декодирования; частично записанные значения нельзя считать принятым доменным состоянием.
	Scan(...any) error
}

// scanPublished scans eight receipt fields and returns the scan error alongside its result, which may be partial on failure. Repository callers decide absence/conflict mapping and discard that partial result.
// scanPublished читает восемь полей квитанции и возвращает ошибку Scan вместе с результатом, который при отказе может быть частичным. Repository callers определяют отсутствие и конфликт и отбрасывают частичный результат.
func scanPublished(row rowScanner) (model.PublishedCredential, error) {
	var result model.PublishedCredential
	err := row.Scan(
		&result.IdempotencyKey,
		&result.DirectiveJTI,
		&result.DirectiveDigest,
		&result.DeliveryReceiptJWS,
		&result.RoleCredentialDigest,
		&result.CredentialGeneration,
		&result.ACKKeyGeneration,
		&result.AcceptedAt,
	)
	return result, err
}

var _ domainrepository.PublisherStore = (*Repository)(nil)
