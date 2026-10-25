package platform

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

// queryPromptContinuationPinTurn embeds first-write binding of context and dependency digests to
// exact run turns only while both stored pins are empty. Caller authorization and digest
// construction remain separate.
// queryPromptContinuationPinTurn встраивает первичную привязку контекстного и зависимостного
// дайджестов к turns точного запуска, только пока оба сохранённых значения пусты. Авторизация и
// вычисление дайджестов выполняются отдельно.
//
//go:embed sql/prompt_continuation_pin_turn.sql
var queryPromptContinuationPinTurn string

// queryPromptContinuationClaimPin embeds the node turn dependency pin, task, attachment/session
// refs and currently active source actor identity. A missing/inactive actor is represented by
// empty fields and must not become claim authority.
// queryPromptContinuationClaimPin встраивает зависимостную привязку turn узла, задачу, ссылки
// вложений и сессии и текущую активную идентичность исходного субъекта. Отсутствующий или
// неактивный субъект представлен пустыми полями и не становится источником прав захвата.
//
//go:embed sql/prompt_continuation_claim_pin.sql
var queryPromptContinuationClaimPin string

// checkClaimContinuationPinTx permits missing rows or empty expected dependency pins; otherwise it
// requires the database-resolved active actor and recomputes continuation preview before comparing
// DependencyDigest. Mismatch is VersionMismatch, lookup failures Unavailable; it changes only the
// local scope copy and performs no claim or commit.
// checkClaimContinuationPinTx допускает отсутствие строки либо пустой ожидаемый зависимостный pin;
// иначе требует активного субъекта из БД и заново строит continuation preview перед сравнением
// DependencyDigest. Несовпадение даёт VersionMismatch, сбой чтения — Unavailable; меняет только
// локальную копию scope, не захватывает работу и не выполняет Commit.
func (repository *Repository) checkClaimContinuationPinTx(ctx context.Context, tx pgx.Tx, current scope, nodeRef string) error {
	var expected, task, attachmentRef, sessionRef string
	err := tx.QueryRow(ctx, queryPromptContinuationClaimPin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "node_ref": nodeRef}).Scan(
		&expected, &task, &attachmentRef, &sessionRef, &current.actorID, &current.actorRef, &current.actorName, &current.organizationRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if expected == "" {
		return nil
	}
	if current.actorID == "" {
		return errs.ErrForbidden
	}
	snapshot, err := repository.promptContinuationPreviewTx(ctx, tx, current, sessionRef, query.PromptPreviewContext{Task: task, AttachmentSetRef: attachmentRef})
	if err != nil {
		return err
	}
	if snapshot.ContextPin.DependencyDigest != expected {
		return errs.ErrVersionMismatch
	}
	return nil
}

// checkContinuationPreviewPinTx returns an empty pin when the client supplied no expected digest.
// Otherwise it requires canonical lowercase 32-byte hex and compares the full freshly computed
// ContextPin.Digest, returning VersionMismatch on drift; it does not create runtime authority.
// checkContinuationPreviewPinTx возвращает пустой pin, если клиент не передал ожидаемый дайджест.
// Иначе требует канонический строчный hex из 32 байт и сравнивает полный заново вычисленный
// ContextPin.Digest, возвращая VersionMismatch при изменении; runtime-полномочия не создаёт.
func (repository *Repository) checkContinuationPreviewPinTx(ctx context.Context, tx pgx.Tx, current scope, payload command.SessionTurnInput) (entity.PromptContextPin, error) {
	if payload.ExpectedPromptContextDigest == "" {
		return entity.PromptContextPin{}, nil
	}
	decoded, err := hex.DecodeString(payload.ExpectedPromptContextDigest)
	if err != nil || len(decoded) != 32 || strings.ToLower(payload.ExpectedPromptContextDigest) != payload.ExpectedPromptContextDigest {
		return entity.PromptContextPin{}, errs.ErrInvalid
	}
	snapshot, err := repository.promptContinuationPreviewTx(ctx, tx, current, payload.SessionRef, query.PromptPreviewContext{Task: payload.Task, AttachmentSetRef: payload.AttachmentSetRef})
	if err != nil {
		return entity.PromptContextPin{}, err
	}
	if snapshot.ContextPin.Digest != payload.ExpectedPromptContextDigest {
		return entity.PromptContextPin{}, errs.ErrVersionMismatch
	}
	return snapshot.ContextPin, nil
}
