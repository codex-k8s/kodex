package admin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

const (
	maxConfirmedArchiveEncodedBytes = 64 << 20
	maxConfirmedArchiveDecodedBytes = 48 << 20
)

func (repo *Repository) EnsureRuntimeRevision(ctx context.Context, input adminrepo.EnsureRuntimeRevisionInput) (entity.RuntimeRevision, error) {
	item, err := scanRuntimeRevision(repo.db.QueryRow(ctx, query("runtime_revisions__ensure.sql"),
		strings.TrimSpace(input.Digest),
		input.Manifest,
		strings.TrimSpace(input.AccountAlias),
		strings.TrimSpace(input.AuthorizationRevision),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		existing, readErr := scanRuntimeRevision(repo.db.QueryRow(ctx, query("runtime_revisions__get_by_digest.sql"), strings.TrimSpace(input.Digest)))
		if readErr == nil && runtimeRevisionMatches(existing, input) {
			return existing, nil
		}
		if readErr != nil && !errors.Is(readErr, pgx.ErrNoRows) {
			return entity.RuntimeRevision{}, fmt.Errorf("read runtime revision after concurrent ensure: %w", readErr)
		}
		return entity.RuntimeRevision{}, adminrepo.ErrRuntimeRevisionConflict
	}
	if err != nil {
		return entity.RuntimeRevision{}, fmt.Errorf("ensure runtime revision: %w", err)
	}
	return item, nil
}

func runtimeRevisionMatches(existing entity.RuntimeRevision, input adminrepo.EnsureRuntimeRevisionInput) bool {
	var existingManifest, expectedManifest any
	if json.Unmarshal([]byte(existing.Manifest), &existingManifest) != nil || json.Unmarshal([]byte(input.Manifest), &expectedManifest) != nil {
		return false
	}
	existingCanonical, existingErr := json.Marshal(existingManifest)
	expectedCanonical, expectedErr := json.Marshal(expectedManifest)
	return existingErr == nil && expectedErr == nil && string(existingCanonical) == string(expectedCanonical) &&
		existing.AccountAlias == strings.TrimSpace(input.AccountAlias) &&
		existing.AuthorizationRevision == strings.TrimSpace(input.AuthorizationRevision)
}

func (repo *Repository) GetRuntimeRevision(ctx context.Context, id int64) (entity.RuntimeRevision, error) {
	item, err := scanRuntimeRevision(repo.db.QueryRow(ctx, query("runtime_revisions__get.sql"), id))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeRevision{}, adminrepo.ErrNotFound
	}
	if err != nil {
		return entity.RuntimeRevision{}, fmt.Errorf("get runtime revision: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetAgentSessionRuntimeRevisionState(ctx context.Context, sessionKey string) (entity.AgentSessionRuntimeRevisionState, error) {
	return repo.scanAgentSessionRuntimeRevisionState(ctx, query("agent_sessions__runtime_revision_state.sql"), strings.TrimSpace(sessionKey))
}

func (repo *Repository) SetAgentSessionDesiredRuntimeRevision(ctx context.Context, input adminrepo.SetAgentSessionDesiredRuntimeRevisionInput) (entity.AgentSessionRuntimeRevisionState, error) {
	return repo.scanAgentSessionRuntimeRevisionState(ctx, query("agent_sessions__set_desired_revision.sql"), strings.TrimSpace(input.SessionKey), input.RuntimeRevisionID)
}

func (repo *Repository) MarkAgentSessionRuntimeApplied(ctx context.Context, input adminrepo.MarkAgentSessionRuntimeAppliedInput) (entity.AgentSessionRuntimeRevisionState, error) {
	return repo.scanAgentSessionRuntimeRevisionState(ctx, query("agent_sessions__mark_applied_revision.sql"), strings.TrimSpace(input.SessionKey), input.RuntimeRevisionID)
}

func (repo *Repository) scanAgentSessionRuntimeRevisionState(ctx context.Context, statement string, arguments ...any) (entity.AgentSessionRuntimeRevisionState, error) {
	var item entity.AgentSessionRuntimeRevisionState
	err := repo.db.QueryRow(ctx, statement, arguments...).Scan(
		&item.SessionID,
		&item.SessionKey,
		&item.DesiredRuntimeRevisionID,
		&item.AppliedRuntimeRevisionID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentSessionRuntimeRevisionState{}, adminrepo.ErrNotFound
	}
	if err != nil {
		return entity.AgentSessionRuntimeRevisionState{}, fmt.Errorf("scan agent session runtime revision state: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetNextQueuedAgentSessionRuntimeRevision(ctx context.Context, sessionID int64) (entity.RuntimeRevision, error) {
	item, err := scanRuntimeRevision(repo.db.QueryRow(ctx, query("agent_session_turns__next_queued_revision.sql"), sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeRevision{}, adminrepo.ErrNotFound
	}
	if err != nil {
		return entity.RuntimeRevision{}, fmt.Errorf("get next queued agent session runtime revision: %w", err)
	}
	return item, nil
}

func (repo *Repository) GetLatestAgentSessionArchive(ctx context.Context, sessionID int64) (entity.AgentSessionArchive, error) {
	item, err := scanAgentSessionArchive(repo.db.QueryRow(ctx, query("agent_session_archives__latest.sql"), sessionID))
	if err == nil {
		return item, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentSessionArchive{}, fmt.Errorf("get latest agent session archive: %w", err)
	}
	var codexSessionID, payload string
	var createdAt entity.AgentSessionArchive
	if err := repo.db.QueryRow(ctx, query("agent_sessions__legacy_archive.sql"), sessionID).Scan(
		&codexSessionID,
		&payload,
		&createdAt.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentSessionArchive{}, adminrepo.ErrNotFound
		}
		return entity.AgentSessionArchive{}, fmt.Errorf("get legacy agent session archive: %w", err)
	}
	sha, size, err := confirmedArchiveMetadata(payload)
	if err != nil {
		return entity.AgentSessionArchive{}, fmt.Errorf("validate legacy agent session archive: %w", err)
	}
	createdAt.SessionID = sessionID
	createdAt.CodexSessionID = codexSessionID
	createdAt.PayloadGzipBase64 = payload
	createdAt.SHA256 = sha
	createdAt.SizeBytes = size
	return createdAt, nil
}

func (repo *Repository) CompleteAgentSessionTurnWithArchive(ctx context.Context, input adminrepo.CompleteAgentSessionTurnWithArchiveInput) (entity.AgentSessionCompletion, error) {
	if err := validateConfirmedArchive(input.SessionArchiveGzipBase64, input.ArchiveSHA256, input.ArchiveSizeBytes); err != nil {
		return entity.AgentSessionCompletion{}, err
	}
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.AgentSessionCompletion{}, fmt.Errorf("begin atomic agent session completion: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var turnSessionID int64
	var currentTurnStatus string
	if err := tx.QueryRow(ctx, query("agent_session_turns__lock_completion.sql"), input.TurnID).Scan(&turnSessionID, &currentTurnStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentSessionCompletion{}, adminrepo.ErrNotFound
		}
		return entity.AgentSessionCompletion{}, fmt.Errorf("lock agent session turn completion: %w", err)
	}
	var sessionID, currentArchiveVersion int64
	var legacyCodexSessionID, legacyPayload string
	if err := tx.QueryRow(ctx, query("agent_sessions__lock_completion.sql"), strings.TrimSpace(input.SessionKey)).Scan(
		&sessionID,
		&currentArchiveVersion,
		&legacyCodexSessionID,
		&legacyPayload,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentSessionCompletion{}, adminrepo.ErrNotFound
		}
		return entity.AgentSessionCompletion{}, fmt.Errorf("lock agent session completion: %w", err)
	}
	if turnSessionID != sessionID {
		return entity.AgentSessionCompletion{}, fmt.Errorf("turn does not belong to session")
	}
	txRepo := newTransactionalRepository(tx)
	if terminalAgentSessionTurnStatus(currentTurnStatus) {
		result, err := txRepo.readExistingAgentSessionCompletion(ctx, sessionID, input.TurnID)
		if err != nil {
			return entity.AgentSessionCompletion{}, err
		}
		result.AlreadyCompleted = true
		if err := tx.Commit(ctx); err != nil {
			return entity.AgentSessionCompletion{}, fmt.Errorf("commit idempotent agent session completion: %w", err)
		}
		return result, nil
	}

	var archive entity.AgentSessionArchive
	if strings.TrimSpace(input.SessionArchiveGzipBase64) != "" {
		if currentArchiveVersion == 0 && strings.TrimSpace(legacyPayload) != "" {
			legacySHA, legacySize, err := confirmedArchiveMetadata(legacyPayload)
			if err != nil {
				return entity.AgentSessionCompletion{}, fmt.Errorf("validate previous agent session archive: %w", err)
			}
			if _, err := scanAgentSessionArchive(tx.QueryRow(ctx, query("agent_session_archives__insert.sql"),
				sessionID, int64(1), legacyCodexSessionID, legacyPayload, legacySHA, legacySize,
			)); err != nil {
				return entity.AgentSessionCompletion{}, fmt.Errorf("preserve previous agent session archive: %w", err)
			}
			currentArchiveVersion = 1
		}
		archive, err = scanAgentSessionArchive(tx.QueryRow(ctx, query("agent_session_archives__insert.sql"),
			sessionID,
			currentArchiveVersion+1,
			strings.TrimSpace(input.CodexSessionID),
			input.SessionArchiveGzipBase64,
			strings.TrimSpace(input.ArchiveSHA256),
			input.ArchiveSizeBytes,
		))
		if err != nil {
			return entity.AgentSessionCompletion{}, fmt.Errorf("insert confirmed agent session archive: %w", err)
		}
	}

	turn, err := txRepo.CompleteAgentSessionTurn(ctx, adminrepo.CompleteAgentSessionTurnInput{
		TurnID: input.TurnID, Status: input.TurnStatus,
		FinalMessage: input.FinalMessage, ErrorMessage: input.ErrorMessage, Artifacts: input.Artifacts,
	})
	if err != nil {
		return entity.AgentSessionCompletion{}, err
	}
	command, err := tx.Exec(ctx, query("agent_sessions__complete_archive.sql"),
		strings.TrimSpace(input.SessionKey),
		strings.TrimSpace(input.CodexSessionID),
		input.SessionArchiveGzipBase64,
		archive.Version,
		archive.SHA256,
		archive.SizeBytes,
		input.SessionStatus,
		input.ExtendTTLSeconds,
	)
	if err != nil {
		return entity.AgentSessionCompletion{}, fmt.Errorf("update atomic agent session completion: %w", err)
	}
	if command.RowsAffected() != 1 {
		return entity.AgentSessionCompletion{}, adminrepo.ErrNotFound
	}
	session, err := txRepo.GetAgentSessionByID(ctx, sessionID)
	if err != nil {
		return entity.AgentSessionCompletion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.AgentSessionCompletion{}, fmt.Errorf("commit atomic agent session completion: %w", err)
	}
	return entity.AgentSessionCompletion{Turn: turn, Session: session, Archive: archive}, nil
}

func (repo *Repository) readExistingAgentSessionCompletion(ctx context.Context, sessionID int64, turnID int64) (entity.AgentSessionCompletion, error) {
	turn, err := repo.GetAgentSessionTurn(ctx, turnID)
	if err != nil {
		return entity.AgentSessionCompletion{}, err
	}
	session, err := repo.GetAgentSessionByID(ctx, sessionID)
	if err != nil {
		return entity.AgentSessionCompletion{}, err
	}
	archive, err := repo.GetLatestAgentSessionArchive(ctx, sessionID)
	if errors.Is(err, adminrepo.ErrNotFound) {
		archive = entity.AgentSessionArchive{}
	} else if err != nil {
		return entity.AgentSessionCompletion{}, err
	}
	return entity.AgentSessionCompletion{Turn: turn, Session: session, Archive: archive}, nil
}

func scanRuntimeRevision(row pgx.Row) (entity.RuntimeRevision, error) {
	var item entity.RuntimeRevision
	err := row.Scan(&item.ID, &item.Digest, &item.Manifest, &item.AccountAlias, &item.AuthorizationRevision, &item.CreatedAt)
	return item, err
}

func scanAgentSessionArchive(row pgx.Row) (entity.AgentSessionArchive, error) {
	var item entity.AgentSessionArchive
	err := row.Scan(
		&item.ID,
		&item.SessionID,
		&item.Version,
		&item.CodexSessionID,
		&item.PayloadGzipBase64,
		&item.SHA256,
		&item.SizeBytes,
		&item.CreatedAt,
	)
	return item, err
}

func validateConfirmedArchive(payload string, expectedSHA256 string, expectedSize int64) error {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		if strings.TrimSpace(expectedSHA256) != "" || expectedSize != 0 {
			return fmt.Errorf("empty archive has unexpected metadata")
		}
		return nil
	}
	actualSHA256, actualSize, err := confirmedArchiveMetadata(payload)
	if err != nil {
		return err
	}
	if actualSHA256 != strings.TrimSpace(expectedSHA256) || actualSize != expectedSize {
		return fmt.Errorf("archive metadata mismatch")
	}
	return nil
}

func confirmedArchiveMetadata(payload string) (string, int64, error) {
	payload = strings.TrimSpace(payload)
	if len(payload) == 0 || len(payload) > maxConfirmedArchiveEncodedBytes {
		return "", 0, fmt.Errorf("archive encoded size is outside the bounded contract")
	}
	if base64.StdEncoding.DecodedLen(len(payload)) > maxConfirmedArchiveDecodedBytes {
		return "", 0, fmt.Errorf("archive decoded size exceeds the bounded contract")
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", 0, fmt.Errorf("decode confirmed archive: %w", err)
	}
	if len(decoded) > maxConfirmedArchiveDecodedBytes {
		return "", 0, fmt.Errorf("archive decoded size exceeds the bounded contract")
	}
	digest := sha256.Sum256(decoded)
	return hex.EncodeToString(digest[:]), int64(len(decoded)), nil
}

func terminalAgentSessionTurnStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "succeeded", "failed", "canceled", "blocked":
		return true
	default:
		return false
	}
}
