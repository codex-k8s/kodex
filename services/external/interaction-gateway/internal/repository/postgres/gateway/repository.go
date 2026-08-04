// Package gateway реализует единственный PostgreSQL owner transport state.
package gateway

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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
}

type rowScanner interface {
	Scan(...any) error
}

func New(pool *pgxpool.Pool, encryptionKey []byte) (*Repository, error) {
	if pool == nil || len(encryptionKey) != 32 {
		return nil, errors.New("interaction repository configuration is invalid")
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, errors.New("create interaction repository cipher")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("create interaction repository AEAD")
	}
	return &Repository{pool: pool, cipher: aead}, nil
}

func (repository *Repository) Check(ctx context.Context) error {
	var version uint64
	if err := repository.pool.QueryRow(ctx, readinessCheckSQL).Scan(&version); err != nil || version != 1 {
		return errors.New("interaction repository is not ready")
	}
	return nil
}

func (repository *Repository) ClaimInbound(ctx context.Context, inbound entity.InboundEvent, lease time.Duration) (entity.InboundEvent, domainrepo.InboundDisposition, error) {
	payload, err := json.Marshal(inbound)
	if err != nil {
		return entity.InboundEvent{}, 0, errors.New("encode inbound event")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.InboundEvent{}, 0, errors.New("begin inbound transaction")
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, inboundInsertSQL, inbound.ID, inbound.ProviderEventID, inbound.Kind,
		inbound.Revision, payload, inbound.DigestSHA256, inbound.OrganizationID, inbound.ProjectID, interval(lease))
	if err != nil {
		return entity.InboundEvent{}, 0, errors.New("insert inbound event")
	}
	stored, processingExpiresAt, err := scanInbound(tx.QueryRow(ctx, inboundLockSQL, inbound.ProviderEventID))
	if err != nil || stored.DigestSHA256 != inbound.DigestSHA256 {
		return entity.InboundEvent{}, 0, errors.New("inbound event idempotency conflict")
	}
	disposition := domainrepo.InboundClaimed
	if tag.RowsAffected() == 0 {
		switch stored.State {
		case enum.InboundCompleted, enum.InboundIgnored, enum.InboundFailed:
			disposition = domainrepo.InboundReplay
		case enum.InboundProcessing:
			if processingExpiresAt.Valid && processingExpiresAt.Time.After(time.Now()) {
				disposition = domainrepo.InboundBusy
			} else if _, err = tx.Exec(ctx, inboundReclaimSQL, stored.ID, interval(lease)); err != nil {
				return entity.InboundEvent{}, 0, errors.New("reclaim inbound event")
			}
		default:
			if _, err = tx.Exec(ctx, inboundReclaimSQL, stored.ID, interval(lease)); err != nil {
				return entity.InboundEvent{}, 0, errors.New("claim inbound event")
			}
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
	tag, err := repository.pool.Exec(ctx, inboundSaveSQL, inbound.ID, payload, inbound.SessionID,
		inbound.PromptArtifactID, attachments, inbound.State, inbound.NextAttemptAt)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("save inbound progress")
	}
	return nil
}

func (repository *Repository) CompleteInbound(ctx context.Context, id, sessionID, turnID string) error {
	tag, err := repository.pool.Exec(ctx, inboundCompleteSQL, id, sessionID, turnID)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("complete inbound event")
	}
	return nil
}

func (repository *Repository) RetryInbound(ctx context.Context, id, code string, next time.Time, terminal bool) error {
	tag, err := repository.pool.Exec(ctx, inboundRetrySQL, id, code, next, terminal)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("retry inbound event")
	}
	return nil
}

func (repository *Repository) ClaimWaitingInbound(ctx context.Context, lease time.Duration) (entity.InboundEvent, bool, error) {
	inbound, _, err := scanInbound(repository.pool.QueryRow(ctx, inboundClaimWaitingSQL, interval(lease)))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.InboundEvent{}, false, nil
	}
	if err != nil {
		return entity.InboundEvent{}, false, errors.New("claim waiting inbound event")
	}
	return inbound, true, nil
}

func (repository *Repository) LoadCursors(ctx context.Context, channels []string) (map[string]int64, error) {
	rows, err := repository.pool.Query(ctx, cursorLoadSQL, channels)
	if err != nil {
		return nil, errors.New("load Mattermost cursors")
	}
	defer rows.Close()
	result := make(map[string]int64, len(channels))
	for rows.Next() {
		var channel string
		var cursor int64
		if rows.Scan(&channel, &cursor) != nil {
			return nil, errors.New("scan Mattermost cursor")
		}
		result[channel] = cursor
	}
	return result, rows.Err()
}

func (repository *Repository) AdvanceCursor(ctx context.Context, channel string, cursor int64) error {
	_, err := repository.pool.Exec(ctx, cursorAdvanceSQL, channel, cursor)
	return err
}

func (repository *Repository) EnqueueDelivery(ctx context.Context, delivery entity.Delivery) (entity.Delivery, bool, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.Delivery{}, false, errors.New("begin delivery transaction")
	}
	defer tx.Rollback(ctx)
	inserted, err := repository.insertDelivery(ctx, tx, delivery)
	if err != nil {
		return entity.Delivery{}, false, err
	}
	stored, err := repository.getDelivery(ctx, tx, delivery.ID)
	if err != nil || stored.PayloadSHA256 != delivery.PayloadSHA256 || stored.ProjectID != delivery.ProjectID {
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
	tokenDigest := digest([]byte(token))
	var id string
	err := repository.pool.QueryRow(ctx, deliveryClaimSQL, tokenDigest, interval(lease), instanceID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Delivery{}, false, nil
	}
	if err != nil {
		return entity.Delivery{}, false, errors.New("claim interaction delivery")
	}
	delivery, err := repository.GetDelivery(ctx, id)
	if err != nil {
		return entity.Delivery{}, false, err
	}
	delivery.LeaseToken = token
	return delivery, true, nil
}

func (repository *Repository) MarkProviderAccepted(ctx context.Context, id string, fence uint64, token, postID, receipt, rootID string) error {
	tag, err := repository.pool.Exec(ctx, deliveryAcceptedSQL, id, fence, digest([]byte(token)), postID, receipt, rootID)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("record Mattermost provider receipt")
	}
	return nil
}

func (repository *Repository) CompleteDelivery(ctx context.Context, id string, fence uint64) error {
	tag, err := repository.pool.Exec(ctx, deliveryCompleteSQL, id, fence)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("complete interaction delivery")
	}
	return nil
}

func (repository *Repository) RetryDelivery(ctx context.Context, id string, fence uint64, code string, next time.Time, terminal bool) error {
	tag, err := repository.pool.Exec(ctx, deliveryRetrySQL, id, fence, code, next, terminal)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("retry interaction delivery")
	}
	return nil
}

func (repository *Repository) GetDelivery(ctx context.Context, id string) (entity.Delivery, error) {
	return repository.getDelivery(ctx, repository.pool, id)
}

func (repository *Repository) GetDeliveryByProviderPost(ctx context.Context, postID string) (entity.Delivery, error) {
	var id string
	if err := repository.pool.QueryRow(ctx, deliveryGetByPostSQL, postID).Scan(&id); err != nil {
		return entity.Delivery{}, err
	}
	return repository.GetDelivery(ctx, id)
}

func (repository *Repository) ListPendingReactionPosts(ctx context.Context, limit int) (map[string]string, error) {
	if limit < 1 || limit > 1024 {
		return nil, errors.New("reaction catch-up limit is invalid")
	}
	rows, err := repository.pool.Query(ctx, gateDeliveryReactionPostsSQL, limit+1)
	if err != nil {
		return nil, errors.New("list pending reaction posts")
	}
	defer rows.Close()
	result := make(map[string]string, limit)
	for rows.Next() {
		var postID, channelID string
		if rows.Scan(&postID, &channelID) != nil {
			return nil, errors.New("scan pending reaction post")
		}
		if len(result) == limit {
			return nil, errors.New("pending reaction post limit exceeded")
		}
		result[postID] = channelID
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("read pending reaction posts")
	}
	return result, nil
}

func (repository *Repository) MarkOwnerGateDecided(ctx context.Context, deliveryID string) error {
	tag, err := repository.pool.Exec(ctx, gateDeliveryDecidedSQL, deliveryID)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("mark owner gate delivery decided")
	}
	return nil
}

func (repository *Repository) ClaimOwnerGateRequest(ctx context.Context) (string, bool, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return "", false, errors.New("begin owner gate claim transaction")
	}
	defer tx.Rollback(ctx)
	var key string
	err = tx.QueryRow(ctx, gateClaimPendingSQL).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		var active bool
		if checkErr := tx.QueryRow(ctx, gateDeliveryActiveSQL).Scan(&active); checkErr != nil {
			return "", false, errors.New("check active owner gate delivery")
		}
		if active {
			return "", false, nil
		}
		key = uuid.NewString()
		if _, err = tx.Exec(ctx, gateClaimInsertSQL, key); err != nil {
			return "", false, errors.New("insert owner gate claim request")
		}
	} else if err != nil {
		return "", false, errors.New("read owner gate claim request")
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, errors.New("commit owner gate claim request")
	}
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
	tag, err := tx.Exec(ctx, gateClaimBindSQL, key, delivery.OwnerGate.GateID, delivery.ID)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("bind owner gate claim request")
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("commit owner gate binding transaction")
	}
	return nil
}

func (repository *Repository) CompleteOwnerGateClaim(ctx context.Context, key string) error {
	tag, err := repository.pool.Exec(ctx, gateClaimCompleteSQL, key)
	if err != nil || tag.RowsAffected() != 1 {
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
	)
	if err != nil {
		return false, errors.New("insert interaction delivery")
	}
	return tag.RowsAffected() == 1, nil
}

func (repository *Repository) getDelivery(ctx context.Context, source queryer, id string) (entity.Delivery, error) {
	var delivery entity.Delivery
	var kind, state string
	var sessionID, turnID, gateID, processID, recipient sql.NullString
	var leaseExpires, claimExpires, recordedAt sql.NullTime
	var payload, attachments, claimCipher []byte
	var gatePayloadDigest string
	var gateVersion, processVersion, claimFence uint64
	err := source.QueryRow(ctx, deliveryGetSQL, id).Scan(
		&delivery.ID, &kind, &state, &delivery.OrganizationID, &delivery.ProjectID,
		&sessionID, &turnID, &delivery.Attempt, &delivery.ImmutableInputSHA256,
		&delivery.TeamID, &delivery.ChannelID, &delivery.RootPostID, &delivery.BotStableKey,
		&delivery.Locale, &payload, &delivery.PayloadSHA256, &attachments,
		&delivery.ProviderPostID, &delivery.ProviderReceiptSHA256, &delivery.Attempts,
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

func scanInbound(row rowScanner) (entity.InboundEvent, sql.NullTime, error) {
	var result entity.InboundEvent
	var kind, state string
	var payload, attachments []byte
	var sessionID, artifactID sql.NullString
	var processingExpires sql.NullTime
	var attempts uint32
	var nextAttemptAt, createdAt, updatedAt time.Time
	var id, providerEventID, payloadDigest, organizationID, projectID string
	var revision uint64
	err := row.Scan(
		&id, &providerEventID, &kind, &revision, &payload,
		&payloadDigest, &state, &organizationID, &projectID,
		&sessionID, &artifactID, &attachments, &attempts, &nextAttemptAt,
		&createdAt, &updatedAt, &processingExpires,
	)
	if err != nil {
		return entity.InboundEvent{}, sql.NullTime{}, err
	}
	if json.Unmarshal(payload, &result) != nil || json.Unmarshal(attachments, &result.AttachmentArtifacts) != nil {
		return entity.InboundEvent{}, sql.NullTime{}, errors.New("decode inbound event")
	}
	if result.ID != id || result.ProviderEventID != providerEventID || string(result.Kind) != kind ||
		result.Revision != revision || result.DigestSHA256 != payloadDigest ||
		result.OrganizationID != organizationID || result.ProjectID != projectID {
		return entity.InboundEvent{}, sql.NullTime{}, errors.New("inbound event projection mismatch")
	}
	result.Kind, result.State = enum.InboundKind(kind), enum.InboundState(state)
	result.SessionID, result.PromptArtifactID = sessionID.String, artifactID.String
	result.Attempts, result.NextAttemptAt = attempts, nextAttemptAt
	result.CreatedAt, result.UpdatedAt = createdAt, updatedAt
	return result, processingExpires, nil
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

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func interval(value time.Duration) string { return value.String() }
