package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/codex-k8s/matter-codex/libs/go/sessionarchive"
	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
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

func (repo *Repository) ObserveRuntimeSecretBinding(ctx context.Context, input adminrepo.ObserveRuntimeSecretBindingInput) (entity.RuntimeSecretBindingRevision, error) {
	var item entity.RuntimeSecretBindingRevision
	err := repo.db.QueryRow(ctx, query("runtime_secret_bindings__observe.sql"),
		strings.TrimSpace(input.BindingKey), strings.TrimSpace(input.SecretName),
		strings.TrimSpace(input.SecretKey), strings.TrimSpace(input.IntegritySHA256),
	).Scan(&item.BindingKey, &item.SecretName, &item.SecretKey, &item.IntegritySHA256, &item.Revision, &item.UpdatedAt)
	if err != nil {
		return entity.RuntimeSecretBindingRevision{}, fmt.Errorf("observe runtime secret binding revision: %w", err)
	}
	return item, nil
}

func (repo *Repository) SetAgentSessionDesiredRuntimeRevision(ctx context.Context, input adminrepo.SetAgentSessionDesiredRuntimeRevisionInput) (entity.AgentSessionRuntimeRevisionState, error) {
	item, err := repo.scanAgentSessionRuntimeRevisionState(ctx, query("agent_sessions__set_desired_revision.sql"), strings.TrimSpace(input.SessionKey), input.RuntimeRevisionID)
	if errors.Is(err, adminrepo.ErrNotFound) {
		return entity.AgentSessionRuntimeRevisionState{}, adminrepo.ErrRuntimeReconciliationConflict
	}
	return item, err
}

func (repo *Repository) AcquireAgentSessionRuntimeLease(ctx context.Context, input adminrepo.AcquireAgentSessionRuntimeLeaseInput) (entity.AgentSessionRuntimeRevisionState, error) {
	if err := validateRuntimeLeaseInput(input); err != nil {
		return entity.AgentSessionRuntimeRevisionState{}, err
	}
	leaseSeconds := input.LeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = 120
	}
	item, err := repo.scanAgentSessionRuntimeRevisionState(ctx, query("agent_sessions__acquire_runtime_lease.sql"),
		strings.TrimSpace(input.SessionKey), input.DesiredRuntimeRevisionID,
		input.ExpectedAppliedRuntimeRevisionID, strings.TrimSpace(input.ExpectedPodUID),
		strings.TrimSpace(input.LeaseToken), leaseSeconds,
	)
	if errors.Is(err, adminrepo.ErrNotFound) {
		return entity.AgentSessionRuntimeRevisionState{}, adminrepo.ErrRuntimeReconciliationConflict
	}
	return item, err
}

func (repo *Repository) RefreshAgentSessionRuntimeLease(ctx context.Context, input adminrepo.AcquireAgentSessionRuntimeLeaseInput) (entity.AgentSessionRuntimeRevisionState, error) {
	if err := validateRuntimeLeaseInput(input); err != nil {
		return entity.AgentSessionRuntimeRevisionState{}, err
	}
	leaseSeconds := input.LeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = 120
	}
	item, err := repo.scanAgentSessionRuntimeRevisionState(ctx, query("agent_sessions__refresh_runtime_lease.sql"),
		strings.TrimSpace(input.SessionKey), input.DesiredRuntimeRevisionID,
		input.ExpectedAppliedRuntimeRevisionID, strings.TrimSpace(input.ExpectedPodUID),
		strings.TrimSpace(input.LeaseToken), leaseSeconds,
	)
	if errors.Is(err, adminrepo.ErrNotFound) {
		return entity.AgentSessionRuntimeRevisionState{}, adminrepo.ErrRuntimeReconciliationConflict
	}
	return item, err
}

func (repo *Repository) MarkAgentSessionRuntimeApplied(ctx context.Context, input adminrepo.MarkAgentSessionRuntimeAppliedInput) (entity.AgentSessionRuntimeRevisionState, error) {
	if strings.TrimSpace(input.SessionKey) == "" || input.RuntimeRevisionID < 0 || input.ExpectedAppliedRuntimeRevisionID < 0 || strings.TrimSpace(input.AppliedPodUID) == "" || strings.TrimSpace(input.LeaseToken) == "" {
		return entity.AgentSessionRuntimeRevisionState{}, fmt.Errorf("mark applied runtime revision requires exact session, Pod UID, revision, and lease")
	}
	if input.ExpectedAppliedRuntimeRevisionID > 0 && strings.TrimSpace(input.ExpectedPodUID) == "" {
		return entity.AgentSessionRuntimeRevisionState{}, fmt.Errorf("mark applied runtime revision requires expected Pod UID")
	}
	item, err := repo.scanAgentSessionRuntimeRevisionState(ctx, query("agent_sessions__mark_applied_revision.sql"),
		strings.TrimSpace(input.SessionKey), input.RuntimeRevisionID, input.ExpectedAppliedRuntimeRevisionID,
		strings.TrimSpace(input.ExpectedPodUID), strings.TrimSpace(input.AppliedPodUID), strings.TrimSpace(input.LeaseToken),
	)
	if errors.Is(err, adminrepo.ErrNotFound) {
		return entity.AgentSessionRuntimeRevisionState{}, adminrepo.ErrRuntimeReconciliationConflict
	}
	return item, err
}

func (repo *Repository) ReleaseAgentSessionRuntimeLease(ctx context.Context, input adminrepo.ReleaseAgentSessionRuntimeLeaseInput) error {
	if strings.TrimSpace(input.SessionKey) == "" || strings.TrimSpace(input.LeaseToken) == "" {
		return fmt.Errorf("release runtime reconciliation lease requires session and lease token")
	}
	_, err := repo.db.Exec(ctx, query("agent_sessions__release_runtime_lease.sql"), strings.TrimSpace(input.SessionKey), strings.TrimSpace(input.LeaseToken))
	if err != nil {
		return fmt.Errorf("release agent session runtime lease: %w", err)
	}
	return nil
}

func validateRuntimeLeaseInput(input adminrepo.AcquireAgentSessionRuntimeLeaseInput) error {
	if strings.TrimSpace(input.SessionKey) == "" || input.DesiredRuntimeRevisionID < 0 || input.ExpectedAppliedRuntimeRevisionID < 0 || strings.TrimSpace(input.LeaseToken) == "" {
		return fmt.Errorf("runtime reconciliation lease requires exact session, revisions, and lease token")
	}
	if input.ExpectedAppliedRuntimeRevisionID > 0 && strings.TrimSpace(input.ExpectedPodUID) == "" {
		return fmt.Errorf("runtime reconciliation lease requires expected Pod UID")
	}
	return nil
}

func (repo *Repository) scanAgentSessionRuntimeRevisionState(ctx context.Context, statement string, arguments ...any) (entity.AgentSessionRuntimeRevisionState, error) {
	var item entity.AgentSessionRuntimeRevisionState
	err := repo.db.QueryRow(ctx, statement, arguments...).Scan(
		&item.SessionID,
		&item.SessionKey,
		&item.DesiredRuntimeRevisionID,
		&item.AppliedRuntimeRevisionID,
		&item.AppliedPodUID,
		&item.ReconcileLeaseToken,
		&item.ReconcileLeaseExpiresAt,
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
	archiveMetadata, err := validateConfirmedArchive(input.TurnStatus, input.SessionArchiveGzipBase64, input.ArchiveSHA256, input.ArchiveSizeBytes)
	if err != nil {
		return entity.AgentSessionCompletion{}, err
	}
	input.ArchiveSHA256 = archiveMetadata.SHA256
	input.ArchiveSizeBytes = archiveMetadata.SizeBytes
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return entity.AgentSessionCompletion{}, fmt.Errorf("begin atomic agent session completion: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var turnSessionID, turnRuntimeRevisionID int64
	var currentTurnStatus, turnRunID, currentFinalMessage, currentErrorMessage, currentArtifacts, completionPodUID string
	if err := tx.QueryRow(ctx, query("agent_session_turns__lock_completion.sql"), input.TurnID).Scan(
		&turnSessionID, &currentTurnStatus, &turnRunID, &turnRuntimeRevisionID,
		&currentFinalMessage, &currentErrorMessage, &currentArtifacts, &completionPodUID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.AgentSessionCompletion{}, adminrepo.ErrNotFound
		}
		return entity.AgentSessionCompletion{}, fmt.Errorf("lock agent session turn completion: %w", err)
	}
	var sessionID, currentArchiveVersion, activeTurnID, appliedRuntimeRevisionID int64
	var legacyCodexSessionID, legacyPayload, activeRunID, appliedPodUID string
	if err := tx.QueryRow(ctx, query("agent_sessions__lock_completion.sql"), strings.TrimSpace(input.SessionKey)).Scan(
		&sessionID,
		&currentArchiveVersion,
		&legacyCodexSessionID,
		&legacyPayload,
		&activeTurnID,
		&activeRunID,
		&appliedRuntimeRevisionID,
		&appliedPodUID,
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
		if strings.TrimSpace(input.RunID) != turnRunID || strings.TrimSpace(input.TurnStatus) != currentTurnStatus ||
			input.FinalMessage != currentFinalMessage || input.ErrorMessage != currentErrorMessage || input.Artifacts != currentArtifacts ||
			input.RuntimeRevisionID != turnRuntimeRevisionID || strings.TrimSpace(input.PodUID) != completionPodUID {
			return entity.AgentSessionCompletion{}, fmt.Errorf("terminal completion replay does not match persisted turn")
		}
		result, err := txRepo.readExistingAgentSessionCompletion(ctx, sessionID, input.TurnID)
		if err != nil {
			return entity.AgentSessionCompletion{}, err
		}
		if !exactArchiveReplay(result.Archive, input) {
			return entity.AgentSessionCompletion{}, fmt.Errorf("terminal completion replay does not match persisted archive")
		}
		result.AlreadyCompleted = true
		if err := tx.Commit(ctx); err != nil {
			return entity.AgentSessionCompletion{}, fmt.Errorf("commit idempotent agent session completion: %w", err)
		}
		return result, nil
	}
	if currentTurnStatus != "running" || strings.TrimSpace(input.RunID) == "" || strings.TrimSpace(input.RunID) != turnRunID ||
		activeTurnID != input.TurnID || strings.TrimSpace(activeRunID) != turnRunID {
		return entity.AgentSessionCompletion{}, fmt.Errorf("completion does not match the active running turn")
	}
	legacyRuntime := turnRuntimeRevisionID == 0 && appliedRuntimeRevisionID == 0 && input.RuntimeRevisionID == 0
	if !legacyRuntime {
		if turnRuntimeRevisionID <= 0 || input.RuntimeRevisionID != turnRuntimeRevisionID || appliedRuntimeRevisionID != turnRuntimeRevisionID ||
			strings.TrimSpace(input.PodUID) == "" || strings.TrimSpace(input.PodUID) != strings.TrimSpace(appliedPodUID) {
			return entity.AgentSessionCompletion{}, fmt.Errorf("completion runtime revision or pod identity fence does not match")
		}
	} else if strings.TrimSpace(input.PodUID) != "" && strings.TrimSpace(input.PodUID) != strings.TrimSpace(appliedPodUID) {
		return entity.AgentSessionCompletion{}, fmt.Errorf("legacy completion pod identity fence does not match")
	}

	var archive entity.AgentSessionArchive
	if strings.TrimSpace(input.SessionArchiveGzipBase64) != "" {
		if currentArchiveVersion == 0 && strings.TrimSpace(legacyPayload) != "" {
			legacySHA, legacySize, err := confirmedArchiveMetadata(legacyPayload)
			if err != nil {
				return entity.AgentSessionCompletion{}, fmt.Errorf("validate previous agent session archive: %w", err)
			}
			if _, err := scanAgentSessionArchive(tx.QueryRow(ctx, query("agent_session_archives__insert.sql"),
				sessionID, nil, int64(1), legacyCodexSessionID, legacyPayload, legacySHA, legacySize,
			)); err != nil {
				return entity.AgentSessionCompletion{}, fmt.Errorf("preserve previous agent session archive: %w", err)
			}
			currentArchiveVersion = 1
		}
		archive, err = scanAgentSessionArchive(tx.QueryRow(ctx, query("agent_session_archives__insert.sql"),
			sessionID,
			input.TurnID,
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
		SessionID: sessionID, TurnID: input.TurnID, RunID: turnRunID,
		ExpectedStatus: currentTurnStatus, Status: input.TurnStatus,
		FinalMessage: input.FinalMessage, ErrorMessage: input.ErrorMessage, Artifacts: input.Artifacts,
		CompletionPodUID: strings.TrimSpace(input.PodUID),
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
	archive, err := scanAgentSessionArchive(repo.db.QueryRow(ctx, query("agent_session_archives__by_turn.sql"), sessionID, turnID))
	if errors.Is(err, pgx.ErrNoRows) {
		archive = entity.AgentSessionArchive{}
		err = nil
	}
	if errors.Is(err, adminrepo.ErrNotFound) {
		archive = entity.AgentSessionArchive{}
	} else if err != nil {
		return entity.AgentSessionCompletion{}, err
	}
	return entity.AgentSessionCompletion{Turn: turn, Session: session, Archive: archive}, nil
}

func exactArchiveReplay(archive entity.AgentSessionArchive, input adminrepo.CompleteAgentSessionTurnWithArchiveInput) bool {
	payload := strings.TrimSpace(input.SessionArchiveGzipBase64)
	if archive.ID == 0 {
		return payload == "" && strings.TrimSpace(input.ArchiveSHA256) == "" && input.ArchiveSizeBytes == 0
	}
	return payload == archive.PayloadGzipBase64 &&
		strings.TrimSpace(input.CodexSessionID) == archive.CodexSessionID &&
		strings.TrimSpace(input.ArchiveSHA256) == archive.SHA256 &&
		input.ArchiveSizeBytes == archive.SizeBytes
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
		&item.TurnID,
		&item.Version,
		&item.CodexSessionID,
		&item.PayloadGzipBase64,
		&item.SHA256,
		&item.SizeBytes,
		&item.CreatedAt,
	)
	return item, err
}

func validateConfirmedArchive(turnStatus string, payload string, expectedSHA256 string, expectedSize int64) (sessionarchive.Metadata, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		if strings.TrimSpace(expectedSHA256) != "" || expectedSize != 0 {
			return sessionarchive.Metadata{}, fmt.Errorf("empty archive has unexpected metadata")
		}
		if strings.TrimSpace(turnStatus) == "succeeded" {
			return sessionarchive.Metadata{}, fmt.Errorf("successful completion requires a non-empty archive")
		}
		return sessionarchive.Metadata{}, nil
	}
	expectedSHA256 = strings.TrimSpace(expectedSHA256)
	if (expectedSHA256 == "") != (expectedSize == 0) || expectedSize < 0 {
		return sessionarchive.Metadata{}, fmt.Errorf("archive metadata is only partially specified")
	}
	metadata, err := sessionarchive.ValidateEncoded(payload, expectedSHA256, expectedSize)
	if err != nil {
		return sessionarchive.Metadata{}, err
	}
	return metadata, nil
}

func confirmedArchiveMetadata(payload string) (string, int64, error) {
	metadata, err := sessionarchive.ValidateEncoded(payload, "", 0)
	return metadata.SHA256, metadata.SizeBytes, err
}

func terminalAgentSessionTurnStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "succeeded", "failed", "canceled", "blocked":
		return true
	default:
		return false
	}
}
