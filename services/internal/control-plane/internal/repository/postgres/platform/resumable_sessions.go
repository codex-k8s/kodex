package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/resumable_sessions__candidates.sql
var queryResumableSessionCandidates string

//go:embed sql/resumable_sessions__get.sql
var queryResumableSessionGet string

func (repository *Repository) applyContinuationEventAction(ctx context.Context, runner queryRunner, current scope, event *entity.RunEvent) error {
	if event == nil || event.Delta.Run == nil {
		return nil
	}
	run := entity.Run{Ref: event.Delta.Run.Ref, State: event.Delta.Run.State, NextActions: event.Delta.Run.NextActions}
	if err := repository.applyContinuationAction(ctx, runner, current, &run); err != nil {
		return err
	}
	event.Delta.Run.NextActions = run.NextActions
	return nil
}

func (repository *Repository) applyContinuationAction(ctx context.Context, runner queryRunner, current scope, run *entity.Run) error {
	if run == nil {
		return nil
	}
	run.NextActions = slices.DeleteFunc(run.NextActions, func(action string) bool { return action == "ADD_TURN" })
	if run.State != "SUCCEEDED" {
		return nil
	}
	tx, ok := runner.(pgx.Tx)
	if !ok {
		return errs.ErrUnavailable
	}
	var item resumableSessionCandidate
	err := tx.QueryRow(ctx, queryResumableSessionGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"authority_project_id": current.authorityProjectID, "run_ref": run.Ref,
	}).Scan(&item.RunRef, &item.Version, &item.SessionID, &item.SessionRef, &item.ProjectID, &item.ProjectRef, &item.TargetType, &item.TargetRef, &item.AccountRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if err := repository.validateContinuationSnapshot(ctx, tx, current, item); err != nil {
		if continuationIneligible(err) {
			return nil
		}
		return err
	}
	run.NextActions = append(run.NextActions, "ADD_TURN")
	return nil
}

func continuationIneligible(err error) bool {
	return errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrVersionMismatch) || errors.Is(err, errs.ErrInvalid)
}

type resumableSessionCandidate struct {
	RunRef, SessionID, SessionRef, ProjectID, ProjectRef string
	TargetType, TargetRef, AccountRef                    string
	Version                                              int64
}

type resumableSessionCursor struct {
	Scope, Ref, Snapshot string
}

// Total и страница выдаются только после полного прохода одного снимка.
// Ограничение времени закрыто отклоняет запрос без частичного результата.
func (repository *Repository) listResumableSessions(ctx context.Context, current scope, filter query.Filter) ([]entity.Run, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if len(filter.States) != 0 || len([]rune(filter.Query)) > 200 || !utf8.ValidString(filter.Query) || strings.ContainsRune(filter.Query, '\x00') {
		return nil, 0, "", errs.ErrInvalid
	}
	if (filter.TargetRef == "") != (filter.TargetType == "") || filter.TargetType != "" && !slices.Contains([]string{"AGENT", "WORKFLOW"}, filter.TargetType) || len(filter.TargetRef) > 96 || !utf8.ValidString(filter.TargetRef) || strings.ContainsRune(filter.TargetRef, '\x00') {
		return nil, 0, "", errs.ErrInvalid
	}
	wantScope := catalogScope(current, "RESUMABLE_SESSION", filter)
	var cursor resumableSessionCursor
	if filter.Page.Token != "" {
		raw, err := base64.RawURLEncoding.DecodeString(filter.Page.Token)
		if err != nil || len(filter.Page.Token) > 512 || json.Unmarshal(raw, &cursor) != nil || cursor.Scope != wantScope || cursor.Ref == "" || cursor.Snapshot == "" {
			return nil, 0, "", errs.ErrInvalid
		}
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if filter.TargetRef != "" {
		launch := command.Command{Kind: command.LaunchRun, Payload: command.LaunchRunInput{ProjectRef: filter.ProjectRef, Target: entity.RunTarget{Type: filter.TargetType, Ref: filter.TargetRef}}}
		if err := repository.authorizeCommand(ctx, tx, current, launch); err != nil {
			return nil, 0, "", err
		}
	}
	limit := boundedPage(filter.Page)
	selected := make([]resumableSessionCandidate, 0, limit+1)
	fingerprint := sha256.New()
	var total int64
	after := ""
	for {
		rows, err := tx.Query(ctx, queryResumableSessionCandidates, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "actor_id": current.actorID,
			"project_ref": filter.ProjectRef, "authority_project_id": current.authorityProjectID,
			"query": filter.Query, "after_ref": after, "limit": int32(100),
			"target_type": filter.TargetType, "target_ref": filter.TargetRef,
		})
		if err != nil {
			return nil, 0, "", errs.ErrUnavailable
		}
		batch := make([]resumableSessionCandidate, 0, 100)
		for rows.Next() {
			var item resumableSessionCandidate
			if rows.Scan(&item.RunRef, &item.Version, &item.SessionID, &item.SessionRef, &item.ProjectID, &item.ProjectRef, &item.TargetType, &item.TargetRef, &item.AccountRef) != nil {
				rows.Close()
				return nil, 0, "", errs.ErrUnavailable
			}
			batch = append(batch, item)
		}
		rows.Close()
		if rows.Err() != nil {
			return nil, 0, "", errs.ErrUnavailable
		}
		for _, item := range batch {
			after = item.RunRef
			if err := repository.validateContinuationSnapshot(ctx, tx, current, item); err != nil {
				if continuationIneligible(err) {
					continue
				}
				return nil, 0, "", err
			}
			total++
			if total > 1<<53-1 {
				return nil, 0, "", errs.ErrUnavailable
			}
			_, _ = fmt.Fprintf(fingerprint, "%s:%d\n", item.RunRef, item.Version)
			if item.RunRef > cursor.Ref && len(selected) <= int(limit) {
				selected = append(selected, item)
			}
		}
		if len(batch) < 100 {
			break
		}
	}
	snapshot := base64.RawURLEncoding.EncodeToString(fingerprint.Sum(nil))
	if cursor.Snapshot != "" && cursor.Snapshot != snapshot {
		return nil, 0, "", errs.ErrVersionMismatch
	}
	next := ""
	if len(selected) > int(limit) {
		selected = selected[:limit]
		raw, _ := json.Marshal(resumableSessionCursor{Scope: wantScope, Snapshot: snapshot, Ref: selected[len(selected)-1].RunRef})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	items := make([]entity.Run, 0, len(selected))
	for _, candidate := range selected {
		item, err := repository.readRunWithIncidents(ctx, tx, current, candidate.RunRef)
		if err != nil {
			return nil, 0, "", err
		}
		item.NextActions = []string{"OPEN", "ADD_TURN"}
		items = append(items, item)
	}
	if ctx.Err() != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	return items, total, next, nil
}

// Каталог использует те же проверки текущего target, runtime и model catalog,
// что запуск. Он не захватывает command locks в read-only снимке.
func (repository *Repository) validateContinuationSnapshot(ctx context.Context, tx pgx.Tx, current scope, candidate resumableSessionCandidate) error {
	launch := command.Command{Kind: command.LaunchRun, Payload: command.LaunchRunInput{
		ProjectRef: candidate.ProjectRef, Target: entity.RunTarget{Type: candidate.TargetType, Ref: candidate.TargetRef},
	}}
	if err := repository.authorizeCommand(ctx, tx, current, launch); err != nil {
		return err
	}
	var agentRefs []string
	switch candidate.TargetType {
	case "AGENT":
		var name string
		if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectAgentsOrganizationIdProjectIdRef, current.organizationID, candidate.ProjectID, candidate.TargetRef).Scan(&name); err != nil {
			return continuationReadError(err)
		}
		agentRefs = []string{candidate.TargetRef}
	case "WORKFLOW":
		var name, versionID, versionRef, digest, coordinatorRef, coordinatorName string
		var raw []byte
		if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectWorkflowsOrganizationIdProjectIdRef, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "project_id": candidate.ProjectID, "workflow_ref": candidate.TargetRef,
		}).Scan(&name, &versionID, &versionRef, &raw, &digest, &coordinatorRef, &coordinatorName); err != nil {
			return continuationReadError(err)
		}
		var version entity.WorkflowVersion
		if json.Unmarshal(raw, &version) != nil || !validWorkflowVersion(version) || version.CoordinatorAgentRef != coordinatorRef || !validWorkflowRunInput(version.Inputs, nil) {
			return errs.ErrConflict
		}
		agentRefs = append(agentRefs, coordinatorRef)
		for _, step := range version.Steps {
			agentRefs = append(agentRefs, step.AgentRef)
		}
	default:
		return errs.ErrConflict
	}
	var ready bool
	if err := tx.QueryRow(ctx, queryCommandsLaunchrunValidateAgentRuntimeContract, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_id": candidate.ProjectID, "agent_refs": agentRefs,
		"role_runtime_contract_revision": repository.roleImages.RoleRuntimeContractRevision,
		"role_runtime_contract_sha256":   repository.roleImages.RoleRuntimeContractSHA256,
	}).Scan(&ready); err != nil {
		return errs.ErrUnavailable
	}
	if !ready {
		return errs.ErrConflict
	}
	configuration, overlay, err := readRuntimeCatalogConfiguration(ctx, tx, current.organizationID, agentRefs[0], "")
	if err != nil {
		return err
	}
	_, _, err = checkedSessionModelCatalogSnapshot(ctx, tx, current.organizationID, candidate.SessionID, candidate.AccountRef, configuration, overlay, false)
	return err
}

func continuationReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	return errs.ErrUnavailable
}
