// Package gateway реализует единственный PostgreSQL owner transport state.
package gateway

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/repository/gateway"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool   *pgxpool.Pool
	cipher cipher.AEAD
	config Config
}

type Config struct {
	EncryptionKey       []byte
	PrincipalName       string
	PrincipalGeneration uint64
	ContextKeyID        string
	ContextSigningKey   []byte
	ContextTTL          time.Duration
	CleanupBase         context.Context
	CleanupTimeout      time.Duration
}

type scope struct {
	OrganizationID string
	ProjectID      string
	ActorID        string
}

type rowScanner interface {
	Scan(...any) error
}

func New(pool *pgxpool.Pool, config Config) (*Repository, error) {
	if pool == nil || len(config.EncryptionKey) != 32 || config.PrincipalName == "" ||
		config.PrincipalGeneration == 0 || config.ContextKeyID == "" || len(config.ContextSigningKey) < 32 ||
		config.ContextTTL < time.Second || config.ContextTTL > 10*time.Second ||
		config.CleanupBase == nil || config.CleanupTimeout < time.Second || config.CleanupTimeout > time.Minute {
		return nil, errors.New("interaction repository configuration is invalid")
	}
	block, err := aes.NewCipher(config.EncryptionKey)
	if err != nil {
		return nil, errors.New("create interaction repository cipher")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("create interaction repository AEAD")
	}
	return &Repository{pool: pool, cipher: aead, config: config}, nil
}

func (repository *Repository) Check(ctx context.Context) error {
	var version uint64
	var identityReady bool
	if err := repository.pool.QueryRow(ctx, readinessCheckSQL, repository.config.PrincipalGeneration,
		repository.config.ContextKeyID, digest(repository.config.ContextSigningKey)).Scan(&version, &identityReady); err != nil ||
		version != 1 || !identityReady {
		return errors.New("interaction repository is not ready")
	}
	organizationID, projectID, otherOrganizationID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	channelID := uuid.NewString()
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite})
	if err != nil {
		return errors.New("begin interaction repository readiness transaction")
	}
	if err := repository.activateScope(ctx, tx, scope{OrganizationID: organizationID, ProjectID: projectID,
		ActorID: "system:interaction-readiness"}); err != nil {
		return errors.Join(err, repository.rollback(tx))
	}
	negative, err := tx.Begin(ctx)
	if err != nil {
		return errors.Join(errors.New("begin interaction repository negative readiness probe"), repository.rollback(tx))
	}
	var negativeOrganizationID, negativeProjectID string
	var negativeCursor int64
	negativeErr := negative.QueryRow(ctx, readinessProbeCursorSQL, channelID, int64(1),
		otherOrganizationID, projectID).Scan(&negativeOrganizationID, &negativeProjectID, &negativeCursor)
	rollbackErr := negative.Rollback(ctx)
	if negativeErr == nil || (rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed)) {
		return errors.Join(errors.New("cross-tenant interaction repository readiness probe was not rejected"), repository.rollback(tx))
	}
	var storedOrganizationID, storedProjectID string
	var storedCursor int64
	if err := tx.QueryRow(ctx, readinessProbeCursorSQL, channelID, int64(2), organizationID, projectID).Scan(
		&storedOrganizationID, &storedProjectID, &storedCursor,
	); err != nil || storedOrganizationID != organizationID || storedProjectID != projectID || storedCursor != 2 {
		return errors.Join(errors.New("scoped interaction repository DML is not ready"), repository.rollback(tx))
	}
	if err := repository.rollback(tx); err != nil {
		return errors.New("rollback interaction repository readiness transaction")
	}
	return nil
}

func (repository *Repository) ClaimInbound(ctx context.Context, inbound entity.InboundEvent, lease time.Duration) (entity.InboundEvent, domainrepo.InboundDisposition, error) {
	leaseToken, err := newLeaseToken()
	if err != nil {
		return entity.InboundEvent{}, 0, err
	}
	payload, err := json.Marshal(inbound)
	if err != nil {
		return entity.InboundEvent{}, 0, errors.New("encode inbound event")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.InboundEvent{}, 0, errors.New("begin inbound transaction")
	}
	defer tx.Rollback(ctx)
	if err := repository.activateScope(ctx, tx, eventScope(inbound, "mattermost:"+inbound.ActorID)); err != nil {
		return entity.InboundEvent{}, 0, err
	}
	tag, err := tx.Exec(ctx, inboundInsertSQL, inbound.ID, inbound.ProviderEventID, inbound.Kind,
		inbound.Revision, payload, inbound.DigestSHA256, inbound.OrganizationID, inbound.ProjectID, interval(lease),
		"transport", digest([]byte(leaseToken)))
	if err != nil {
		return entity.InboundEvent{}, 0, errors.New("insert inbound event")
	}
	stored, leaseActive, err := scanInbound(tx.QueryRow(ctx, inboundLockSQL, inbound.ProviderEventID))
	if err != nil || stored.DigestSHA256 != inbound.DigestSHA256 {
		return entity.InboundEvent{}, 0, errors.New("inbound event idempotency conflict")
	}
	disposition := domainrepo.InboundClaimed
	if tag.RowsAffected() != 0 {
		stored.LeaseToken = leaseToken
	} else {
		switch stored.State {
		case enum.InboundCompleted, enum.InboundIgnored, enum.InboundFailed:
			disposition = domainrepo.InboundReplay
		case enum.InboundProcessing:
			if leaseActive {
				disposition = domainrepo.InboundBusy
			} else {
				tag, err = tx.Exec(ctx, inboundReclaimSQL, stored.ID, interval(lease), "transport", digest([]byte(leaseToken)))
				if err != nil {
					return entity.InboundEvent{}, 0, errors.New("reclaim inbound event")
				}
				if tag.RowsAffected() != 1 {
					disposition = domainrepo.InboundBusy
				}
			}
		case enum.InboundWaitingScan, enum.InboundWaitingCleanup:
			if stored.SemanticOutcome != "" {
				disposition = domainrepo.InboundReplay
			} else {
				disposition = domainrepo.InboundBusy
			}
		case enum.InboundPending:
			tag, err = tx.Exec(ctx, inboundReclaimSQL, stored.ID, interval(lease), "transport", digest([]byte(leaseToken)))
			if err != nil {
				return entity.InboundEvent{}, 0, errors.New("claim inbound event")
			}
			if tag.RowsAffected() != 1 {
				disposition = domainrepo.InboundBusy
			}
		default:
			disposition = domainrepo.InboundBusy
		}
		if disposition == domainrepo.InboundClaimed {
			stored, _, err = scanInbound(tx.QueryRow(ctx, inboundLockSQL, inbound.ProviderEventID))
			if err != nil {
				return entity.InboundEvent{}, 0, errors.New("read reclaimed inbound event")
			}
			stored.LeaseToken = leaseToken
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.InboundEvent{}, 0, errors.New("commit inbound transaction")
	}
	return stored, disposition, nil
}

func (repository *Repository) SaveInboundProgress(ctx context.Context, inbound entity.InboundEvent) error {
	payload, err := json.Marshal(inbound)
	if err != nil {
		return errors.New("encode inbound progress")
	}
	attachments, err := json.Marshal(inbound.AttachmentArtifacts)
	if err != nil {
		return errors.New("encode inbound artifacts")
	}
	return repository.withScope(ctx, eventScope(inbound, "system:interaction-inbound"), pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, execErr := tx.Exec(ctx, inboundSaveSQL, inbound.ID, payload, inbound.SessionID,
			inbound.PromptArtifactID, attachments, inbound.State, inbound.NextAttemptAt,
			inbound.Fence, digest([]byte(inbound.LeaseToken)), inbound.SemanticOutcome,
			inbound.ResponseMessage, inbound.NextAction)
		if execErr != nil || tag.RowsAffected() != 1 {
			return errors.New("save inbound progress")
		}
		return nil
	})
}

func (repository *Repository) CompleteInbound(ctx context.Context, inbound entity.InboundEvent, sessionID, turnID, message string) error {
	return repository.withScope(ctx, eventScope(inbound, "system:interaction-inbound"), pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, inboundCompleteSQL, inbound.ID, sessionID, turnID,
			inbound.Fence, digest([]byte(inbound.LeaseToken)), message)
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("complete inbound event")
		}
		return nil
	})
}

func (repository *Repository) RetryInbound(ctx context.Context, inbound entity.InboundEvent, code, message, nextAction string, next time.Time, terminal bool) error {
	return repository.withScope(ctx, eventScope(inbound, "system:interaction-inbound"), pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, inboundRetrySQL, inbound.ID, code, next, terminal,
			inbound.Fence, digest([]byte(inbound.LeaseToken)), message, nextAction)
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("retry inbound event")
		}
		return nil
	})
}

func (repository *Repository) ClaimWaitingInbound(ctx context.Context, lease time.Duration) (entity.InboundEvent, bool, error) {
	leaseToken, err := newLeaseToken()
	if err != nil {
		return entity.InboundEvent{}, false, err
	}
	workScope, ok, err := repository.nextWorkScope(ctx, "INBOUND")
	if err != nil || !ok {
		return entity.InboundEvent{}, false, err
	}
	var inbound entity.InboundEvent
	err = repository.withScope(ctx, workScope, pgx.ReadWrite, func(tx pgx.Tx) error {
		var scanErr error
		inbound, _, scanErr = scanInbound(tx.QueryRow(ctx, inboundClaimWaitingSQL,
			interval(lease), "worker", digest([]byte(leaseToken))))
		return scanErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.InboundEvent{}, false, nil
	}
	if err != nil {
		return entity.InboundEvent{}, false, errors.New("claim waiting inbound event")
	}
	inbound.LeaseToken = leaseToken
	return inbound, true, nil
}

func (repository *Repository) LoadCursors(ctx context.Context, boundaries []entity.Boundary) (map[string]int64, error) {
	result := make(map[string]int64, len(boundaries))
	for _, boundary := range boundaries {
		err := repository.withScope(ctx, scope{OrganizationID: boundary.OrganizationID, ProjectID: boundary.ProjectID,
			ActorID: "system:interaction-cursor"}, pgx.ReadOnly, func(tx pgx.Tx) error {
			rows, queryErr := tx.Query(ctx, cursorLoadSQL, []string{boundary.ChannelID})
			if queryErr != nil {
				return errors.New("load Mattermost cursors")
			}
			defer rows.Close()
			for rows.Next() {
				var channel string
				var cursor int64
				if rows.Scan(&channel, &cursor) != nil {
					return errors.New("scan Mattermost cursor")
				}
				result[channel] = cursor
			}
			return rows.Err()
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (repository *Repository) AdvanceCursor(ctx context.Context, boundary entity.Boundary, channel string, cursor int64) error {
	return repository.withScope(ctx, scope{OrganizationID: boundary.OrganizationID, ProjectID: boundary.ProjectID,
		ActorID: "system:interaction-cursor"}, pgx.ReadWrite, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, cursorAdvanceSQL, channel, cursor, boundary.OrganizationID, boundary.ProjectID)
		return err
	})
}

func (repository *Repository) HasDeletionPending(ctx context.Context, organizationID, projectID, chatID, sessionID string) (bool, error) {
	var pending bool
	err := repository.withScope(ctx, scope{OrganizationID: organizationID, ProjectID: projectID,
		ActorID: "system:interaction-lifecycle"}, pgx.ReadOnly, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, inboundDeletionPendingSQL, organizationID, projectID, chatID, sessionID).Scan(&pending)
	})
	if err != nil {
		return false, errors.New("read conversation deletion fence")
	}
	return pending, nil
}

func (repository *Repository) CancelDeletion(ctx context.Context, organizationID, projectID, chatID, sessionID, message string) error {
	return repository.withScope(ctx, scope{OrganizationID: organizationID, ProjectID: projectID,
		ActorID: "system:interaction-lifecycle"}, pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, inboundCancelDeletionSQL, organizationID, projectID, chatID, sessionID, message)
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("cancel conversation deletion")
		}
		return nil
	})
}

func (repository *Repository) ResolveThreadSession(ctx context.Context, organizationID, projectID, channelID, rootPostID string) (string, error) {
	var sessionID string
	err := repository.withScope(ctx, scope{OrganizationID: organizationID, ProjectID: projectID,
		ActorID: "system:interaction-resolution"}, pgx.ReadOnly, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, inboundThreadSessionSQL, projectID, channelID, rootPostID).Scan(&sessionID)
	})
	if err != nil {
		return "", errors.New("resolve server-owned Mattermost thread session")
	}
	return sessionID, nil
}

func (repository *Repository) EnqueueDelivery(ctx context.Context, delivery entity.Delivery) (entity.Delivery, bool, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.Delivery{}, false, errors.New("begin delivery transaction")
	}
	defer tx.Rollback(ctx)
	if err := repository.activateScope(ctx, tx, deliveryEntityScope(delivery, "system:interaction-enqueue")); err != nil {
		return entity.Delivery{}, false, err
	}
	inserted, err := repository.insertDelivery(ctx, tx, delivery)
	if err != nil {
		return entity.Delivery{}, false, err
	}
	stored, err := repository.getDelivery(ctx, tx, delivery.ID)
	if err != nil || stored.PayloadSHA256 != delivery.PayloadSHA256 || stored.OrganizationID != delivery.OrganizationID ||
		stored.ProjectID != delivery.ProjectID || stored.SessionID != delivery.SessionID || stored.TurnID != delivery.TurnID ||
		stored.Attempt != delivery.Attempt || stored.ImmutableInputSHA256 != delivery.ImmutableInputSHA256 ||
		stored.TeamID != delivery.TeamID || stored.ChannelID != delivery.ChannelID || stored.RootPostID != delivery.RootPostID ||
		stored.BotStableKey != delivery.BotStableKey || stored.Locale != delivery.Locale || stored.Kind != delivery.Kind ||
		stored.UpdatePostID != delivery.UpdatePostID || !reflect.DeepEqual(stored.Attachments, delivery.Attachments) {
		return entity.Delivery{}, false, errors.New("delivery idempotency conflict")
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.Delivery{}, false, errors.New("commit delivery transaction")
	}
	return stored, inserted, nil
}

func (repository *Repository) ClaimDelivery(ctx context.Context, instanceID, token string, lease time.Duration) (entity.Delivery, bool, error) {
	if instanceID == "" || len(instanceID) > 128 {
		return entity.Delivery{}, false, errors.New("delivery lease owner is invalid")
	}
	workScope, ok, err := repository.nextWorkScope(ctx, "DELIVERY")
	if err != nil || !ok {
		return entity.Delivery{}, false, err
	}
	tokenDigest := digest([]byte(token))
	var delivery entity.Delivery
	err = repository.withScope(ctx, workScope, pgx.ReadWrite, func(tx pgx.Tx) error {
		var id string
		if claimErr := tx.QueryRow(ctx, deliveryClaimSQL, tokenDigest, interval(lease), instanceID).Scan(&id); claimErr != nil {
			return claimErr
		}
		var getErr error
		delivery, getErr = repository.getDelivery(ctx, tx, id)
		return getErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Delivery{}, false, nil
	}
	if err != nil {
		return entity.Delivery{}, false, errors.New("claim interaction delivery")
	}
	delivery.LeaseToken = token
	return delivery, true, nil
}

func (repository *Repository) MarkProviderAccepted(ctx context.Context, delivery entity.Delivery, postID, receipt, rootID string) error {
	return repository.withScope(ctx, deliveryEntityScope(delivery, "system:interaction-delivery"), pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, deliveryAcceptedSQL, delivery.ID, delivery.Fence,
			digest([]byte(delivery.LeaseToken)), postID, receipt, rootID)
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("record Mattermost provider receipt")
		}
		return nil
	})
}

func (repository *Repository) GetUploadReceipt(ctx context.Context, delivery entity.Delivery, artifactID string) (entity.UploadReceipt, bool, error) {
	var receipt entity.UploadReceipt
	err := repository.withScope(ctx, deliveryEntityScope(delivery, "system:interaction-delivery"), pgx.ReadOnly, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, uploadReceiptGetSQL, delivery.ID, artifactID).Scan(
			&receipt.DeliveryID, &receipt.ArtifactID, &receipt.ProviderFileID, &receipt.ChannelID,
			&receipt.Name, &receipt.SizeBytes, &receipt.MediaType, &receipt.SHA256, &receipt.CreatedAt,
		)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.UploadReceipt{}, false, nil
	}
	if err != nil {
		return entity.UploadReceipt{}, false, errors.New("read Mattermost upload receipt")
	}
	return receipt, true, nil
}

func (repository *Repository) SaveUploadReceipt(ctx context.Context, delivery entity.Delivery, receipt entity.UploadReceipt) error {
	if receipt.DeliveryID != delivery.ID {
		return errors.New("mattermost upload receipt delivery mismatch")
	}
	if err := repository.withScope(ctx, deliveryEntityScope(delivery, "system:interaction-delivery"), pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, execErr := tx.Exec(ctx, uploadReceiptSaveSQL,
			delivery.ID, receipt.ArtifactID, receipt.ProviderFileID, receipt.ChannelID,
			receipt.Name, receipt.SizeBytes, receipt.MediaType, receipt.SHA256,
			delivery.Fence, digest([]byte(delivery.LeaseToken)))
		if execErr != nil || tag.RowsAffected() != 1 {
			return errors.New("save fenced Mattermost upload receipt")
		}
		return nil
	}); err != nil {
		return errors.New("save Mattermost upload receipt")
	}
	stored, ok, err := repository.GetUploadReceipt(ctx, delivery, receipt.ArtifactID)
	if err != nil || !ok || stored.ProviderFileID != receipt.ProviderFileID ||
		stored.ChannelID != receipt.ChannelID || stored.Name != receipt.Name ||
		stored.SizeBytes != receipt.SizeBytes || stored.MediaType != receipt.MediaType || stored.SHA256 != receipt.SHA256 {
		return errors.New("verify Mattermost upload receipt")
	}
	return nil
}

func (repository *Repository) CompleteDelivery(ctx context.Context, delivery entity.Delivery) error {
	return repository.withScope(ctx, deliveryEntityScope(delivery, "system:interaction-delivery"), pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, deliveryCompleteSQL, delivery.ID, delivery.Fence, digest([]byte(delivery.LeaseToken)))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("complete interaction delivery")
		}
		return nil
	})
}

func (repository *Repository) RetryDelivery(ctx context.Context, delivery entity.Delivery, code string, next time.Time, terminal bool) error {
	return repository.withScope(ctx, deliveryEntityScope(delivery, "system:interaction-delivery"), pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, deliveryRetrySQL, delivery.ID, delivery.Fence, code, next, terminal,
			digest([]byte(delivery.LeaseToken)))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("retry interaction delivery")
		}
		return nil
	})
}

func (repository *Repository) GetDelivery(ctx context.Context, id string) (entity.Delivery, error) {
	value, err := repository.deliveryScope(ctx, id)
	if err != nil {
		return entity.Delivery{}, err
	}
	var delivery entity.Delivery
	err = repository.withScope(ctx, value, pgx.ReadOnly, func(tx pgx.Tx) error {
		var getErr error
		delivery, getErr = repository.getDelivery(ctx, tx, id)
		return getErr
	})
	return delivery, err
}

func (repository *Repository) GetDeliveryScoped(ctx context.Context, organizationID, projectID, id string) (entity.Delivery, error) {
	var delivery entity.Delivery
	err := repository.withScope(ctx, scope{OrganizationID: organizationID, ProjectID: projectID,
		ActorID: "system:interaction-readback"}, pgx.ReadOnly, func(tx pgx.Tx) error {
		var getErr error
		delivery, getErr = repository.getDeliveryWithSQL(ctx, tx, deliveryGetScopedSQL, id, organizationID, projectID)
		if getErr != nil {
			return getErr
		}
		delivery.UploadReceipts, getErr = repository.listUploadReceipts(ctx, tx, id)
		return getErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Delivery{}, domainrepo.ErrNotFound
	}
	return delivery, err
}

func (repository *Repository) listUploadReceipts(ctx context.Context, source pgx.Tx, deliveryID string) ([]entity.UploadReceipt, error) {
	rows, err := source.Query(ctx, uploadReceiptListSQL, deliveryID)
	if err != nil {
		return nil, errors.New("list Mattermost upload receipts")
	}
	defer rows.Close()
	receipts := make([]entity.UploadReceipt, 0, 4)
	for rows.Next() {
		var receipt entity.UploadReceipt
		if err := rows.Scan(&receipt.DeliveryID, &receipt.ArtifactID, &receipt.ProviderFileID,
			&receipt.ChannelID, &receipt.Name, &receipt.SizeBytes, &receipt.MediaType,
			&receipt.SHA256, &receipt.CreatedAt); err != nil {
			return nil, errors.New("scan Mattermost upload receipt")
		}
		receipts = append(receipts, receipt)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("read Mattermost upload receipts")
	}
	return receipts, nil
}

func (repository *Repository) GetDeliveryByProviderPost(ctx context.Context, postID string) (entity.Delivery, error) {
	value, err := repository.deliveryScopeByPost(ctx, postID)
	if err != nil {
		return entity.Delivery{}, err
	}
	var id string
	var delivery entity.Delivery
	err = repository.withScope(ctx, value, pgx.ReadOnly, func(tx pgx.Tx) error {
		if queryErr := tx.QueryRow(ctx, deliveryGetByPostSQL, postID).Scan(&id); queryErr != nil {
			return queryErr
		}
		var getErr error
		delivery, getErr = repository.getDelivery(ctx, tx, id)
		return getErr
	})
	return delivery, err
}

func (repository *Repository) ListPendingReactionPosts(ctx context.Context, boundaries []entity.Boundary, limit int) (map[string]string, error) {
	if limit < 1 || limit > 1024 {
		return nil, errors.New("reaction catch-up limit is invalid")
	}
	result := make(map[string]string, limit)
	for _, boundary := range boundaries {
		err := repository.withScope(ctx, scope{OrganizationID: boundary.OrganizationID, ProjectID: boundary.ProjectID,
			ActorID: "system:interaction-reaction-catchup"}, pgx.ReadOnly, func(tx pgx.Tx) error {
			rows, queryErr := tx.Query(ctx, gateDeliveryReactionPostsSQL, limit+1)
			if queryErr != nil {
				return errors.New("list pending reaction posts")
			}
			defer rows.Close()
			for rows.Next() {
				var postID, channelID string
				if rows.Scan(&postID, &channelID) != nil {
					return errors.New("scan pending reaction post")
				}
				if len(result) == limit {
					return errors.New("pending reaction post limit exceeded")
				}
				result[postID] = channelID
			}
			return rows.Err()
		})
		if err != nil {
			return nil, errors.New("read pending reaction posts")
		}
	}
	return result, nil
}

func (repository *Repository) MarkOwnerGateDecided(ctx context.Context, delivery entity.Delivery) error {
	return repository.withScope(ctx, deliveryEntityScope(delivery, "system:interaction-owner-decision"), pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, gateDeliveryDecidedSQL, delivery.ID)
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("mark owner gate delivery decided")
		}
		return nil
	})
}

func (repository *Repository) SaveTurnWatch(ctx context.Context, inbound entity.InboundEvent, turnID string) error {
	return repository.withScope(ctx, eventScope(inbound, "system:interaction-turn-watch"), pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, turnWatchSaveSQL, inbound.ID, turnID, inbound.Fence,
			digest([]byte(inbound.LeaseToken)))
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("save control-plane turn watch")
		}
		return nil
	})
}

func (repository *Repository) ClaimTurnWatch(ctx context.Context, instanceID, token string, lease time.Duration) (entity.TurnWatch, bool, error) {
	var watch entity.TurnWatch
	var inboundPayload []byte
	workScope, ok, err := repository.nextWorkScope(ctx, "TURN_WATCH")
	if err != nil || !ok {
		return entity.TurnWatch{}, false, err
	}
	err = repository.withScope(ctx, workScope, pgx.ReadWrite, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, turnWatchClaimSQL, instanceID, digest([]byte(token)), interval(lease)).Scan(
			&watch.TurnID, &watch.LastVersion, &watch.Fence, &watch.LeaseExpiresAt, &inboundPayload,
		)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.TurnWatch{}, false, nil
	}
	if err != nil || json.Unmarshal(inboundPayload, &watch.Inbound) != nil || watch.Inbound.ID == "" {
		return entity.TurnWatch{}, false, errors.New("claim control-plane turn watch")
	}
	watch.LeaseToken = token
	return watch, true, nil
}

func (repository *Repository) AdvanceTurnWatch(ctx context.Context, watch entity.TurnWatch, version uint64, terminal bool, next time.Time) error {
	return repository.withScope(ctx, eventScope(watch.Inbound, "system:interaction-turn-watch"), pgx.ReadWrite, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, turnWatchAdvanceSQL, watch.TurnID, watch.Fence,
			digest([]byte(watch.LeaseToken)), version, terminal, next)
		if err != nil || tag.RowsAffected() != 1 {
			return errors.New("advance control-plane turn watch")
		}
		return nil
	})
}

func (repository *Repository) ClaimOwnerGateRequest(ctx context.Context) (string, bool, error) {
	var key string
	var nullableKey sql.NullString
	if err := repository.pool.QueryRow(ctx, gateClaimPendingSQL).Scan(&nullableKey); err != nil {
		return "", false, errors.New("read owner gate claim request")
	}
	if !nullableKey.Valid || nullableKey.String == "" {
		return "", false, nil
	}
	key = nullableKey.String
	return key, true, nil
}

func (repository *Repository) SaveOwnerGateClaim(ctx context.Context, key string, delivery entity.Delivery) error {
	if delivery.OwnerGate == nil {
		return errors.New("owner gate delivery binding is missing")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return errors.New("begin owner gate binding transaction")
	}
	defer tx.Rollback(ctx)
	if err := repository.activateScope(ctx, tx, deliveryEntityScope(delivery, "system:interaction-owner-gate")); err != nil {
		return err
	}
	inserted, err := repository.insertDelivery(ctx, tx, delivery)
	if err != nil {
		return err
	}
	if !inserted {
		claimCipher, encryptErr := repository.encrypt([]byte(delivery.OwnerGate.ClaimToken), []byte(delivery.ID))
		if encryptErr != nil {
			return encryptErr
		}
		tag, rebindErr := tx.Exec(ctx, gateDeliveryRebindSQL,
			delivery.ID, delivery.OwnerGate.GateID, delivery.OwnerGate.GateVersion,
			delivery.OwnerGate.ProcessRunID, delivery.OwnerGate.ProcessVersion, claimCipher,
			delivery.OwnerGate.ClaimFence, delivery.OwnerGate.ClaimExpiresAt,
			delivery.OwnerGate.RecipientActorID, delivery.OwnerGate.DeliveryPayloadSHA256,
			delivery.PayloadSHA256,
		)
		if rebindErr != nil || tag.RowsAffected() != 1 {
			return errors.New("rebind owner gate delivery claim")
		}
	}
	var bound bool
	err = tx.QueryRow(ctx, gateClaimBindSQL, key, delivery.OwnerGate.GateID, delivery.ID).Scan(&bound)
	if err != nil || !bound {
		return errors.New("bind owner gate claim request")
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("commit owner gate binding transaction")
	}
	return nil
}

func (repository *Repository) CompleteOwnerGateClaim(ctx context.Context, key string) error {
	var completed bool
	err := repository.pool.QueryRow(ctx, gateClaimCompleteSQL, key).Scan(&completed)
	if err != nil || !completed {
		return errors.New("complete owner gate claim request")
	}
	return nil
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (repository *Repository) insertDelivery(ctx context.Context, tx pgx.Tx, delivery entity.Delivery) (bool, error) {
	payload := []byte(delivery.Payload)
	attachments, err := json.Marshal(delivery.Attachments)
	if err != nil {
		return false, errors.New("encode delivery attachments")
	}
	var gateID, processID, recipient, gatePayloadDigest string
	var gateVersion, processVersion, claimFence uint64
	var claimCipher []byte
	var claimExpires *time.Time
	if delivery.OwnerGate != nil {
		gateID, gateVersion = delivery.OwnerGate.GateID, delivery.OwnerGate.GateVersion
		processID, processVersion = delivery.OwnerGate.ProcessRunID, delivery.OwnerGate.ProcessVersion
		claimFence, recipient = delivery.OwnerGate.ClaimFence, delivery.OwnerGate.RecipientActorID
		claimExpires = &delivery.OwnerGate.ClaimExpiresAt
		gatePayloadDigest = delivery.OwnerGate.DeliveryPayloadSHA256
		claimCipher, err = repository.encrypt([]byte(delivery.OwnerGate.ClaimToken), []byte(delivery.ID))
		if err != nil {
			return false, err
		}
	}
	tag, err := tx.Exec(ctx, deliveryInsertSQL,
		delivery.ID, delivery.Kind, delivery.OrganizationID, delivery.ProjectID,
		delivery.SessionID, delivery.TurnID, delivery.Attempt, delivery.ImmutableInputSHA256,
		delivery.TeamID, delivery.ChannelID, delivery.RootPostID, delivery.BotStableKey,
		delivery.Locale, payload, delivery.PayloadSHA256, attachments,
		gateID, gateVersion, processID, processVersion, claimCipher, claimFence, claimExpires, recipient, gatePayloadDigest,
		delivery.UpdatePostID,
	)
	if err != nil {
		return false, errors.New("insert interaction delivery")
	}
	return tag.RowsAffected() == 1, nil
}

func (repository *Repository) getDelivery(ctx context.Context, source queryer, id string) (entity.Delivery, error) {
	return repository.getDeliveryWithSQL(ctx, source, deliveryGetSQL, id)
}

func (repository *Repository) getDeliveryWithSQL(ctx context.Context, source queryer, statement string, arguments ...any) (entity.Delivery, error) {
	var delivery entity.Delivery
	var kind, state string
	var sessionID, turnID, gateID, processID, recipient sql.NullString
	var leaseExpires, claimExpires, recordedAt sql.NullTime
	var payload, attachments, claimCipher []byte
	var gatePayloadDigest string
	var gateVersion, processVersion, claimFence uint64
	err := source.QueryRow(ctx, statement, arguments...).Scan(
		&delivery.ID, &kind, &state, &delivery.OrganizationID, &delivery.ProjectID,
		&sessionID, &turnID, &delivery.Attempt, &delivery.ImmutableInputSHA256,
		&delivery.TeamID, &delivery.ChannelID, &delivery.RootPostID, &delivery.BotStableKey,
		&delivery.Locale, &payload, &delivery.PayloadSHA256, &attachments,
		&delivery.ProviderPostID, &delivery.ProviderReceiptSHA256, &delivery.UpdatePostID, &delivery.Attempts, &delivery.AckAttempts,
		&delivery.Fence, &leaseExpires, &delivery.NextAttemptAt, &delivery.LastErrorCode,
		&gateID, &gateVersion, &processID, &processVersion, &claimCipher, &claimFence,
		&claimExpires, &recipient, &gatePayloadDigest, &recordedAt, &delivery.CreatedAt, &delivery.UpdatedAt,
	)
	if err != nil {
		return entity.Delivery{}, err
	}
	delivery.Kind, delivery.State, delivery.Payload = enum.DeliveryKind(kind), enum.DeliveryState(state), payload
	delivery.SessionID, delivery.TurnID = sessionID.String, turnID.String
	if leaseExpires.Valid {
		delivery.LeaseExpiresAt = leaseExpires.Time
	}
	if json.Unmarshal(attachments, &delivery.Attachments) != nil {
		return entity.Delivery{}, errors.New("decode delivery attachments")
	}
	if gateID.Valid {
		if !claimExpires.Valid || !recipient.Valid || gatePayloadDigest == "" {
			return entity.Delivery{}, errors.New("decrypt owner gate claim")
		}
		var claimToken []byte
		if len(claimCipher) > 0 {
			var decryptErr error
			claimToken, decryptErr = repository.decrypt(claimCipher, []byte(delivery.ID))
			if decryptErr != nil {
				return entity.Delivery{}, errors.New("decrypt owner gate claim")
			}
		} else if delivery.State != enum.DeliveryDelivered {
			return entity.Delivery{}, errors.New("owner gate claim is unavailable")
		}
		delivery.OwnerGate = &entity.OwnerGateBinding{
			GateID: gateID.String, GateVersion: gateVersion, ProcessRunID: processID.String,
			ProcessVersion: processVersion, ClaimToken: string(claimToken), ClaimFence: claimFence,
			ClaimExpiresAt: claimExpires.Time, RecipientActorID: recipient.String,
			DeliveryPayloadSHA256: gatePayloadDigest,
		}
		if recordedAt.Valid {
			delivery.OwnerGate.DeliveryRecordedAt = recordedAt.Time
		}
	}
	return delivery, nil
}

func scanInbound(row rowScanner) (entity.InboundEvent, bool, error) {
	var result entity.InboundEvent
	var kind, state string
	var payload, attachments []byte
	var sessionID, artifactID sql.NullString
	var processingExpires sql.NullTime
	var attempts, scanPolls uint32
	var fence uint64
	var leaseActive bool
	var nextAttemptAt, createdAt, updatedAt time.Time
	var id, providerEventID, payloadDigest, organizationID, projectID string
	var revision uint64
	err := row.Scan(
		&id, &providerEventID, &kind, &revision, &payload,
		&payloadDigest, &state, &organizationID, &projectID,
		&sessionID, &artifactID, &attachments, &attempts, &scanPolls, &fence, &nextAttemptAt,
		&createdAt, &updatedAt, &processingExpires, &leaseActive,
		&result.SemanticOutcome, &result.ResponseMessage, &result.TerminalErrorCode, &result.NextAction,
	)
	if err != nil {
		return entity.InboundEvent{}, false, err
	}
	if json.Unmarshal(payload, &result) != nil || json.Unmarshal(attachments, &result.AttachmentArtifacts) != nil {
		return entity.InboundEvent{}, false, errors.New("decode inbound event")
	}
	if result.ID != id || result.ProviderEventID != providerEventID || string(result.Kind) != kind ||
		result.Revision != revision || result.DigestSHA256 != payloadDigest ||
		result.OrganizationID != organizationID || result.ProjectID != projectID {
		return entity.InboundEvent{}, false, errors.New("inbound event projection mismatch")
	}
	result.Kind, result.State = enum.InboundKind(kind), enum.InboundState(state)
	result.SessionID, result.PromptArtifactID = sessionID.String, artifactID.String
	result.Attempts, result.ScanPolls, result.Fence, result.NextAttemptAt = attempts, scanPolls, fence, nextAttemptAt
	result.CreatedAt, result.UpdatedAt = createdAt, updatedAt
	if processingExpires.Valid {
		result.LeaseExpiresAt = processingExpires.Time
	}
	return result, leaseActive, nil
}

func (repository *Repository) encrypt(plaintext, additional []byte) ([]byte, error) {
	nonce := make([]byte, repository.cipher.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, errors.New("create owner gate claim nonce")
	}
	return repository.cipher.Seal(nonce, nonce, plaintext, additional), nil
}

func (repository *Repository) decrypt(ciphertext, additional []byte) ([]byte, error) {
	if len(ciphertext) <= repository.cipher.NonceSize() {
		return nil, errors.New("owner gate claim ciphertext is invalid")
	}
	nonce := ciphertext[:repository.cipher.NonceSize()]
	plaintext, err := repository.cipher.Open(nil, nonce, ciphertext[repository.cipher.NonceSize():], additional)
	if err != nil {
		return nil, errors.New("decrypt owner gate claim")
	}
	return plaintext, nil
}

func (repository *Repository) withScope(
	ctx context.Context,
	value scope,
	access pgx.TxAccessMode,
	callback func(pgx.Tx) error,
) error {
	if value.OrganizationID == "" || value.ProjectID == "" || value.ActorID == "" {
		return errors.New("interaction repository scope is invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: access})
	if err != nil {
		return errors.New("begin scoped interaction transaction")
	}
	if err := repository.activateScope(ctx, tx, value); err != nil {
		return errors.Join(err, repository.rollback(tx))
	}
	if err := callback(tx); err != nil {
		return errors.Join(err, repository.rollback(tx))
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.Join(errors.New("commit scoped interaction transaction"), repository.rollback(tx))
	}
	return nil
}

func (repository *Repository) activateScope(ctx context.Context, tx pgx.Tx, value scope) error {
	nonce := uuid.NewString()
	expires := time.Now().UTC().Add(repository.config.ContextTTL).UnixMicro()
	canonical := "v1\n" + repository.config.PrincipalName + "\n" +
		strconv.FormatUint(repository.config.PrincipalGeneration, 10) + "\n" + value.OrganizationID + "\n" +
		value.ProjectID + "\n" + value.ActorID + "\n" + nonce + "\n" + strconv.FormatInt(expires, 10)
	mac := hmac.New(sha256.New, repository.config.ContextSigningKey)
	_, _ = mac.Write([]byte(canonical))
	if _, err := tx.Exec(ctx, transactionActivateScopeSQL, value.OrganizationID, value.ProjectID, value.ActorID,
		repository.config.PrincipalName, repository.config.PrincipalGeneration, repository.config.ContextKeyID,
		nonce, expires, mac.Sum(nil)); err != nil {
		return errors.New("activate signed interaction transaction scope")
	}
	return nil
}

func (repository *Repository) rollback(tx pgx.Tx) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(repository.config.CleanupBase), repository.config.CleanupTimeout)
	defer cancel()
	if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return fmt.Errorf("rollback scoped interaction transaction: %w", err)
	}
	return nil
}

func (repository *Repository) nextWorkScope(ctx context.Context, kind string) (scope, bool, error) {
	var value scope
	if err := repository.pool.QueryRow(ctx, nextWorkScopeSQL, kind).Scan(&value.OrganizationID, &value.ProjectID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return scope{}, false, nil
		}
		return scope{}, false, errors.New("resolve interaction work scope")
	}
	if value.OrganizationID == "" || value.ProjectID == "" {
		return scope{}, false, nil
	}
	value.ActorID = "system:interaction-gateway-worker"
	return value, true, nil
}

func (repository *Repository) deliveryScope(ctx context.Context, deliveryID string) (scope, error) {
	var value scope
	if err := repository.pool.QueryRow(ctx, deliveryScopeSQL, deliveryID).Scan(
		&value.OrganizationID, &value.ProjectID,
	); err != nil || value.OrganizationID == "" || value.ProjectID == "" {
		return scope{}, errors.New("resolve interaction delivery scope")
	}
	value.ActorID = "system:interaction-gateway-delivery"
	return value, nil
}

func (repository *Repository) deliveryScopeByPost(ctx context.Context, postID string) (scope, error) {
	var value scope
	if err := repository.pool.QueryRow(ctx, deliveryScopeByPostSQL, postID).Scan(
		&value.OrganizationID, &value.ProjectID,
	); err != nil || value.OrganizationID == "" || value.ProjectID == "" {
		return scope{}, errors.New("resolve Mattermost post delivery scope")
	}
	value.ActorID = "system:interaction-gateway-callback"
	return value, nil
}

func eventScope(inbound entity.InboundEvent, actor string) scope {
	return scope{OrganizationID: inbound.OrganizationID, ProjectID: inbound.ProjectID, ActorID: actor}
}

func deliveryEntityScope(delivery entity.Delivery, actor string) scope {
	return scope{OrganizationID: delivery.OrganizationID, ProjectID: delivery.ProjectID, ActorID: actor}
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func newLeaseToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", errors.New("create interaction lease token")
	}
	return hex.EncodeToString(raw), nil
}

func interval(value time.Duration) string { return value.String() }
