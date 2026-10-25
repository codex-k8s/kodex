package platform

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	// queryServiceCredentialGeneration embeds the protected workload credential generation read from
	// the durable high-watermark row. No caller-provided generation is accepted and the lookup does
	// not renew or revoke a grant.
	// queryServiceCredentialGeneration встраивает чтение protected credential generation workload из
	// устойчивой high-watermark строки. Поколение клиента не принимается, чтение не продлевает и не
	// отзывает grant.
	//go:embed sql/service_credential_generation.sql
	queryServiceCredentialGeneration string
	// queryTrustedWorkloadGeneration embeds enabled trusted workload generation combined with the
	// previous protected high-watermark floor. It cannot lower durable generation merely because
	// transport profile changed.
	// queryTrustedWorkloadGeneration встраивает поколение включённого trusted workload совместно с
	// прежним protected high-watermark floor. Не позволяет понизить устойчивое поколение из-за смены
	// транспортного профиля.
	//go:embed sql/trusted_workload__generation.sql
	queryTrustedWorkloadGeneration string
	// queryWorkerGrantAcceptGeneration embeds the shared workload generation-floor insert/lock with
	// database-time validity, at most five seconds future issue time and a four-minute lifetime. Lower
	// generations are rejected; an advance resets the generation-local legacy revision without
	// invalidating sibling instance revisions.
	// queryWorkerGrantAcceptGeneration встраивает вставку и блокировку общего generation floor
	// workload с проверкой времени БД, выпуском максимум на пять секунд в будущем и lifetime четыре
	// минуты. Меньшие поколения отклоняются; продвижение сбрасывает legacy revision поколения без
	// инвалидации ревизий соседних instances.
	//go:embed sql/proof_worker_grant_accept_generation.sql
	queryWorkerGrantAcceptGeneration string
	// queryWorkerGrantAcceptInstance embeds durable per-workload/instance generation/revision
	// advancement or an exact same-revision replay including times and envelope hash. Other same/lower
	// revisions are rejected; caller generation-floor acceptance must share the transaction.
	// queryWorkerGrantAcceptInstance встраивает устойчивое продвижение generation/revision пары
	// workload/instance либо точный повтор той же ревизии с временем и хешем оболочки. Другие равные
	// либо меньшие ревизии отклоняются; допуск общего generation floor выполняется в той же
	// транзакции.
	//go:embed sql/proof_worker_grant_accept_instance.sql
	queryWorkerGrantAcceptInstance string
)

// ConfigureRPCProfile sets the repository transport mode from the closed
// protected-empty/trusted-cluster choices during composition before publication. Unknown profiles
// return a fixed English error; request headers and admission failures cannot switch this setting
// through this method.
// ConfigureRPCProfile назначает транспортный режим repository из закрытых вариантов
// protected-пустой либо trusted-cluster при сборке до публикации. Неизвестный профиль возвращает
// фиксированную английскую ошибку; заголовки запросов и отказы допуска не переключают настройку
// через этот метод.
func (repository *Repository) ConfigureRPCProfile(profile string) error {
	switch profile {
	case "":
		repository.trustedCluster = false
	case transportprofile.TrustedCluster:
		repository.trustedCluster = true
	default:
		return errors.New("repository RPC profile rejected")
	}
	return nil
}

// ResolveServiceCredentialGeneration loads generation from the profile-specific durable owner
// registry, rejecting unknown workloads and disabled trusted registrations and values
// outside1..2^53-1. It never accepts caller generation or changes grant history; certificate
// lifetime and revocation policy remain with admission/consumers, while the retained generation
// serves domain lease fences.
// ResolveServiceCredentialGeneration читает поколение из соответствующего профилю устойчивого
// реестра владельца, отклоняя неизвестный workload либо выключенную trusted-регистрацию и значения
// вне1..2^53-1. Не принимает поколение клиента и не меняет историю grants; lifetime сертификата и
// отзыв остаются у допуска и потребителей, сохранённое поколение используется доменными lease
// fences.
func (repository *Repository) ResolveServiceCredentialGeneration(ctx context.Context, workload string) (uint64, error) {
	if workload == "" {
		return 0, errs.ErrForbidden
	}
	var generation uint64
	var err error
	if repository.trustedCluster {
		err = repository.pool.QueryRow(ctx, queryTrustedWorkloadGeneration,
			pgx.StrictNamedArgs{"workload_id": workload}).Scan(&generation)
	} else {
		err = repository.pool.QueryRow(ctx, queryServiceCredentialGeneration, workload).Scan(&generation)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, errs.ErrForbidden
	}
	if err != nil {
		return 0, errs.ErrUnavailable
	}
	if generation == 0 || generation > 9007199254740991 {
		return 0, errs.ErrForbidden
	}
	return generation, nil
}

// acceptWorkerGrantInstance requires canonical nonzero instance UUID, lowercase SHA-256 and
// issue-time-derived revision, then atomically accepts the shared generation floor and exact
// per-instance watermark. SQL enforces validity and monotonic/replay constraints; mismatched
// returned generation/revision is Conflict. It commits only after both checks and does not verify
// signatures or mint new grants itself.
// acceptWorkerGrantInstance требует канонический ненулевой UUID instance, строчный SHA-256 и
// ревизию из времени выпуска, затем атомарно допускает общий generation floor и точный watermark
// instance. SQL проверяет срок и монотонность либо точный replay; несовпадение возвращённых
// generation/revision даёт Conflict. Завершает транзакцию после обеих проверок, сам не проверяет
// подписи и не выпускает grants.
func (repository *Repository) acceptWorkerGrantInstance(ctx context.Context, input platformrepo.WorkerGrantInput) error {
	instance, err := uuid.Parse(input.InstanceID)
	digest, digestErr := hex.DecodeString(input.EnvelopeSHA256)
	if err != nil || instance == uuid.Nil || instance.String() != input.InstanceID ||
		digestErr != nil || len(digest) != 32 || strings.ToLower(input.EnvelopeSHA256) != input.EnvelopeSHA256 ||
		input.IssuedAt.Unix() <= 0 || input.Revision != uint64(input.IssuedAt.Unix()) {
		return errs.ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	// Общая строка блокирует также прежних v1 consumers. Новый instance не
	// обходит generation floor; его revision не инвалидирует соседние Pod.
	var generation uint64
	err = tx.QueryRow(ctx, queryWorkerGrantAcceptGeneration, input.WorkloadID,
		input.CredentialGeneration, input.IssuedAt.UTC(), input.ExpiresAt.UTC()).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if generation != input.CredentialGeneration {
		return errs.ErrConflict
	}
	var accepted uint64
	err = tx.QueryRow(ctx, queryWorkerGrantAcceptInstance, input.WorkloadID, instance,
		input.CredentialGeneration, input.Revision, input.IssuedAt.UTC(), input.ExpiresAt.UTC(), input.EnvelopeSHA256).Scan(&accepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if accepted != input.Revision {
		return errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
